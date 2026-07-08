package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// appDirName is the name of the threev application data directory,
// created under the OS-specific user configuration directory.
const appDirName = "threev"

// dbFileName is the file name of the SQLite database within AppDataDir.
const dbFileName = "threev.db"

// AppDataDir returns the platform-specific application data directory for
// threev, creating it (with permissions 0700) if it does not yet exist.
//
// It resolves to:
//   - Windows: %AppData%/threev
//   - macOS:   ~/Library/Application Support/threev
//   - Linux:   ~/.config/threev
//
// (These are exactly the paths os.UserConfigDir returns per platform, with
// the appDirName suffix appended.)
func AppDataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}

	dir := filepath.Join(base, appDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create app data dir %q: %w", dir, err)
	}

	return dir, nil
}

// DBPath returns the full path to the SQLite database file, ensuring the
// parent application data directory exists.
func DBPath() (string, error) {
	dir, err := AppDataDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, dbFileName), nil
}
