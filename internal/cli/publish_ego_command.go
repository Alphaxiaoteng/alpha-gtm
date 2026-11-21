package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/ui"
)

func addEgoPublishCmd(root *cobra.Command) {
	publish := &cobra.Command{
		Use:   "publish",
		Short: "Execute an approved frozen batch through EgoLite Spaces",
	}
	addPublishPlanHandoffCmd(publish)
	batch := &cobra.Command{
		Use:   "batch",
		Short: "Prepare or submit one approved frozen publish plan",
		RunE: func(cmd *cobra.Command, _ []string) error {
			planID, _ := cmd.Flags().GetInt64("plan-id")
			snapshotHash, _ := cmd.Flags().GetString("snapshot-hash")
			approvalID, _ := cmd.Flags().GetString("approval-id")
			nonce, _ := cmd.Flags().GetString("nonce")
			action, _ := cmd.Flags().GetString("action")
			if planID <= 0 || strings.TrimSpace(snapshotHash) == "" ||
				strings.TrimSpace(approvalID) == "" || strings.TrimSpace(nonce) == "" {
				return fmt.Errorf("plan-id, snapshot-hash, approval-id and nonce are required")
			}
			cfg := activeCfg
			if cfg.DataDir == "" {
				cfg = config.DefaultConfig()
			}
			server, err := ui.NewServer(cfg)
			if err != nil {
				return err
			}
			defer server.Close()

			var result any
			switch strings.ToLower(strings.TrimSpace(action)) {
			case "", "prepare":
				result, err = server.PrepareApprovedPublishPlan(cmd.Context(), planID, snapshotHash, approvalID, nonce)
			case "submit":
				result, err = server.SubmitApprovedPublishPlan(cmd.Context(), planID, snapshotHash, approvalID, nonce)
			case "acceptance_submit", "acceptance-submit":
				result, err = server.SubmitAcceptancePublishPlan(cmd.Context(), planID, snapshotHash, approvalID, nonce)
			default:
				return fmt.Errorf("action must be prepare, submit, or acceptance_submit")
			}
			if result != nil {
				_ = json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			}
			return err
		},
	}
	batch.Flags().Int64("plan-id", 0, "Frozen publish plan ID")
	batch.Flags().String("snapshot-hash", "", "Frozen plan snapshot hash")
	batch.Flags().String("approval-id", "", "Single-use local approval ID")
	batch.Flags().String("nonce", "", "Single-use local approval nonce")
	batch.Flags().String("action", "prepare", "prepare|submit|acceptance_submit")
	publish.AddCommand(batch)

	listing := &cobra.Command{
		Use:   "listing",
		Short: "Prepare a shop listing for manual review",
	}
	prepare := &cobra.Command{
		Use:   "prepare",
		Short: "Open an approved shop listing plan for manual review",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			planID, _ := cmd.Flags().GetInt64("plan-id")
			snapshotHash, _ := cmd.Flags().GetString("snapshot-hash")
			approvalID, _ := cmd.Flags().GetString("approval-id")
			nonce, _ := cmd.Flags().GetString("nonce")
			if planID <= 0 || strings.TrimSpace(snapshotHash) == "" ||
				strings.TrimSpace(approvalID) == "" || strings.TrimSpace(nonce) == "" {
				return fmt.Errorf("plan-id, snapshot-hash, approval-id and nonce are required")
			}

			cfg := activeCfg
			if cfg.DataDir == "" {
				cfg = config.DefaultConfig()
			}
			server, err := ui.NewServer(cfg)
			if err != nil {
				return err
			}
			defer server.Close()

			result, err := server.PrepareShopListing(cmd.Context(), planID, snapshotHash, approvalID, nonce)
			if result != nil {
				if encodeErr := json.NewEncoder(cmd.OutOrStdout()).Encode(result); encodeErr != nil && err == nil {
					return encodeErr
				}
			}
			return err
		},
	}
	prepare.Flags().Int64("plan-id", 0, "Frozen shop listing plan ID")
	prepare.Flags().String("snapshot-hash", "", "Frozen shop listing snapshot hash")
	prepare.Flags().String("approval-id", "", "Single-use local approval ID")
	prepare.Flags().String("nonce", "", "Single-use local approval nonce")
	listing.AddCommand(prepare)
	publish.AddCommand(listing)
	root.AddCommand(publish)
}
