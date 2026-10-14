package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
)

var (
	resetAdminConfigPath   string
	resetAdminDatabasePath string
	resetAdminUser         string
	resetAdminConnection   string
)

var resetAdminCmd = &cobra.Command{
	Use:   "reset-admin",
	Short: "Recover the CH-UI admin role",
	Long: `Recover the CH-UI admin role when nobody who holds it can sign in.

With no flags, removes every CH-UI admin assignment. The next ClickHouse admin
who signs in becomes the CH-UI admin, as on a fresh install. Other roles
(analyst, viewer) are kept.

With --user, makes that user an admin instead and leaves existing admins alone.
Use the ClickHouse username for a password login, or sso:<email> for someone
who signs in with SSO. A ClickHouse user's role applies per connection: add
--connection (id or name) to pick one, or leave it out to grant admin on every
connection except those added from the login page, which never grant admin.

Safe to run while the server is up. Needs access to the server's database
file, so it is run on the server host, or with "docker exec" in a container.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Load(resetAdminConfigPath)
		path := cfg.DatabasePath
		if resetAdminDatabasePath != "" {
			path = resetAdminDatabasePath
		}
		return runResetAdmin(cmd.OutOrStdout(), path, resetAdminUser, resetAdminConnection)
	},
}

// runResetAdmin does the work of reset-admin against the database at path,
// writing the user-facing result to out. Split from RunE so it can be tested
// without flags or a config file.
func runResetAdmin(out io.Writer, path, user, connection string) error {
	// database.Open creates a missing file, which would hide a wrong path.
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("database not found at %q: %w", path, err)
	}

	db, err := database.Open(path)
	if err != nil {
		return err
	}
	defer db.Close()

	actor := "cli:reset-admin"
	if user := strings.TrimSpace(user); user != "" {
		if database.IsSSORoleKey(user) {
			if err := db.SetUserRole(user, "", "admin"); err != nil {
				return err
			}
			if err := db.SetSessionsUserRole(user, "", "admin"); err != nil {
				return err
			}
			_ = db.CreateAuditLog(database.AuditLogParams{
				Action:   "user_role.reset_admin",
				Username: &actor,
				Details:  strPtr("Granted admin to " + user + " from the command line"),
			})
			fmt.Fprintf(out, "%s is now a CH-UI admin.\n", user)
			return nil
		}

		conns, err := resetAdminConnections(db, strings.TrimSpace(connection))
		if err != nil {
			return err
		}
		names := make([]string, 0, len(conns))
		for _, c := range conns {
			if err := db.SetUserRole(user, c.ID, "admin"); err != nil {
				return err
			}
			if err := db.SetSessionsUserRole(user, c.ID, "admin"); err != nil {
				return err
			}
			names = append(names, c.Name)
			_ = db.CreateAuditLog(database.AuditLogParams{
				Action:       "user_role.reset_admin",
				Username:     &actor,
				ConnectionID: strPtr(c.ID),
				Details:      strPtr(fmt.Sprintf("Granted admin to %s on connection %q from the command line", user, c.Name)),
			})
		}
		fmt.Fprintf(out, "%s is now a CH-UI admin on: %s\n", user, strings.Join(names, ", "))
		return nil
	}

	revoked, err := db.RevokeAllAdmins()
	if err != nil {
		return err
	}
	if len(revoked) == 0 {
		fmt.Fprintln(out, "No CH-UI admin was assigned. The next ClickHouse admin who signs in becomes the CH-UI admin.")
		return nil
	}
	labels := make([]string, 0, len(revoked))
	for _, r := range revoked {
		labels = append(labels, r.String())
	}
	_ = db.CreateAuditLog(database.AuditLogParams{
		Action:   "user_role.reset_admin",
		Username: &actor,
		Details:  strPtr("Removed admin from " + strings.Join(labels, ", ") + " from the command line"),
	})
	fmt.Fprintf(out, "Removed CH-UI admin from: %s\n", strings.Join(labels, ", "))
	fmt.Fprintln(out, "The next ClickHouse admin who signs in becomes the CH-UI admin.")
	return nil
}

// resetAdminConnections returns the connections a ClickHouse user is made
// admin on: the one named by id or name, or every connection not added from
// the login page.
func resetAdminConnections(db *database.DB, connection string) ([]database.Connection, error) {
	all, err := db.GetConnections()
	if err != nil {
		return nil, err
	}
	var out []database.Connection
	for _, c := range all {
		if connection != "" && c.ID != connection && c.Name != connection {
			continue
		}
		loginURL, err := db.IsLoginURLConnection(c.ID)
		if err != nil {
			return nil, err
		}
		if loginURL {
			if connection != "" {
				return nil, fmt.Errorf("connection %q was added from the login page and cannot grant CH-UI admin", connection)
			}
			continue
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		if connection != "" {
			return nil, fmt.Errorf("connection %q not found", connection)
		}
		return nil, fmt.Errorf("no connection to grant admin on; add one first")
	}
	if connection != "" && len(out) > 1 {
		return nil, fmt.Errorf("more than one connection is named %q; pass its id", connection)
	}
	return out, nil
}

func strPtr(s string) *string { return &s }

func init() {
	resetAdminCmd.Flags().StringVarP(&resetAdminConfigPath, "config", "c", "", "Path to config file")
	resetAdminCmd.Flags().StringVar(&resetAdminDatabasePath, "database-path", "", "Path to the database (overrides config)")
	resetAdminCmd.Flags().StringVar(&resetAdminUser, "user", "", "Make this user an admin instead of clearing admins")
	resetAdminCmd.Flags().StringVar(&resetAdminConnection, "connection", "", "With --user and a ClickHouse user: the connection (id or name) to grant admin on; default every connection not added from the login page")
	rootCmd.AddCommand(resetAdminCmd)
}
