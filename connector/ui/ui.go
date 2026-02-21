package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
)

// UI handles formatted terminal output
type UI struct {
	out      io.Writer
	noColor  bool
	quiet    bool
	verbose  bool
	jsonMode bool

	green   *color.Color
	red     *color.Color
	yellow  *color.Color
	cyan    *color.Color
	blue    *color.Color
	magenta *color.Color
	bold    *color.Color
	dim     *color.Color
}

// New creates a new UI instance
func New(noColor, quiet, verbose, jsonMode bool) *UI {
	if noColor {
		color.NoColor = true
	}

	return &UI{
		out:      os.Stdout,
		noColor:  noColor,
		quiet:    quiet,
		verbose:  verbose,
		jsonMode: jsonMode,
		green:    color.New(color.FgGreen),
		red:      color.New(color.FgRed),
		yellow:   color.New(color.FgYellow),
		cyan:     color.New(color.FgCyan),
		blue:     color.New(color.FgBlue),
		magenta:  color.New(color.FgMagenta),
		bold:     color.New(color.Bold),
		dim:      color.New(color.Faint),
	}
}

// Logo prints the CH-UI ASCII art logo
func (u *UI) Logo(version string) {
	if u.quiet || u.jsonMode {
		return
	}

	logo := `
  ██████╗██╗  ██╗      ██╗   ██╗██╗
 ██╔════╝██║  ██║      ██║   ██║██║
 ██║     ███████║█████╗██║   ██║██║
 ██║     ██╔══██║╚════╝██║   ██║██║
 ╚██████╗██║  ██║      ╚██████╔╝██║
  ╚═════╝╚═╝  ╚═╝       ╚═════╝ ╚═╝`

	u.cyan.Println(logo)
	u.dim.Printf("  Tunnel %s\n\n", version)
}

// Info prints an info message
func (u *UI) Info(format string, args ...interface{}) {
	if u.quiet || u.jsonMode {
		return
	}
	u.cyan.Print("→ ")
	fmt.Fprintf(u.out, format+"\n", args...)
}

// Success prints a success message
func (u *UI) Success(format string, args ...interface{}) {
	if u.quiet || u.jsonMode {
		return
	}
	u.green.Print("✓ ")
	fmt.Fprintf(u.out, format+"\n", args...)
}

