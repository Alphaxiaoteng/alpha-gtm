package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egolite"
)

func newEgoCommand(cfg *config.Config) *cobra.Command {
	egoCmd := &cobra.Command{
		Use:   "ego",
		Short: "Diagnose and manage ego-lite identities and Spaces",
	}
	var openIdentityID int64
	openCmd := &cobra.Command{
		Use:   "open",
		Short: "Open up to three configured platform pages in one identity Space",
		RunE: func(cmd *cobra.Command, _ []string) error {
			database, err := openEgoDB(*cfg)
			if err != nil {
				return err
			}
			defer database.Close()
			result, err := egolite.NewSpaceService(
				database,
				egolite.NewRuntime(cfg.EgoBrowserCommand, 0),
			).OpenLoggedInPlatforms(cmd.Context(), openIdentityID, 0)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	}
	openCmd.Flags().Int64Var(&openIdentityID, "identity-id", 0, "logical browser identity ID")
	_ = openCmd.MarkFlagRequired("identity-id")
	var loginCheckAccountID int64
	loginCheckCmd := &cobra.Command{
		Use:   "login-check",
		Short: "Open one platform login page and safely report QR availability",
		RunE: func(cmd *cobra.Command, _ []string) error {
			database, err := openEgoDB(*cfg)
			if err != nil {
				return err
			}
			defer database.Close()
			service := egolite.NewSpaceService(database, egolite.NewRuntime(cfg.EgoBrowserCommand, 0))
			result, err := service.OpenLoginQR(cmd.Context(), loginCheckAccountID, true)
			if err != nil {
				return err
			}
			if _, err := service.ReleaseLoginQR(cmd.Context(), loginCheckAccountID); err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
				"account_id": result.AccountID, "platform_code": result.PlatformCode,
				"state": result.State, "image_available": result.ImageDataURL != "",
				"page_origin": result.PageOrigin, "expires_at": result.ExpiresAt,
			})
		},
	}
	loginCheckCmd.Flags().Int64Var(&loginCheckAccountID, "account-id", 0, "platform account ID")
	_ = loginCheckCmd.MarkFlagRequired("account-id")
	var verifyIdentityID int64
	var verifyPlatforms []string
	verifyCmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify selected platform sessions in one Ego identity and persist public profile metadata",
		RunE: func(cmd *cobra.Command, _ []string) error {
			database, err := openEgoDB(*cfg)
			if err != nil {
				return err
			}
			defer database.Close()
			result, err := egolite.NewSpaceService(
				database,
				egolite.NewRuntime(cfg.EgoBrowserCommand, 0),
			).VerifyLoginBatch(cmd.Context(), verifyIdentityID, verifyPlatforms)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	}
	verifyCmd.Flags().Int64Var(&verifyIdentityID, "identity-id", 0, "logical browser identity ID")
	verifyCmd.Flags().StringSliceVar(&verifyPlatforms, "platform", nil, "platform code (repeatable or comma-separated; at most three per run)")
	_ = verifyCmd.MarkFlagRequired("identity-id")
	_ = verifyCmd.MarkFlagRequired("platform")
	egoCmd.AddCommand(
		&cobra.Command{
			Use:   "doctor",
			Short: "Verify the required ego-browser helper contract",
			RunE: func(cmd *cobra.Command, _ []string) error {
				result, err := egolite.NewRuntime(cfg.EgoBrowserCommand, 0).Doctor(cmd.Context())
				if result != nil {
					_ = json.NewEncoder(cmd.OutOrStdout()).Encode(result)
				}
				return err
			},
		},
		openCmd,
		loginCheckCmd,
		verifyCmd,
		&cobra.Command{
			Use:   "identities",
			Short: "List logical Ego identities",
			RunE: func(cmd *cobra.Command, _ []string) error {
				database, err := openEgoDB(*cfg)
				if err != nil {
					return err
				}
				defer database.Close()
				identities, err := database.ListEgoIdentities()
				if err != nil {
					return err
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(identities)
			},
		},
		&cobra.Command{
			Use:   "spaces",
			Short: "List account-bound ego Spaces",
			RunE: func(cmd *cobra.Command, _ []string) error {
				database, err := openEgoDB(*cfg)
				if err != nil {
					return err
				}
				defer database.Close()
				spaces, err := database.ListEgoSpaces()
				if err != nil {
					return err
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(spaces)
			},
		},
		&cobra.Command{
			Use:   "calibrate",
			Short: "Measure safe local concurrent Space capacity",
			RunE: func(cmd *cobra.Command, _ []string) error {
				runtime := egolite.NewRuntime(cfg.EgoBrowserCommand, 0)
				if _, err := runtime.Doctor(cmd.Context()); err != nil {
					return err
				}
				result, err := egolite.NewCalibrator(egolite.NewRuntimeProbe(runtime)).Run(cmd.Context())
				if err != nil {
					return err
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			},
		},
	)
	return egoCmd
}

func openEgoDB(cfg config.Config) (*db.DB, error) {
	dataDir, err := cfg.ResolvedDataDir()
	if err != nil {
		return nil, err
	}
	database, err := db.Open(dataDir)
	if err != nil {
		return nil, err
	}
	if err := database.Migrate(); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	return database, nil
}
