package install

import (
	"os"
	"path/filepath"
)

func ResolveInstallDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	candidates := []string{
		filepath.Join(home, ".local/share/Steam/compatibilitytools.d"),
		filepath.Join(home, ".steam/steam/compatibilitytools.d"),
		filepath.Join(home, ".steam/root/compatibilitytools.d"),
		filepath.Join(home, ".var/app/com.valvesoftware.Steam/data/Steam/compatibilitytools.d"),
	}
	for _, c := range candidates {
		if dirExists(c) {
			return c, nil
		}
	}
	// fallback: create the first
	fp := candidates[0]
	if err := EnsureDir(fp); err != nil {
		return "", err
	}
	return fp, nil
}

func EnsureDir(p string) error {
	return os.MkdirAll(p, 0o755)
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
