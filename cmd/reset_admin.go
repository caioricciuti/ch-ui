package cmd

import (
	"fmt"
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
who signs in with SSO.

Safe to run while the server is up. Needs access to the server's database
file, so it is run on the server host, or with "docker exec" in a container.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Load(resetAdminConfigPath)
		path := cfg.DatabasePath
		if resetAdminDatabasePath != "" {
			path = resetAdminDatabasePath
		}
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
		if user := strings.TrimSpace(resetAdminUser); user != "" {
			if err := db.SetUserRole(user, "admin"); err != nil {
				return err
			}
			if err := db.SetSessionsUserRole(user, "admin"); err != nil {
				return err
			}
			_ = db.CreateAuditLog(database.AuditLogParams{
				Action:   "user_role.reset_admin",
				Username: &actor,
				Details:  strPtr("Granted admin to " + user + " from the command line"),
			})
			fmt.Printf("%s is now a CH-UI admin.\n", user)
			return nil
		}

		revoked, err := db.RevokeAllAdmins()
		if err != nil {
			return err
		}
		if len(revoked) == 0 {
			fmt.Println("No CH-UI admin was assigned. The next ClickHouse admin who signs in becomes the CH-UI admin.")
			return nil
		}
		_ = db.CreateAuditLog(database.AuditLogParams{
			Action:   "user_role.reset_admin",
			Username: &actor,
			Details:  strPtr("Removed admin from " + strings.Join(revoked, ", ") + " from the command line"),
		})
		fmt.Printf("Removed CH-UI admin from: %s\n", strings.Join(revoked, ", "))
		fmt.Println("The next ClickHouse admin who signs in becomes the CH-UI admin.")
		return nil
	},
}

func strPtr(s string) *string { return &s }

func init() {
	resetAdminCmd.Flags().StringVarP(&resetAdminConfigPath, "config", "c", "", "Path to config file")
	resetAdminCmd.Flags().StringVar(&resetAdminDatabasePath, "database-path", "", "Path to the database (overrides config)")
	resetAdminCmd.Flags().StringVar(&resetAdminUser, "user", "", "Make this user an admin instead of clearing admins")
	rootCmd.AddCommand(resetAdminCmd)
}
