package config

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestConfigDirFor(t *testing.T) {
	home := func() (string, error) { return "/home/ana", nil }
	appData := func() (string, error) { return `C:\Users\ana\AppData\Roaming`, nil }
	noAppData := func() (string, error) { return "", errors.New("%AppData% is not defined") }

	tests := []struct {
		name       string
		goos       string
		userConfig func() (string, error)
		want       string
	}{
		{"macOS", "darwin", appData, filepath.Join("/home/ana", ".config", "ch-ui")},
		{"Linux", "linux", appData, "/etc/ch-ui"},
		{"Windows", "windows", appData, filepath.Join(`C:\Users\ana\AppData\Roaming`, "ch-ui", "connect")},
		{"Windows without AppData", "windows", noAppData, filepath.Join("/home/ana", "AppData", "Roaming", "ch-ui", "connect")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := configDirFor(tt.goos, home, tt.userConfig); got != tt.want {
				t.Fatalf("configDirFor(%s) = %q, want %q", tt.goos, got, tt.want)
			}
		})
	}
}
