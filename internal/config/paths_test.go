package config

import (
	"os"
	"path/filepath"
	"testing"
)

// These tests exercise the real, unmodified os.UserConfigDir() resolution
// for whatever platform the test runs on (Windows: %AppData%, macOS:
// ~/Library/Application Support, Linux: ~/.config) rather than stubbing it
// out: os.UserConfigDir only honors an overridable environment variable on
// Windows (%AppData%) and Linux (XDG_CONFIG_HOME), never on macOS, so there
// is no single t.Setenv override that isolates all three platforms
// uniformly. CI runners are single-use, ephemeral machines, so writing to
// the real user config directory here is an accepted, intentional
// trade-off rather than an oversight.

func TestAppDataDirCreatesDirectory(t *testing.T) {
	t.Parallel()

	dir, err := AppDataDir()
	if err != nil {
		t.Fatalf("AppDataDir() error = %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("AppDataDir() = %q, but os.Stat failed: %v", dir, err)
	}
	if !info.IsDir() {
		t.Fatalf("AppDataDir() = %q is not a directory", dir)
	}

	if filepath.Base(dir) != appDirName {
		t.Errorf("AppDataDir() = %q, want last path segment %q", dir, appDirName)
	}
}

func TestAppDataDirIdempotent(t *testing.T) {
	t.Parallel()

	first, err := AppDataDir()
	if err != nil {
		t.Fatalf("AppDataDir() error = %v", err)
	}

	// Second call must succeed even though the directory already exists
	// (os.MkdirAll is idempotent), and must resolve to the same path.
	second, err := AppDataDir()
	if err != nil {
		t.Fatalf("AppDataDir() (second call) error = %v", err)
	}

	if first != second {
		t.Errorf("AppDataDir() returned different paths across calls: %q != %q", first, second)
	}
}

func TestDBPathEndsInDBFile(t *testing.T) {
	t.Parallel()

	path, err := DBPath()
	if err != nil {
		t.Fatalf("DBPath() error = %v", err)
	}

	if filepath.Base(path) != dbFileName {
		t.Errorf("DBPath() = %q, want file name %q", path, dbFileName)
	}

	parent := filepath.Dir(path)
	if filepath.Base(parent) != appDirName {
		t.Errorf("DBPath() parent dir = %q, want last segment %q", parent, appDirName)
	}

	if _, err := os.Stat(parent); err != nil {
		t.Errorf("DBPath() parent directory does not exist: %v", err)
	}
}
