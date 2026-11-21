package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"alpha-nexus-egolite/internal/batch"
	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/egolite"
	"alpha-nexus-egolite/internal/errors"
)

// runAccountLoginHandoff opens the account's isolated Ego Space and delegates it
// to the user. No browser process, profile, cookie, or CDP endpoint is managed.
func runAccountLoginHandoff(cmd *cobra.Command, cfg config.Config, accountID int64) error {
	if accountID <= 0 {
		return fmt.Errorf("%s: --id is required", errors.INVALID_ACCOUNT_ID)
	}

	database, _, err := openDB(cmd, cfg)
	if err != nil {
		return err
	}
	defer database.Close()

	account, err := database.GetAccount(accountID)
	if err != nil {
		return fmt.Errorf("%s: %w", errors.ACCOUNT_NOT_FOUND, err)
	}

	batch.EmitNDJSON(map[string]interface{}{
		"event":     "account.login_handoff.started",
		"accountId": accountID,
		"platform":  account.PlatformCode,
		"ts":        time.Now().UTC().Format(time.RFC3339),
	})

	space, err := egolite.NewSpaceService(
		database,
		egolite.NewRuntime(cfg.EgoBrowserCommand, 0),
	).OpenLoginAndHandoff(cmd.Context(), accountID)
	if err != nil {
		return err
	}

	batch.EmitNDJSON(map[string]interface{}{
		"event":     "account.login_handoff.ready",
		"accountId": accountID,
		"spaceId":   space.SpaceID,
		"ownership": space.Ownership,
		"state":     space.State,
		"platform":  account.PlatformCode,
		"ts":        time.Now().UTC().Format(time.RFC3339),
	})

	cmd.Printf("Login handoff ready for account #%d (%s / %s)\n", accountID, account.PlatformCode, account.Name)
	cmd.Printf("  Space:     %d\n", space.SpaceID)
	cmd.Printf("  Ownership: %s\n", space.Ownership)
	cmd.Printf("  State:     %s\n", space.State)
	cmd.Println()
	cmd.Println("1. Complete login, QR code, or 2FA in the delegated EgoLite Space.")
	cmd.Println("2. Return control from the EgoLite task UI.")
	cmd.Println("3. Then run the passive snapshot check:")
	cmd.Printf("   alpha-nexus-egolite account check --id %d\n", accountID)
	return nil
}
