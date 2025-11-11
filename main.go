package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

const (
	repoOwner = "GloriousEggroll"
	repoName  = "proton-ge-custom"
	apiURL    = "https://api.github.com/repos/" + repoOwner + "/" + repoName + "/releases"
)

type ghRelease struct {
	TagName   string    `json:"tag_name"`
	Name      string    `json:"name"`
	Prerel    bool      `json:"prerelease"`
	Assets    []ghAsset `json:"assets"`
	Draft     bool      `json:"draft"`
	Body      string    `json:"body"`
	HTMLURL   string    `json:"html_url"`
	Created   time.Time `json:"created_at"`
	Published time.Time `json:"published_at"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	ContentType        string `json:"content_type"`
}

var (
	geFolderPattern = regexp.MustCompile(`(?i)^(GE-Proton|Proton-GE).*`)
	tarballPattern  = regexp.MustCompile(`(?i)\.tar\.gz$`)
)

func main() {
	if runtime.GOOS != "linux" {
		fmt.Println("Note: Proton-GE is primarily for Linux Steam installations. Paths here target Linux layouts.")
	}

	a := app.NewWithID("com.sirrandall.protonge.manager")
	w := a.NewWindow("Proton-GE Manager")
	w.Resize(fyne.NewSize(980, 640))

	// Lists
	installedList := widget.NewList(
		func() int { return len(currentInstalled) },
		func() fyne.CanvasObject { return widget.NewLabel("item") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(currentInstalled[i])
		},
	)
	availableList := widget.NewList(
		func() int { return len(currentAvailable) },
		func() fyne.CanvasObject { return widget.NewLabel("release") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(currentAvailable[i])
		},
	)

	// Track selection
	installedList.OnSelected = func(id widget.ListItemID) { selectedInstalled = id }
	installedList.OnUnselected = func(id widget.ListItemID) {
		if selectedInstalled == id {
			selectedInstalled = -1
		}
	}
	availableList.OnSelected = func(id widget.ListItemID) { selectedAvailable = id }
	availableList.OnUnselected = func(id widget.ListItemID) {
		if selectedAvailable == id {
			selectedAvailable = -1
		}
	}

	installDirEntry := widget.NewEntry()
	installDirEntry.Disable()
	pickDirBtn := widget.NewButton("Change…", nil)

	refreshInstalledBtn := widget.NewButton("Refresh Installed", nil)
	refreshAvailableBtn := widget.NewButton("Refresh Releases", nil)

	installBtn := widget.NewButton("Install Selected Release", nil)
	removeBtn := widget.NewButton("Remove Selected Installed", nil)

	progress := widget.NewProgressBar()
	progress.Min = 0
	progress.Max = 1
	progress.Hide()

	logView := widget.NewMultiLineEntry()
	logView.Disable()
	logView.SetPlaceHolder("Logs will appear here…")

	// Layout
	left := container.NewBorder(
		container.NewVBox(widget.NewLabel("Installed Proton-GE:"), refreshInstalledBtn),
		container.NewVBox(removeBtn),
		nil, nil,
		installedList,
	)
	right := container.NewBorder(
		container.NewVBox(widget.NewLabel("Available Releases (GitHub):"), refreshAvailableBtn),
		container.NewVBox(installBtn),
		nil, nil,
		availableList,
	)

	pathRow := container.NewHBox(
		widget.NewLabel("Install directory:"),
		installDirEntry,
		pickDirBtn,
	)

	bottom := container.NewVBox(
		pathRow,
		progress,
		widget.NewLabel("Status / Logs"),
		logView,
	)

	content := container.NewBorder(nil, bottom, left, nil, right)
	w.SetContent(content)

	// Wiring
	instDir, err := resolveInstallDir()
	if err != nil {
		logLine(logView, "Could not resolve install dir automatically: %v", err)
	}
	installDirEntry.SetText(instDir)

	pickDirBtn.OnTapped = func() {
		d := dialog.NewFolderOpen(func(list fyne.ListableURI, err error) {
			if err != nil || list == nil {
				return
			}
			localPath := list.Path()
			if localPath == "" {
				return
			}
			if err := ensureDir(localPath); err != nil {
				dialog.ShowError(err, w)
				return
			}
			installDirEntry.SetText(localPath)
			logLine(logView, "Install dir set to: %s", localPath)
			loadInstalled(installDirEntry.Text, installedList, logView)
		}, w)
		d.Show()
	}

	refreshInstalledBtn.OnTapped = func() {
		loadInstalled(installDirEntry.Text, installedList, logView)
	}

	refreshAvailableBtn.OnTapped = func() {
		loadAvailable(availableList, logView, w)
	}

	removeBtn.OnTapped = func() {
		i := selectedInstalled
		if i < 0 || i >= len(currentInstalled) {
			dialog.ShowInformation("Remove", "Select an installed version to remove.", w)
			return
		}
		name := currentInstalled[i]
		confirm := dialog.NewConfirm("Remove Proton-GE", fmt.Sprintf("Delete '%s' from disk?", name), func(ok bool) {
			if !ok {
				return
			}
			if err := os.RemoveAll(filepath.Join(installDirEntry.Text, name)); err != nil {
				dialog.ShowError(err, w)
				return
			}
			logLine(logView, "Removed %s", name)
			loadInstalled(installDirEntry.Text, installedList, logView)
		}, w)
		confirm.Show()
	}

	installBtn.OnTapped = func() {
		i := selectedAvailable
		if i < 0 || i >= len(availableReleases) {
			dialog.ShowInformation("Install", "Select a release to install.", w)
			return
		}
		rel := availableReleases[i]
		asset, ok := pickLinuxTarball(rel.Assets)
		if !ok {
			dialog.ShowInformation("Install", "No suitable .tar.gz asset found on that release.", w)
			return
		}

		targetDir := installDirEntry.Text
		if targetDir == "" {
			dialog.ShowInformation("Install", "Install directory not set.", w)
			return
		}

		go func() {
			runOnUI(w, func() {
				progress.SetValue(0)
				progress.Show()
			})
			defer runOnUI(w, func() { progress.Hide() })

			logLine(logView, "Downloading %s …", asset.Name)
			err := downloadAndExtract(w, asset.BrowserDownloadURL, asset.Size, targetDir, progress, logView)
			if err != nil {
				runOnUI(w, func() {
					dialog.ShowError(err, w)
					logLine(logView, "Install failed: %v", err)
				})
				return
			}
			logLine(logView, "Installed release: %s", formatReleaseLabel(rel))
			loadInstalled(targetDir, installedList, logView)
		}()
	}

	// Initial loads
	loadInstalled(instDir, installedList, logView)
	loadAvailable(availableList, logView, w)

	w.ShowAndRun()
}

/* ---------- Globals & helpers for UI binding ---------- */

var (
	currentInstalled  []string
	availableReleases []ghRelease
	currentAvailable  []string // formatted labels from availableReleases

	selectedInstalled = -1
	selectedAvailable = -1
)

func formatReleaseLabel(r ghRelease) string {
	flag := ""
	if r.Prerel {
		flag = " (pre-release)"
	}
	return fmt.Sprintf("%s%s — %s", r.TagName, flag, r.Name)
}

/* ---------- Install dir detection ---------- */

func resolveInstallDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	candidates := []string{
		filepath.Join(home, ".local/share/Steam/compatibilitytools.d"),                          // native Steam (XDG)
		filepath.Join(home, ".steam/steam/compatibilitytools.d"),                                // legacy
		filepath.Join(home, ".steam/root/compatibilitytools.d"),                                 // legacy
		filepath.Join(home, ".var/app/com.valvesoftware.Steam/data/Steam/compatibilitytools.d"), // Flatpak Steam
	}
	for _, c := range candidates {
		if dirExists(c) {
			return c, nil
		}
	}
	// Fallback: create the XDG path
	fp := candidates[0]
	if err := ensureDir(fp); err != nil {
		return "", err
	}
	return fp, nil
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func ensureDir(p string) error {
	return os.MkdirAll(p, 0o755)
}

/* ---------- Installed list ---------- */

func loadInstalled(installDir string, list *widget.List, log *widget.Entry) {
	if installDir == "" {
		return
	}
	entries, err := os.ReadDir(installDir)
	if err != nil {
		logLine(log, "Read error: %v", err)
		return
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && geFolderPattern.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	currentInstalled = names
	selectedInstalled = -1

	// UI-thread safe refresh & log
	runOnUI(nil, func() {
		list.Refresh()
	})
	logLine(log, "Found %d installed Proton-GE folders.", len(currentInstalled))
}

/* ---------- Available releases (GitHub) ---------- */

// Updated: fetches ALL pages (not just the first) and filters drafts; keeps pre-releases with tarball
func loadAvailable(list *widget.List, log *widget.Entry, w fyne.Window) {
	go func() {
		rels, err := fetchAllReleases(context.Background())
		if err != nil {
			runOnUI(w, func() {
				dialog.ShowError(err, w)
				logLine(log, "GitHub fetch failed: %v", err)
			})
			return
		}
		availableReleases = rels
		currentAvailable = make([]string, len(rels))
		for i, r := range rels {
			currentAvailable[i] = formatReleaseLabel(r)
		}
		runOnUI(w, func() {
			selectedAvailable = -1
			list.Refresh()
			logLine(log, "Loaded %d GitHub releases.", len(rels))
		})
	}()
}

func fetchAllReleases(ctx context.Context) ([]ghRelease, error) {
	client := &http.Client{Timeout: 45 * time.Second}
	page := 1
	perPage := 100

	var all []ghRelease
	for {
		req, _ := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
		q := req.URL.Query()
		q.Set("per_page", fmt.Sprint(perPage))
		q.Set("page", fmt.Sprint(page))
		req.URL.RawQuery = q.Encode()
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "ProtonGE-Manager/1.0 (+fyne)")
		if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusForbidden {
				err = fmt.Errorf("GitHub API returned 403 (rate limited?). Try setting GITHUB_TOKEN")
				return
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
				err = fmt.Errorf("GitHub API error: %s: %s", resp.Status, strings.TrimSpace(string(body)))
				return
			}

			var rels []ghRelease
			if derr := json.NewDecoder(resp.Body).Decode(&rels); derr != nil {
				err = derr
				return
			}
			if len(rels) == 0 {
				// no more pages
				return
			}
			all = append(all, rels...)
		}()
		if err != nil {
			return nil, err
		}
		// Break if last page returned empty
		if len(all) < page*perPage {
			break
		}
		page++
	}

	// Filter: skip drafts; require a tarball
	out := all[:0]
	for _, r := range all {
		if r.Draft {
			continue
		}
		if _, ok := pickLinuxTarball(r.Assets); ok {
			out = append(out, r)
		}
	}
	return out, nil
}

func pickLinuxTarball(assets []ghAsset) (ghAsset, bool) {
	// Prefer .tar.gz assets that look like Proton-GE
	var candidates []ghAsset
	for _, a := range assets {
		if tarballPattern.MatchString(a.Name) && strings.Contains(strings.ToLower(a.Name), "ge-proton") {
			candidates = append(candidates, a)
		}
	}
	if len(candidates) == 0 {
		// fallback: any .tar.gz
		for _, a := range assets {
			if tarballPattern.MatchString(a.Name) {
				candidates = append(candidates, a)
			}
		}
	}
	if len(candidates) == 0 {
		return ghAsset{}, false
	}
	// Pick the largest (usually the main payload)
	slices.SortFunc(candidates, func(a, b ghAsset) int {
		switch {
		case a.Size == b.Size:
			return 0
		case a.Size < b.Size:
			return 1
		default:
			return -1
		}
	})
	return candidates[0], true
}

/* ---------- Download & extract (progress-safe) ---------- */

func downloadAndExtract(w fyne.Window, url string, sizeHint int64, destRoot string, bar *widget.ProgressBar, log *widget.Entry) error {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "ProtonGE-Manager/1.0")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	contentLen := resp.ContentLength
	if contentLen <= 0 {
		contentLen = int64(sizeHint)
	}

	var reader io.Reader = resp.Body
	var bytesRead int64

	update := func() {
		if contentLen > 0 {
			bar.SetValue(float64(bytesRead) / float64(contentLen))
		}
	}

	reader = io.TeeReader(reader, writerFunc(func(p []byte) (int, error) {
		n := len(p)
		bytesRead += int64(n)
		runOnUI(w, update)
		return n, nil
	}))

	gzr, err := gzip.NewReader(reader)
	if err != nil {
		return fmt.Errorf("gzip open: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	createdTopFolder := ""

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}
		if hdr == nil {
			continue
		}
		// sanitize paths
		name := filepath.Clean(hdr.Name)
		if name == "." || name == "/" || strings.HasPrefix(name, "..") {
			continue
		}
		// Proton-GE tarballs include top directory like GE-Proton9-5
		parts := strings.Split(name, "/")
		if createdTopFolder == "" && len(parts) > 0 {
			createdTopFolder = parts[0]
		}
		targetPath := filepath.Join(destRoot, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return err
			}
			_ = f.Close()
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return err
			}
			link := filepath.Clean(hdr.Linkname)
			if strings.HasPrefix(link, "..") {
				link = strings.TrimPrefix(link, "../")
			}
			if err := os.Symlink(link, targetPath); err != nil {
				logLine(log, "Symlink create failed for %s -> %s: %v (continuing)", targetPath, link, err)
			}
		default:
			// ignore other types
		}
	}

	if createdTopFolder == "" {
		return fmt.Errorf("unexpected archive layout (no top folder)")
	}
	logLine(log, "Extracted: %s", filepath.Join(destRoot, createdTopFolder))
	return nil
}

/* ---------- Utilities (thread-safe UI) ---------- */

func runOnUI(_ fyne.Window, fn func()) {
	// Ensure UI work runs on Fyne's call thread (safe across versions)
	fyne.Do(fn)
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// UPDATED: make logLine UI-thread safe regardless of caller goroutine.
func logLine(e *widget.Entry, format string, args ...any) {
	fn := func() {
		ts := time.Now().Format("15:04:05")
		line := fmt.Sprintf("[%s] %s\n", ts, fmt.Sprintf(format, args...))
		e.SetText(e.Text + line)
		e.CursorColumn = 0
		e.CursorRow = strings.Count(e.Text, "\n")
	}
	fyne.Do(fn)
}

/* ---------- Extras / future helpers ---------- */

// readSmallFile returns up to n bytes of a file or "" if not present.
func readSmallFile(p string, n int) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	b, _ := io.ReadAll(io.LimitReader(f, int64(n)))
	return string(b)
}

// humanizeBytes formats bytes as a human string.
func humanizeBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
