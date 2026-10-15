package cmd

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/spf13/cobra"
)

var (
	desktopPort           int
	desktopHost           string
	desktopNoBrowser      bool
	desktopConfig         string
	desktopClickHouse     string
	desktopConnectionName string
)

const (
	desktopDefaultPort = 3488
	desktopDBName      = "ch-ui.db"
	desktopPIDName     = "desktop.pid"
	// desktopAddrName holds the host:port the running desktop instance
	// listens on, so a second launch can open the browser to it.
	desktopAddrName = "desktop.addr"
)

var desktopCmd = &cobra.Command{
	Use:   "desktop",
	Short: "Run CH-UI as a personal app on this computer",
	Long: `Run CH-UI for yourself on this computer and open it in your browser.

It listens on 127.0.0.1 only, never on the network. The database and the
app secret key live in your user data folder:

  macOS     ~/Library/Application Support/ch-ui
  Windows   %AppData%\ch-ui
  Linux     $XDG_CONFIG_HOME/ch-ui, or ~/.config/ch-ui

DATABASE_PATH and APP_SECRET_KEY override the database and key as they do
for ch-ui server. Port 3488 is used when free, otherwise any free port.
If CH-UI desktop is already running, this opens the browser to it.`,
	Args: cobra.NoArgs,
	RunE: runDesktop,
}

func init() {
	f := desktopCmd.Flags()
	f.IntVarP(&desktopPort, "port", "p", desktopDefaultPort, "Port to listen on; if the default is taken, any free port is used")
	f.StringVar(&desktopHost, "host", "127.0.0.1", "Loopback address to listen on (127.0.0.1, ::1 or localhost)")
	f.BoolVar(&desktopNoBrowser, "no-browser", false, "Do not open the browser")
	f.StringVarP(&desktopConfig, "config", "c", "", "Path to config file")
	f.StringVar(&desktopClickHouse, "clickhouse-url", "", "Local ClickHouse HTTP URL for the embedded connection")
	f.StringVar(&desktopConnectionName, "connection-name", "", "Display name for the embedded local connection")
	rootCmd.AddCommand(desktopCmd)
}

func runDesktop(cmd *cobra.Command, args []string) error {
	host, err := loopbackHost(desktopHost)
	if err != nil {
		return err
	}

	dataDir, err := desktopDataDir(runtime.GOOS, os.Getenv)
	if err != nil {
		return err
	}
	dbPath := filepath.Join(dataDir, desktopDBName)
	if v := os.Getenv("DATABASE_PATH"); v != "" {
		if dbPath, err = filepath.Abs(v); err != nil {
			return fmt.Errorf("resolve DATABASE_PATH: %w", err)
		}
	}
	// The PID and address files sit next to the database, so an instance
	// with its own DATABASE_PATH never collides with the default one.
	stateDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("create data folder %s: %w", stateDir, err)
	}
	pidPath := filepath.Join(stateDir, desktopPIDName)
	addrPath := filepath.Join(stateDir, desktopAddrName)

	if addr, ok := runningDesktopAddr(pidPath, addrPath); ok {
		url := "http://" + addr
		fmt.Printf("CH-UI desktop is already running at %s\n", url)
		if !desktopNoBrowser {
			openBrowserOrHint(url)
		}
		return nil
	}

	ln, err := listenDesktop(host, desktopPort, cmd.Flags().Changed("port"))
	if err != nil {
		return err
	}
	defer ln.Close()
	addr := ln.Addr().String()
	port := ln.Addr().(*net.TCPAddr).Port

	cfg := config.Load(desktopConfig)
	cfg.DatabasePath = dbPath
	cfg.Port = port
	cfg.AppURL = "http://" + addr
	cfg.TunnelURL = "ws://" + addr + "/connect"
	// Loopback needs no TLS, and nothing should proxy a personal instance:
	// trust no X-Forwarded-For.
	cfg.TLSCertFile = ""
	cfg.TLSKeyFile = ""
	cfg.TrustedProxies = []string{}
	cfg.DevMode = false
	if cmd.Flags().Changed("clickhouse-url") {
		cfg.ClickHouseURL = strings.TrimSpace(desktopClickHouse)
	}
	if cmd.Flags().Changed("connection-name") {
		cfg.ConnectionName = strings.TrimSpace(desktopConnectionName)
	}

	if err := writeDesktopState(pidPath, addrPath, os.Getpid(), addr); err != nil {
		return err
	}
	defer cleanupDesktopState(pidPath, addrPath, os.Getpid())

	fmt.Printf("CH-UI desktop is running at %s\n", cfg.AppURL)
	fmt.Printf("Data folder: %s\n", stateDir)
	fmt.Println("Press Ctrl+C to stop.")
	// The listener is already bound, so the browser's request waits in the
	// accept queue until the server is ready.
	if !desktopNoBrowser {
		openBrowserOrHint(cfg.AppURL)
	}

	return serveConfig(cfg, ln)
}

