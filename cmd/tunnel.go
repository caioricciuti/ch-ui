package cmd

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	serverconfig "github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/license"
	"github.com/spf13/cobra"
)

var (
	tunnelConfigPath  string
	tunnelDBPath      string
	tunnelURLOverride string

	tunnelCreateName string
	tunnelShowToken  bool

	tunnelDeleteForce bool
)

var tunnelCmd = &cobra.Command{
	Use:   "tunnel",
	Short: "Manage tunnel keys for remote ClickHouse agents",
	Long: `Create and manage tunnel connection keys in this CH-UI server database.
Run these commands on the server host (VM where CH-UI server stores its SQLite DB)
to bootstrap remote agents from other machines (VM2, VM3, ...).`,
}

var tunnelCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new tunnel connection and token",
	RunE: func(cmd *cobra.Command, args []string) error {
		name := strings.TrimSpace(tunnelCreateName)
		if name == "" {
			return errors.New("connection name is required (use --name)")
		}

		db, cfg, err := openTunnelDB()
		if err != nil {
			return err
		}
		defer db.Close()

		token := license.GenerateTunnelToken()
		id, err := db.CreateConnection(name, token, false)
		if err != nil {
			return fmt.Errorf("create connection: %w", err)
		}

		conn, err := db.GetConnectionByID(id)
		if err != nil {
			return fmt.Errorf("connection created but failed to load: %w", err)
		}
		if conn == nil {
			return errors.New("connection created but failed to load: not found")
		}

		printTunnelConnectionInfo(cfg, *conn)
		return nil
	},
}

var tunnelListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tunnel connections",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, _, err := openTunnelDB()
		if err != nil {
			return err
		}
		defer db.Close()

		conns, err := db.GetConnections()
		if err != nil {
			return fmt.Errorf("list connections: %w", err)
		}

		if len(conns) == 0 {
			fmt.Println("No tunnel connections found.")
			fmt.Println("Create one with: ch-ui tunnel create --name <connection-name>")
			return nil
		}

		fmt.Printf("%-36s  %-22s  %-12s  %-8s  %-35s\n", "ID", "NAME", "STATUS", "EMBEDDED", "TOKEN")
		for _, c := range conns {
			token := maskToken(c.TunnelToken)
			if tunnelShowToken {
				token = c.TunnelToken
			}
			embedded := "no"
			if c.IsEmbedded {
				embedded = "yes"
			}
			fmt.Printf("%-36s  %-22s  %-12s  %-8s  %-35s\n",
				c.ID,
				truncate(c.Name, 22),
				c.Status,
				embedded,
				token,
			)
		}
		return nil
	},
}

var tunnelShowCmd = &cobra.Command{
	Use:   "show <connection-id>",
	Short: "Show token and setup instructions for a connection",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		db, cfg, err := openTunnelDB()
		if err != nil {
			return err
		}
		defer db.Close()

		connID := strings.TrimSpace(args[0])
		conn, err := db.GetConnectionByID(connID)
		if err != nil {
			return fmt.Errorf("load connection: %w", err)
		}
		if conn == nil {
			return fmt.Errorf("connection %q not found", connID)
		}

		printTunnelConnectionInfo(cfg, *conn)
		return nil
	},
}

var tunnelRotateCmd = &cobra.Command{
	Use:   "rotate <connection-id>",
	Short: "Rotate (regenerate) tunnel token for a connection",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		db, cfg, err := openTunnelDB()
		if err != nil {
			return err
		}
		defer db.Close()

		connID := strings.TrimSpace(args[0])
		conn, err := db.GetConnectionByID(connID)
		if err != nil {
			return fmt.Errorf("load connection: %w", err)
		}
		if conn == nil {
			return fmt.Errorf("connection %q not found", connID)
		}

		newToken := license.GenerateTunnelToken()
		if err := db.UpdateConnectionToken(connID, newToken); err != nil {
			return fmt.Errorf("rotate token: %w", err)
		}

		updated, err := db.GetConnectionByID(connID)
		if err != nil {
			return fmt.Errorf("token rotated but failed to reload connection: %w", err)
		}
		if updated == nil {
			return errors.New("token rotated but failed to reload connection: not found")
		}

		fmt.Println("Token rotated successfully. Previous token is now invalid.")
		printTunnelConnectionInfo(cfg, *updated)
		return nil
	},
}

var tunnelDeleteCmd = &cobra.Command{
	Use:   "delete <connection-id>",
	Short: "Delete a tunnel connection",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		db, _, err := openTunnelDB()
		if err != nil {
			return err
		}
		defer db.Close()

		connID := strings.TrimSpace(args[0])
		conn, err := db.GetConnectionByID(connID)
		if err != nil {
			return fmt.Errorf("load connection: %w", err)
		}
		if conn == nil {
			return fmt.Errorf("connection %q not found", connID)
		}
		if conn.IsEmbedded && !tunnelDeleteForce {
			return errors.New("refusing to delete embedded connection without --force")
		}

		if err := db.DeleteConnection(connID); err != nil {
			return fmt.Errorf("delete connection: %w", err)
		}

		fmt.Printf("Deleted connection %q (%s)\n", conn.Name, conn.ID)
		return nil
	},
}

