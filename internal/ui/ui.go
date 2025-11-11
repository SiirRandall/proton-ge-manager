package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/SiirRandall/proton-ge-manager/internal/install"

	"github.com/SiirRandall/proton-ge-manager/internal/github"
)

// internal state for lists & selection
var (
	currentInstalled  []string
	availableReleases []github.Release
	currentAvailable  []string

	selectedInstalled = -1
	selectedAvailable = -1

	geFolderPattern = regexp.MustCompile(`(?i)^(GE-Proton|Proton-GE).*`)
)

// read-only entry helpers (theme-friendly, no SetReadOnly in Fyne v2.7)
var (
	roMu   sync.Mutex
	roLast = map[*widget.Entry]string{}
)

func makeReadOnlyEntry(e *widget.Entry) {
	e.Wrapping = fyne.TextWrapWord
	e.TextStyle = fyne.TextStyle{Monospace: true}
	e.OnChanged = func(s string) {
		roMu.Lock()
		last := roLast[e]
		roMu.Unlock()
		if s != last {
			fyne.Do(func() {
				onchg := e.OnChanged
				e.OnChanged = nil
				e.SetText(last)
				e.CursorColumn = 0
				e.CursorRow = strings.Count(e.Text, "\n")
				e.OnChanged = onchg
			})
		}
	}
}
func setEntryText(e *widget.Entry, s string) {
	fyne.Do(func() {
		onchg := e.OnChanged
		e.OnChanged = nil
		e.SetText(s)
		e.CursorColumn = 0
		e.CursorRow = strings.Count(e.Text, "\n")
		e.OnChanged = onchg
		roMu.Lock()
		roLast[e] = s
		roMu.Unlock()
	})
}

func logLine(e *widget.Entry, format string, args ...any) {
	fyne.Do(func() {
		ts := time.Now().Format("15:04:05")
		line := fmt.Sprintf("[%s] %s\n", ts, fmt.Sprintf(format, args...))
		setEntryText(e, e.Text+line)
	})
}

func runOnUI(fn func()) { fyne.Do(fn) }

// Build builds and mounts the UI on the given window.
func Build(w fyne.Window) {
	// Resolve default install dir
	instDir, _ := install.ResolveInstallDir()

	// Lists
	installedList := widget.NewList(
		func() int { return len(currentInstalled) },
		func() fyne.CanvasObject { return widget.NewLabel("item") },
		func(i widget.ListItemID, o fyne.CanvasObject) { o.(*widget.Label).SetText(currentInstalled[i]) },
	)
	availableList := widget.NewList(
		func() int { return len(currentAvailable) },
		func() fyne.CanvasObject { return widget.NewLabel("release") },
		func(i widget.ListItemID, o fyne.CanvasObject) { o.(*widget.Label).SetText(currentAvailable[i]) },
	)
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

	// Install directory row (wider via GridWrap)
	installDirEntry := widget.NewEntry()
	makeReadOnlyEntry(installDirEntry)
	setEntryText(installDirEntry, instDir)

	entryW := float32(650)
	entryH := installDirEntry.MinSize().Height
	installDirBox := container.New(layout.NewGridWrapLayout(fyne.NewSize(entryW, entryH)), installDirEntry)

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
	makeReadOnlyEntry(logView)
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
		installDirBox,
		pickDirBtn,
	)
	bottom := container.NewVBox(
		pathRow,
		progress,
		widget.NewLabel("Status / Logs"),
		logView,
	)
	w.SetContent(container.NewBorder(nil, bottom, left, nil, right))

	// Combined refresh that keeps lists in sync
	refreshBoth := func() {
		loadInstalled(installDirEntry.Text, installedList, logView)
		loadAvailable(availableList, logView, w)
	}

	// Wiring
	pickDirBtn.OnTapped = func() {
		d := dialog.NewFolderOpen(func(list fyne.ListableURI, err error) {
			if err != nil || list == nil {
				return
			}
			localPath := list.Path()
			if localPath == "" {
				return
			}
			if err := install.EnsureDir(localPath); err != nil {
				dialog.ShowError(err, w)
				return
			}
			setEntryText(installDirEntry, localPath)
			logLine(logView, "Install dir set to: %s", localPath)
			refreshBoth()
		}, w)
		d.Show()
	}

	refreshInstalledBtn.OnTapped = refreshBoth
	refreshAvailableBtn.OnTapped = refreshBoth

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
			if err := removeInstalled(installDirEntry.Text, name); err != nil {
				dialog.ShowError(err, w)
				return
			}
			logLine(logView, "Removed %s", name)
			refreshBoth()
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
		asset, ok := github.PickLinuxTarball(rel.Assets)
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
			runOnUI(func() { progress.SetValue(0); progress.Show() })
			defer runOnUI(func() { progress.Hide() })

			logLine(logView, "Downloading %s …", asset.Name)
			err := install.DownloadAndExtract(w, asset.BrowserDownloadURL, asset.Size, targetDir, progress, func(f string, a ...any) {
				logLine(logView, f, a...)
			})
			if err != nil {
				runOnUI(func() {
					dialog.ShowError(err, w)
					logLine(logView, "Install failed: %v", err)
				})
				return
			}
			runOnUI(func() { progress.SetValue(1.0) })
			logLine(logView, "Installed release: %s%s — %s", rel.TagName, ternary(rel.Prerelease, " (pre-release)", ""), rel.Name)
			refreshBoth()
		}()
	}

	// First load
	refreshBoth()
}

