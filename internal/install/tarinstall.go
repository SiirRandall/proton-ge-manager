package install

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// DownloadAndExtract streams a .tar.gz and extracts under destRoot, showing progress on bar.
// logLine should be a threadsafe func for appending to a log widget.
func DownloadAndExtract(w fyne.Window, url string, sizeHint int64, destRoot string, bar *widget.ProgressBar, logLine func(format string, args ...any)) error {
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
		fyne.Do(update)
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

		name := filepath.Clean(hdr.Name)
		if name == "." || name == "/" || strings.HasPrefix(name, "..") {
			continue
		}
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
				logLine("Symlink create failed for %s -> %s: %v (continuing)", targetPath, link, err)
			}
		default:
			// ignore
		}
	}

	if createdTopFolder == "" {
		return fmt.Errorf("unexpected archive layout (no top folder)")
	}
	logLine("Extracted: %s", filepath.Join(destRoot, createdTopFolder))
	return nil
}