func init() {
	tunnelCmd.PersistentFlags().StringVarP(&tunnelConfigPath, "config", "c", "", "Path to server config file")
	tunnelCmd.PersistentFlags().StringVar(&tunnelDBPath, "db", "", "Override SQLite database path")
	tunnelCmd.PersistentFlags().StringVar(&tunnelURLOverride, "url", "", "Public tunnel URL (ws:// or wss://) for setup output")

	tunnelCreateCmd.Flags().StringVar(&tunnelCreateName, "name", "", "Connection name (e.g. VM2 ClickHouse)")
	_ = tunnelCreateCmd.MarkFlagRequired("name")

	tunnelListCmd.Flags().BoolVar(&tunnelShowToken, "show-token", false, "Show full tunnel tokens")

	tunnelDeleteCmd.Flags().BoolVar(&tunnelDeleteForce, "force", false, "Force delete embedded connection")

	tunnelCmd.AddCommand(tunnelCreateCmd, tunnelListCmd, tunnelShowCmd, tunnelRotateCmd, tunnelDeleteCmd)
	rootCmd.AddCommand(tunnelCmd)
}

func openTunnelDB() (*database.DB, *serverconfig.Config, error) {
	cfg := serverconfig.Load(tunnelConfigPath)
	if strings.TrimSpace(tunnelDBPath) != "" {
		cfg.DatabasePath = strings.TrimSpace(tunnelDBPath)
	}

	db, err := database.Open(cfg.DatabasePath)
	if err != nil {
		return nil, nil, fmt.Errorf("open database %q: %w", cfg.DatabasePath, err)
	}
	return db, cfg, nil
}

func printTunnelConnectionInfo(cfg *serverconfig.Config, conn database.Connection) {
	tunnelURL := inferPublicTunnelURL(cfg)
	token := conn.TunnelToken

	connectCmd := fmt.Sprintf("ch-ui connect --url %s --key %s --clickhouse-url http://localhost:8123", tunnelURL, token)
	serviceCmd := fmt.Sprintf("ch-ui service install --url %s --key %s --clickhouse-url http://localhost:8123", tunnelURL, token)

	fmt.Println()
	fmt.Printf("Connection:         %s\n", conn.Name)
	fmt.Printf("Connection ID:      %s\n", conn.ID)
	fmt.Printf("Tunnel Token:       %s\n", token)
	fmt.Println()
	fmt.Println("Use on the ClickHouse host:")
	fmt.Printf("  %s\n", connectCmd)
	fmt.Println()
	fmt.Println("Run as service on the ClickHouse host:")
	fmt.Printf("  %s\n", serviceCmd)
	fmt.Println()

	if isLoopbackTunnelURL(tunnelURL) {
		fmt.Fprintf(os.Stderr, "Warning: tunnel URL %q is loopback/local. Set --url or APP_URL/TUNNEL_URL in server config for remote VM setup.\n", tunnelURL)
	}
}

func inferPublicTunnelURL(cfg *serverconfig.Config) string {
	if strings.TrimSpace(tunnelURLOverride) != "" {
		return websocketConnectURL(strings.TrimSpace(tunnelURLOverride))
	}

	configTunnelURL := strings.TrimSpace(cfg.TunnelURL)
	if configTunnelURL != "" && !isLoopbackTunnelURL(configTunnelURL) {
		return websocketConnectURL(configTunnelURL)
	}

	if appURL := strings.TrimSpace(cfg.AppURL); appURL != "" {
		return websocketConnectURL(appURL)
	}

	if configTunnelURL != "" {
		return websocketConnectURL(configTunnelURL)
	}

	return "ws://127.0.0.1:3488/connect"
}

func websocketConnectURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
		// already websocket scheme
	default:
		// keep as-is (can still be validated by caller command later)
	}

	path := strings.TrimRight(u.Path, "/")
	if path == "" {
		u.Path = "/connect"
	} else if !strings.HasSuffix(path, "/connect") {
		u.Path = path + "/connect"
	}

	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func isLoopbackTunnelURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		s := strings.ToLower(raw)
		return strings.Contains(s, "127.0.0.1") || strings.Contains(s, "localhost")
	}
	host := strings.ToLower(u.Hostname())
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func maskToken(token string) string {
	if len(token) <= 12 {
		return token
	}
	return token[:8] + "..." + token[len(token)-4:]
}

func truncate(s string, max int) string {
	if max < 4 || len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
