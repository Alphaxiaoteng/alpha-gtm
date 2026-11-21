package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"alpha-nexus-egolite/internal/auth"
)

func addAuthCmd(root *cobra.Command) {
	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "Local tool login users (intranet demo)",
		Long: `Manage app_users for UI/API login (NOT social platform accounts).

Demo:
  alpha-nexus auth bootstrap --username demo --password demo123
  alpha-nexus-egolite ui serve
  # open UI → login with demo / demo123
  # or: curl -X POST .../api/auth/login -d '{"username":"demo","password":"demo123"}'
`,
	}

	bootstrap := &cobra.Command{
		Use:   "bootstrap",
		Short: "Create first admin user and JWT secret (enables API auth)",
		RunE: func(cmd *cobra.Command, args []string) error {
			user, _ := cmd.Flags().GetString("username")
			pass, _ := cmd.Flags().GetString("password")
			if user == "" || pass == "" {
				return fmt.Errorf("--username and --password required")
			}
			if len(pass) < 6 {
				return fmt.Errorf("password min 6 chars")
			}
			d := openQuickDB()
			defer d.Close()
			n, err := d.CountAppUsers()
			if err != nil {
				return err
			}
			if n > 0 {
				return fmt.Errorf("users already exist (%d); use auth user-add", n)
			}
			hash, err := auth.HashPassword(pass)
			if err != nil {
				return err
			}
			u, err := d.CreateAppUser(user, hash, "admin")
			if err != nil {
				return err
			}
			if _, err := auth.LoadOrCreateSecret(d.DataDir); err != nil {
				return err
			}
			cmd.Printf("Auth bootstrap OK\n")
			cmd.Printf("  user:     %s (id=%d)\n", u.Username, u.ID)
			cmd.Printf("  data-dir: %s\n", d.DataDir)
			cmd.Printf("  secret:   %s/auth_secret\n", d.DataDir)
			cmd.Printf("\nNext: alpha-nexus-egolite ui serve\nLogin: %s / (your password)\n", user)
			return nil
		},
	}
	bootstrap.Flags().String("username", "demo", "Admin username")
	bootstrap.Flags().String("password", "", "Admin password")
	bootstrap.MarkFlagRequired("password")

	userAdd := &cobra.Command{
		Use:   "user-add",
		Short: "Add another app user",
		RunE: func(cmd *cobra.Command, args []string) error {
			user, _ := cmd.Flags().GetString("username")
			pass, _ := cmd.Flags().GetString("password")
			role, _ := cmd.Flags().GetString("role")
			if user == "" || pass == "" {
				return fmt.Errorf("--username and --password required")
			}
			d := openQuickDB()
			defer d.Close()
			hash, err := auth.HashPassword(pass)
			if err != nil {
				return err
			}
			u, err := d.CreateAppUser(user, hash, role)
			if err != nil {
				return err
			}
			cmd.Printf("User created: %s (#%d) role=%s\n", u.Username, u.ID, u.Role)
			return nil
		},
	}
	userAdd.Flags().String("username", "", "Username")
	userAdd.Flags().String("password", "", "Password")
	userAdd.Flags().String("role", "admin", "Role")
	userAdd.MarkFlagRequired("username")
	userAdd.MarkFlagRequired("password")

	userList := &cobra.Command{
		Use:   "user-list",
		Short: "List app users",
		RunE: func(cmd *cobra.Command, args []string) error {
			d := openQuickDB()
			defer d.Close()
			users, err := d.ListAppUsers()
			if err != nil {
				return err
			}
			if len(users) == 0 {
				cmd.Println("No users. Run: auth bootstrap --username demo --password demo123")
				return nil
			}
			for _, u := range users {
				en := "yes"
				if !u.Enabled {
					en = "no"
				}
				cmd.Printf("%d\t%s\t%s\tenabled=%s\n", u.ID, u.Username, u.Role, en)
			}
			return nil
		},
	}

	status := &cobra.Command{
		Use:   "status",
		Short: "Show whether API auth is active",
		RunE: func(cmd *cobra.Command, args []string) error {
			d := openQuickDB()
			defer d.Close()
			n, err := d.CountAppUsers()
			if err != nil {
				return err
			}
			cmd.Printf("auth_required=%v user_count=%d data_dir=%s\n", n > 0, n, d.DataDir)
			return nil
		},
	}

	authCmd.AddCommand(bootstrap, userAdd, userList, status)
	root.AddCommand(authCmd)
}