func loadInstalled(installDir string, list *widget.List, log *widget.Entry) {
	if installDir == "" {
		return
	}

	dirs, err := readDirNames(installDir)
	if err != nil {
		logLine(log, "Read error: %v", err)
		return
	}

	var names []string
	for _, n := range dirs {
		if geFolderPattern.MatchString(n) {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	currentInstalled = names
	selectedInstalled = -1
	runOnUI(func() { list.Refresh() })
	logLine(log, "Found %d installed Proton-GE folders.", len(currentInstalled))
}

func loadAvailable(list *widget.List, log *widget.Entry, w fyne.Window) {
	go func() {
		rels, err := github.FetchAllReleases(context.Background())
		if err != nil {
			runOnUI(func() {
				dialog.ShowError(err, w)
				logLine(log, "GitHub fetch failed: %v", err)
			})
			return
		}
		// filter out installed by tag name
		installed := map[string]bool{}
		for _, inst := range currentInstalled {
			installed[strings.ToLower(inst)] = true
		}
		filtered := make([]github.Release, 0, len(rels))
		for _, r := range rels {
			if !installed[strings.ToLower(r.TagName)] {
				filtered = append(filtered, r)
			}
		}
		availableReleases = filtered
		currentAvailable = make([]string, len(filtered))
		for i, r := range filtered {
			currentAvailable[i] = formatReleaseLabel(r)
		}
		runOnUI(func() {
			selectedAvailable = -1
			list.Refresh()
			logLine(log, "Loaded %d GitHub releases (%d filtered).", len(filtered), len(rels)-len(filtered))
		})
	}()
}

func formatReleaseLabel(r github.Release) string {
	flag := ""
	if r.Prerelease {
		flag = " (pre-release)"
	}
	return fmt.Sprintf("%s%s — %s", r.TagName, flag, r.Name)
}

func removeInstalled(root, name string) error {
	return osRemoveAll(join(root, name))
}

/* -------- small OS helpers (keep UI file self-contained) -------- */

func readDirNames(p string) ([]string, error) {
	ents, err := osReadDir(p)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ents))
	for _, e := range ents {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// wrappers (avoid importing os directly at top to keep imports tidy)
var (
	osReadDir   = func(name string) ([]osDirEntry, error) { return realReadDir(name) }
	osRemoveAll = func(path string) error { return realRemoveAll(path) }
	join        = func(elem ...string) string { return realJoin(elem...) }
)

type osDirEntry interface {
	Name() string
	IsDir() bool
}

func realReadDir(name string) ([]osDirEntry, error) {
	ents, err := os.ReadDir(name)
	if err != nil {
		return nil, err
	}
	out := make([]osDirEntry, len(ents))
	for i := range ents {
		out[i] = ents[i]
	}
	return out, nil
}
func realRemoveAll(path string) error { return os.RemoveAll(path) }
func realJoin(elem ...string) string  { return filepath.Join(elem...) }

// tiny ternary helper
func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}
