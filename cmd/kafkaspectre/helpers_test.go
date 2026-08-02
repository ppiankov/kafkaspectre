package main

import (
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