// loopbackHost validates the desktop listen host and returns it normalized.
// Anything that is not a loopback address is refused, so a personal
// instance is never reachable from the network.
func loopbackHost(raw string) (string, error) {
	h := strings.TrimSpace(raw)
	h = strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")
	if strings.EqualFold(h, "localhost") {
		return "127.0.0.1", nil
	}
	ip, err := netip.ParseAddr(h)
	if err != nil || !ip.Unmap().IsLoopback() {
		return "", fmt.Errorf("ch-ui desktop only listens on a loopback address such as 127.0.0.1, not %q; use ch-ui server to serve on the network", raw)
	}
	return ip.Unmap().String(), nil
}

// desktopDataDir returns the per-user CH-UI folder. It follows the rules of
// os.UserConfigDir with the environment injected, so each OS can be tested
// from any OS.
func desktopDataDir(goos string, getenv func(string) string) (string, error) {
	var base string
	switch goos {
	case "windows":
		base = getenv("AppData")
		if base == "" {
			return "", errors.New("%AppData% is not set; set DATABASE_PATH to choose where CH-UI keeps its data")
		}
	case "darwin", "ios":
		home := getenv("HOME")
		if home == "" {
			return "", errors.New("$HOME is not set; set DATABASE_PATH to choose where CH-UI keeps its data")
		}
		base = filepath.Join(home, "Library", "Application Support")
	default:
		base = getenv("XDG_CONFIG_HOME")
		if base == "" || !filepath.IsAbs(base) {
			home := getenv("HOME")
			if home == "" {
				return "", errors.New("neither $XDG_CONFIG_HOME nor $HOME is set; set DATABASE_PATH to choose where CH-UI keeps its data")
			}
			base = filepath.Join(home, ".config")
		}
	}
	return filepath.Join(base, "ch-ui"), nil
}

// listenDesktop binds host:port. Unless the port was asked for explicitly,
// a busy port falls back to any free one.
func listenDesktop(host string, port int, strict bool) (net.Listener, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		if strict {
			return nil, fmt.Errorf("listen on %s port %d: %w", host, port, err)
		}
		ln, err = net.Listen("tcp", net.JoinHostPort(host, "0"))
		if err != nil {
			return nil, fmt.Errorf("listen on %s: %w", host, err)
		}
	}
	tcp, ok := ln.Addr().(*net.TCPAddr)
	if !ok || !tcp.IP.IsLoopback() {
		_ = ln.Close()
		return nil, fmt.Errorf("refusing to serve on non-loopback address %s", ln.Addr())
	}
	return ln, nil
}

// runningDesktopAddr returns the address of a live desktop instance. Its
// process must exist and its recorded loopback address must accept
// connections; otherwise the files are stale and are removed.
func runningDesktopAddr(pidPath, addrPath string) (string, bool) {
	pid, err := readServerPIDFile(pidPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			removeDesktopState(pidPath, addrPath)
		}
		return "", false
	}
	data, err := os.ReadFile(addrPath)
	addr := strings.TrimSpace(string(data))
	if err == nil && processExists(pid) && isLoopbackAddr(addr) && isTCPPortOpen(addr) {
		return addr, true
	}
	removeDesktopState(pidPath, addrPath)
	return "", false
}

func isLoopbackAddr(addr string) bool {
	ap, err := netip.ParseAddrPort(addr)
	return err == nil && ap.Port() != 0 && ap.Addr().Unmap().IsLoopback()
}

func writeDesktopState(pidPath, addrPath string, pid int, addr string) error {
	// Address first: whoever sees the PID file can rely on the address.
	if err := os.WriteFile(addrPath, []byte(addr+"\n"), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", addrPath, err)
	}
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", pid)), 0o600); err != nil {
		_ = os.Remove(addrPath)
		return fmt.Errorf("write %s: %w", pidPath, err)
	}
	return nil
}

// cleanupDesktopState removes the state files if they still belong to pid.
func cleanupDesktopState(pidPath, addrPath string, pid int) {
	if current, err := readServerPIDFile(pidPath); err == nil && current == pid {
		removeDesktopState(pidPath, addrPath)
	}
}

func removeDesktopState(pidPath, addrPath string) {
	_ = os.Remove(pidPath)
	_ = os.Remove(addrPath)
}

// browserCommand is the program that opens url in the default browser. It
// runs without a shell, so the URL is never interpreted.
func browserCommand(goos, url string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{url}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		return "xdg-open", []string{url}
	}
}

func openBrowserOrHint(url string) {
	name, args := browserCommand(runtime.GOOS, url)
	c := exec.Command(name, args...)
	if err := c.Start(); err != nil {
		fmt.Printf("Could not open a browser (%v); open %s yourself.\n", err, url)
		return
	}
	go func() { _ = c.Wait() }()
}
