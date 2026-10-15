package cmd

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

func TestLoopbackHost(t *testing.T) {
	ok := map[string]string{
		"127.0.0.1":        "127.0.0.1",
		" 127.0.0.1 ":      "127.0.0.1",
		"127.0.0.2":        "127.0.0.2",
		"localhost":        "127.0.0.1",
		"LOCALHOST":        "127.0.0.1",
		"::1":              "::1",
		"[::1]":            "::1",
		"::ffff:127.0.0.1": "127.0.0.1",
	}
	for in, want := range ok {
		got, err := loopbackHost(in)
		if err != nil || got != want {
			t.Errorf("loopbackHost(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0.0.0.0", "::", "192.168.1.10", "10.0.0.1", "example.com", "127.0.0.1:3488", "::ffff:10.0.0.1"} {
		if got, err := loopbackHost(in); err == nil {
			t.Errorf("loopbackHost(%q) = %q, want an error", in, got)
		}
	}
}

func TestDesktopDataDir(t *testing.T) {
	tests := []struct {
		name    string
		goos    string
		env     map[string]string
		want    string
		wantErr bool
	}{
		{name: "macOS", goos: "darwin", env: map[string]string{"HOME": "/Users/ana"}, want: filepath.Join("/Users/ana", "Library", "Application Support", "ch-ui")},
		{name: "macOS ignores XDG", goos: "darwin", env: map[string]string{"HOME": "/Users/ana", "XDG_CONFIG_HOME": "/x"}, want: filepath.Join("/Users/ana", "Library", "Application Support", "ch-ui")},
		{name: "macOS without HOME", goos: "darwin", env: map[string]string{}, wantErr: true},
		{name: "Windows", goos: "windows", env: map[string]string{"AppData": `C:\Users\ana\AppData\Roaming`}, want: filepath.Join(`C:\Users\ana\AppData\Roaming`, "ch-ui")},
		{name: "Windows without AppData", goos: "windows", env: map[string]string{"HOME": "/home/ana"}, wantErr: true},
		{name: "Linux XDG", goos: "linux", env: map[string]string{"HOME": "/home/ana", "XDG_CONFIG_HOME": "/home/ana/cfg"}, want: filepath.Join("/home/ana/cfg", "ch-ui")},
		{name: "Linux relative XDG ignored", goos: "linux", env: map[string]string{"HOME": "/home/ana", "XDG_CONFIG_HOME": "cfg"}, want: filepath.Join("/home/ana", ".config", "ch-ui")},
		{name: "Linux HOME", goos: "linux", env: map[string]string{"HOME": "/home/ana"}, want: filepath.Join("/home/ana", ".config", "ch-ui")},
		{name: "Linux nothing set", goos: "linux", env: map[string]string{}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := desktopDataDir(tt.goos, func(k string) string { return tt.env[k] })
			if tt.wantErr {
				if err == nil {
					t.Fatalf("got %q, want an error", got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

// freePort returns a loopback port that was free a moment ago.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func TestListenDesktop(t *testing.T) {
	t.Run("preferred port when free", func(t *testing.T) {
		port := freePort(t)
		ln, err := listenDesktop("127.0.0.1", port, false)
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		if got := ln.Addr().(*net.TCPAddr).Port; got != port {
			t.Fatalf("port = %d, want %d", got, port)
		}
	})

	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	busyPort := busy.Addr().(*net.TCPAddr).Port

	t.Run("busy default port falls back", func(t *testing.T) {
		ln, err := listenDesktop("127.0.0.1", busyPort, false)
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		tcp := ln.Addr().(*net.TCPAddr)
		if tcp.Port == busyPort || tcp.Port == 0 {
			t.Fatalf("port = %d, want a different free port", tcp.Port)
		}
		if !tcp.IP.IsLoopback() {
			t.Fatalf("listening on %s, want loopback", tcp.IP)
		}
	})

	t.Run("busy explicit port fails", func(t *testing.T) {
		ln, err := listenDesktop("127.0.0.1", busyPort, true)
		if err == nil {
			ln.Close()
			t.Fatal("want an error for a busy explicit port")
		}
	})
}

func TestRunningDesktopAddr(t *testing.T) {
	live, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	liveAddr := live.Addr().String()
	closedAddr := "127.0.0.1:" + strconv.Itoa(freePort(t))

	tests := []struct {
		name      string
		pid       string // "" means no PID file
		addr      string // "" means no address file
		wantOK    bool
		wantFiles bool // state files still present afterwards
	}{
		{name: "nothing recorded", wantOK: false},
		{name: "live instance", pid: strconv.Itoa(os.Getpid()), addr: liveAddr, wantOK: true, wantFiles: true},
		{name: "port closed", pid: strconv.Itoa(os.Getpid()), addr: closedAddr},
		{name: "no address file", pid: strconv.Itoa(os.Getpid())},
		{name: "garbage PID", pid: "nope", addr: liveAddr},
		{name: "non-loopback address", pid: strconv.Itoa(os.Getpid()), addr: "192.0.2.1:3488"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			pidPath := filepath.Join(dir, desktopPIDName)
			addrPath := filepath.Join(dir, desktopAddrName)
			if tt.pid != "" {
				if err := os.WriteFile(pidPath, []byte(tt.pid+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tt.addr != "" {
				if err := os.WriteFile(addrPath, []byte(tt.addr+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			got, ok := runningDesktopAddr(pidPath, addrPath)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (addr %q)", ok, tt.wantOK, got)
			}
			if ok && got != liveAddr {
				t.Fatalf("addr = %q, want %q", got, liveAddr)
			}
			_, pidErr := os.Stat(pidPath)
			_, addrErr := os.Stat(addrPath)
			if tt.wantFiles && (pidErr != nil || addrErr != nil) {
				t.Fatalf("state files removed for a live instance")
			}
			if !tt.wantFiles && (pidErr == nil || addrErr == nil) {
				t.Fatalf("stale state files left behind")
			}
		})
	}
}

func TestDesktopStateCleanup(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, desktopPIDName)
	addrPath := filepath.Join(dir, desktopAddrName)
	if err := writeDesktopState(pidPath, addrPath, 4242, "127.0.0.1:3488"); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		for _, p := range []string{pidPath, addrPath} {
			info, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			if perm := info.Mode().Perm(); perm != 0o600 {
				t.Errorf("%s mode = %o, want 600", filepath.Base(p), perm)
			}
		}
	}

	// Another instance's files are left alone.
	cleanupDesktopState(pidPath, addrPath, 1)
	if _, err := os.Stat(pidPath); err != nil {
		t.Fatal("cleanup removed a PID file owned by another process")
	}
	cleanupDesktopState(pidPath, addrPath, 4242)
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatal("PID file not removed")
	}
	if _, err := os.Stat(addrPath); !os.IsNotExist(err) {
		t.Fatal("address file not removed")
	}
}

func TestBrowserCommand(t *testing.T) {
	url := "http://127.0.0.1:3488"
	tests := []struct {
		goos string
		name string
		args []string
	}{
		{"darwin", "open", []string{url}},
		{"windows", "rundll32", []string{"url.dll,FileProtocolHandler", url}},
		{"linux", "xdg-open", []string{url}},
	}
	for _, tt := range tests {
		name, args := browserCommand(tt.goos, url)
		if name != tt.name || len(args) != len(tt.args) {
			t.Fatalf("%s: got %s %v, want %s %v", tt.goos, name, args, tt.name, tt.args)
		}
		for i := range args {
			if args[i] != tt.args[i] {
				t.Fatalf("%s: got %s %v, want %s %v", tt.goos, name, args, tt.name, tt.args)
			}
		}
	}
}

func TestReleaseAssetName(t *testing.T) {
	cases := map[[2]string]string{
		{"linux", "amd64"}:   "ch-ui-linux-amd64",
		{"darwin", "arm64"}:  "ch-ui-darwin-arm64",
		{"windows", "amd64"}: "ch-ui-windows-amd64.exe",
		{"windows", "arm64"}: "ch-ui-windows-arm64.exe",
	}
	for in, want := range cases {
		if got := releaseAssetName(in[0], in[1]); got != want {
			t.Errorf("releaseAssetName(%s, %s) = %q, want %q", in[0], in[1], got, want)
		}
	}
}
