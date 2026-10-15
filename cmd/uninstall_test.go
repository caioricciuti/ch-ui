package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeepDesktopData(t *testing.T) {
	root := t.TempDir()
	desk := filepath.Join(root, "Roaming", "ch-ui")
	connector := filepath.Join(desk, "connect")
	paths := []string{
		desk,                               // the folder itself
		filepath.Join(desk, "ch-ui.db"),    // inside it
		root,                               // contains it
		filepath.Join(root, "Roaming"),     // contains it
		filepath.Join(root, "ch-ui-other"), // unrelated
		filepath.Join(root, "Roaming", "ch-ui2"),
		connector, // inside it, but the connector folder is still removed
	}
	got := keepDesktopData(paths, desk, connector)
	want := []string{filepath.Join(root, "ch-ui-other"), filepath.Join(root, "Roaming", "ch-ui2"), connector}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("keepDesktopData = %v, want %v", got, want)
	}
	if got := keepDesktopData(paths, "", connector); len(got) != len(paths) {
		t.Fatalf("no desktop folder: got %d paths, want %d", len(got), len(paths))
	}
}

func TestManualUninstallCommandsWindows(t *testing.T) {
	plan := uninstallPlan{
		serverConfigPath: `C:\Users\ana\server.yaml`,
		databasePath:     `.\data\ch-ui.db`,
		serverPIDFiles:   []string{`C:\Users\ana\ch-ui-server.pid`},
		connectorDir:     `C:\Users\ana\AppData\Roaming\ch-ui\connect`,
		desktopDataDir:   `C:\Users\ana\AppData\Roaming\ch-ui`,
	}
	cmds := manualUninstallCommands("windows", plan)
	all := strings.Join(cmds, "\n")
	for _, banned := range []string{"sudo", "systemctl", "launchctl", "rm -", "/usr/local/bin"} {
		if strings.Contains(all, banned) {
			t.Errorf("Windows commands contain %q:\n%s", banned, all)
		}
	}
	if cmds[0] != `Remove-Item -Recurse -Force -ErrorAction SilentlyContinue 'C:\Users\ana\AppData\Roaming\ch-ui\connect'` {
		t.Errorf("first command = %q", cmds[0])
	}
	for _, c := range cmds {
		if strings.HasSuffix(c, "'"+plan.desktopDataDir+"'") {
			t.Errorf("manual commands delete the desktop data folder: %q", c)
		}
	}
	if !strings.Contains(all, `Remove-Item -Force -ErrorAction SilentlyContinue '.\data\ch-ui.db'`) {
		t.Errorf("database removal missing:\n%s", all)
	}
}

func TestManualUninstallCommandsLinuxUnchanged(t *testing.T) {
	plan := uninstallPlan{
		serverConfigPath: "/etc/ch-ui/server.yaml",
		databasePath:     "./data/ch-ui.db",
		connectorDir:     "/etc/ch-ui",
		binaryPath:       "/usr/local/bin/ch-ui",
		desktopDataDir:   "/home/ana/.config/ch-ui",
	}
	all := strings.Join(manualUninstallCommands("linux", plan), "\n")
	for _, want := range []string{"sudo systemctl daemon-reload", "sudo rm -rf '/etc/ch-ui'", "sudo rm -f '/usr/local/bin/ch-ui'"} {
		if !strings.Contains(all, want) {
			t.Errorf("Linux commands miss %q:\n%s", want, all)
		}
	}
	if strings.Contains(all, plan.desktopDataDir) {
		t.Errorf("Linux commands delete the desktop data folder:\n%s", all)
	}
}

func TestPSQuote(t *testing.T) {
	if got := psQuote(`C:\Users\o'neil\x`); got != `'C:\Users\o''neil\x'` {
		t.Fatalf("psQuote = %s", got)
	}
}

func TestStopDesktopInstanceStaleState(t *testing.T) {
	dir := t.TempDir()
	if err := stopDesktopInstance(dir); err != nil {
		t.Fatalf("no state: %v", err)
	}
	pidPath := filepath.Join(dir, desktopPIDName)
	addrPath := filepath.Join(dir, desktopAddrName)
	if err := os.WriteFile(pidPath, []byte("garbage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(addrPath, []byte("127.0.0.1:3488\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stopDesktopInstance(dir); err != nil {
		t.Fatalf("stale state: %v", err)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatal("stale PID file not removed")
	}
	if _, err := os.Stat(addrPath); !os.IsNotExist(err) {
		t.Fatal("stale address file not removed")
	}
}