// Error prints an error message
func (u *UI) Error(format string, args ...interface{}) {
	if u.jsonMode {
		return
	}
	u.red.Print("✗ ")
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// ErrorType represents the category of error
type ErrorType string

const (
	ErrorTypeNetwork ErrorType = "NETWORK"
	ErrorTypeAuth    ErrorType = "AUTH"
	ErrorTypeServer  ErrorType = "SERVER"
	ErrorTypeConfig  ErrorType = "CONFIG"
	ErrorTypeUnknown ErrorType = "UNKNOWN"
)

// DiagnosticError prints a detailed error with source, type, and suggestions
func (u *UI) DiagnosticError(errType ErrorType, source, message string, suggestions []string) {
	if u.jsonMode {
		return
	}

	fmt.Fprintln(os.Stderr)

	// Error header with type badge
	u.red.Fprint(os.Stderr, "┌─ ERROR ")
	u.dim.Fprintf(os.Stderr, "[%s]\n", errType)

	// Source
	u.red.Fprint(os.Stderr, "│\n")
	u.red.Fprint(os.Stderr, "│  ")
	u.bold.Fprint(os.Stderr, "Source: ")
	fmt.Fprintln(os.Stderr, source)

	// Message
	u.red.Fprint(os.Stderr, "│  ")
	u.bold.Fprint(os.Stderr, "Error:  ")
	fmt.Fprintln(os.Stderr, message)

	// Suggestions
	if len(suggestions) > 0 {
		u.red.Fprint(os.Stderr, "│\n")
		u.red.Fprint(os.Stderr, "│  ")
		u.yellow.Fprintln(os.Stderr, "Possible causes:")
		for _, s := range suggestions {
			u.red.Fprint(os.Stderr, "│    ")
			u.dim.Fprint(os.Stderr, "• ")
			fmt.Fprintln(os.Stderr, s)
		}
	}

	u.red.Fprint(os.Stderr, "│\n")
	u.red.Fprintln(os.Stderr, "└─")
	fmt.Fprintln(os.Stderr)
}

// AuthError prints an authentication-specific error with helpful context
func (u *UI) AuthError(serverMessage string) {
	source := "CH-UI Server"
	var suggestions []string

	// Classify the error and provide specific suggestions
	switch {
	case strings.Contains(strings.ToLower(serverMessage), "invalid") && strings.Contains(strings.ToLower(serverMessage), "token"):
		suggestions = []string{
			"The tunnel token is invalid or has been revoked",
			"Check that you copied the complete token (starts with 'cht_')",
			"Generate a new token on the server with: ch-ui tunnel create --name <connection-name>",
			"Verify the token belongs to the target server instance",
		}
	case strings.Contains(strings.ToLower(serverMessage), "license"):
		suggestions = []string{
			"The server license may have expired",
			"Contact your administrator to renew the license",
			"Check server logs for license validation details",
		}
	case strings.Contains(strings.ToLower(serverMessage), "already connected"):
		suggestions = []string{
			"Another agent process is already connected with this token",
			"Stop the existing process or service before starting a new one",
			"Use 'ch-ui service status' to check service mode",
			"Reconnect with '--takeover' to replace the active session",
		}
	case strings.Contains(strings.ToLower(serverMessage), "not found"):
		suggestions = []string{
			"The organization associated with this token may have been deleted",
			"The tunnel connection may have been removed",
			"Contact your administrator",
		}
	default:
		suggestions = []string{
			"Check that your token is valid and not expired",
			"Verify the tunnel URL is correct",
			"Check CH-UI server logs for tunnel auth errors",
		}
	}

	u.DiagnosticError(ErrorTypeAuth, source, serverMessage, suggestions)
}

// ConnectionError prints a connection-specific error
func (u *UI) ConnectionError(err error, tunnelURL string) {
	source := fmt.Sprintf("Connection to %s", tunnelURL)
	message := err.Error()
	var suggestions []string

	switch {
	case strings.Contains(message, "connection refused"):
		suggestions = []string{
			"The CH-UI server may be down or unreachable",
			"Check if the tunnel URL is correct: " + tunnelURL,
			"Verify your network/firewall allows outbound WebSocket connections",
			"If using a custom server, ensure it's running",
		}
	case strings.Contains(message, "no such host") || strings.Contains(message, "lookup"):
		suggestions = []string{
			"Cannot resolve the tunnel server hostname",
			"Check your DNS settings",
			"Verify the tunnel URL is correct: " + tunnelURL,
		}
	case strings.Contains(message, "timeout") || strings.Contains(message, "deadline"):
		suggestions = []string{
			"Connection timed out - server may be overloaded or unreachable",
			"Check your network connection",
			"Try again in a few moments",
		}
	case strings.Contains(message, "certificate") || strings.Contains(message, "tls"):
		suggestions = []string{
			"SSL/TLS certificate error",
			"If using a self-signed certificate, this is expected in dev mode",
			"Verify the tunnel URL protocol (ws:// vs wss://)",
		}
	default:
		suggestions = []string{
			"Check your network connection",
			"Verify the tunnel URL: " + tunnelURL,
			"Try running with --verbose for more details",
		}
	}

	u.DiagnosticError(ErrorTypeNetwork, source, message, suggestions)
}

// Warn prints a warning message
func (u *UI) Warn(format string, args ...interface{}) {
	if u.quiet || u.jsonMode {
		return
	}
	u.yellow.Print("! ")
	fmt.Fprintf(u.out, format+"\n", args...)
}

// Debug prints a debug message (only in verbose mode)
func (u *UI) Debug(format string, args ...interface{}) {
	if !u.verbose || u.jsonMode {
		return
	}
	u.dim.Printf("[debug] "+format+"\n", args...)
}

// Status prints the connection status block
func (u *UI) Status(tunnelURL, clickhouseURL string, uptime time.Duration) {
	if u.quiet || u.jsonMode {
		return
	}

	fmt.Println()
	u.bold.Println("  Status:     ", u.green.Sprint("Connected"))
	fmt.Printf("  Tunnel:     %s\n", tunnelURL)
	fmt.Printf("  ClickHouse: %s\n", clickhouseURL)
	fmt.Printf("  Uptime:     %s\n", formatDuration(uptime))
	fmt.Println()
	u.dim.Println("Press Ctrl+C to disconnect")
	fmt.Println()
}

// QueryLog prints a query execution log line
func (u *UI) QueryLog(queryID string, elapsed time.Duration, rows int) {
	if u.quiet || u.jsonMode {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	u.dim.Printf("[%s] ", timestamp)
	fmt.Printf("Query %s executed ", u.cyan.Sprint(queryID[:8]))
	u.dim.Printf("(%s, %s rows)\n", elapsed.Round(time.Millisecond), formatNumber(rows))
}

// QueryError prints a query error log line
func (u *UI) QueryError(queryID string, err error) {
	if u.jsonMode {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	u.dim.Printf("[%s] ", timestamp)
	u.red.Printf("Query %s failed: %v\n", queryID[:8], err)
}

// Disconnected prints a disconnection message
func (u *UI) Disconnected(reason string) {
	if u.jsonMode {
		return
	}
	u.yellow.Print("! ")
	fmt.Printf("Disconnected: %s\n", reason)
}

// Reconnecting prints a reconnection message
func (u *UI) Reconnecting(delay time.Duration) {
	if u.quiet || u.jsonMode {
		return
	}
	u.cyan.Print("→ ")
	fmt.Printf("Reconnecting in %s...\n", delay.Round(time.Millisecond))
}

// Box prints a boxed message
func (u *UI) Box(title string, lines map[string]string, order []string) {
	if u.quiet || u.jsonMode {
		return
	}

	fmt.Println()
	u.bold.Println(title)
	fmt.Println(strings.Repeat("─", len(title)+2))

	for _, key := range order {
		if val, ok := lines[key]; ok {
			fmt.Printf("  %-12s %s\n", key+":", val)
		}
	}
	fmt.Println()
}

// Helpers

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

func formatNumber(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1000000)
}

// FormatBytes formats bytes to human readable format
func FormatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
