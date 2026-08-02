package config

import (
	"os"
	"runtime"
	"testing"
)

// setHomeDir points os.UserHomeDir at dir on every supported platform.
//
// WO-32: os.UserHomeDir reads $HOME on Unix but %USERPROFILE% on Windows, so
// setting HOME alone left the Windows CI leg reading the runner's real home
// directory and picking up whatever config lived there.
func setHomeDir(t *testing.T, dir string) {
	t.Helper()

	t.Setenv("HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
	}
}

// TestSetHomeDirIsolates guards the helper itself: if os.UserHomeDir stops
// honouring what we set, every config auto-discovery test silently starts
// reading the real home directory instead of the fixture.
// WO-32: home isolation guard
func TestSetHomeDirIsolates(t *testing.T) {
	dir := t.TempDir()
	setHomeDir(t, dir)

	got, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	if got != dir {
		t.Fatalf("UserHomeDir() = %q, want %q — home isolation is not working on %s", got, dir, runtime.GOOS)
	}
}
