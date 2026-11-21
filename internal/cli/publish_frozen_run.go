//go:build legacy_chromium

package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"alpha-nexus-egolite/internal/publishplan"
	"alpha-nexus-egolite/internal/publishworker"
)

const (
	defaultFrozenPublishDelay = 24 * time.Hour
	maxFrozenPublishDelay     = 7 * 24 * time.Hour
)

func newPublishFrozenRunCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "frozen-run",
		Short: "Run one frozen target in the background without focusing its browser",
		Long: `Clone one reviewed frozen target with a schedule relative to execution time,
prepare it through BrowserFacade, and click the final platform control once only
when the prepared snapshot is submit-ready. This command never marks publication
successful without separate creator-backend evidence.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sourcePlanID, _ := cmd.Flags().GetInt64("from-plan")
			after, _ := cmd.Flags().GetDuration("after")
			confirmSubmit, _ := cmd.Flags().GetBool("confirm-submit")
			return runFrozenPublish(cmd, sourcePlanID, after, confirmSubmit)
		},
	}
	command.Flags().Int64("from-plan", 0, "Existing reviewed single-target plan to clone")
	command.Flags().Duration("after", defaultFrozenPublishDelay, "Native publish delay relative to execution time")
	command.Flags().Bool("confirm-submit", false, "Explicitly authorize the single final platform click")
	_ = command.MarkFlagRequired("from-plan")
	return command
}

func newPublishFrozenSubmitCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "frozen-submit",
		Short: "Retry only the submit step of one prepared frozen target",
		Long: `Reuse one live, submit-ready frozen composer after a prior submit
stopped before any trusted final click. Any durable submit_attempted_at blocks
this command, so an unknown platform outcome can never be replayed.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			planID, _ := cmd.Flags().GetInt64("plan")
			confirmSubmit, _ := cmd.Flags().GetBool("confirm-submit")
			return runPreparedFrozenSubmit(cmd, planID, confirmSubmit)
		},
	}
	command.Flags().Int64("plan", 0, "Existing submit-ready single-target plan")
	command.Flags().Bool("confirm-submit", false, "Explicitly authorize the single final platform click")
	_ = command.MarkFlagRequired("plan")
	return command
}

func newPublishResolveNotPublishedCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "resolve-not-published",
		Short: "Resolve one prior submit as absent after creator-backend verification",
		RunE: func(cmd *cobra.Command, _ []string) error {
			jobID, _ := cmd.Flags().GetInt64("job")
			evidencePath, _ := cmd.Flags().GetString("evidence")
			confirm, _ := cmd.Flags().GetBool("confirm")
			if !confirm {
				return fmt.Errorf("PUBLISH_ABSENT_RESOLUTION_CONFIRMATION_REQUIRED: pass --confirm")
			}
			if jobID <= 0 || evidencePath == "" {
				return fmt.Errorf("--job and --evidence are required")
			}
			file, err := os.Open(evidencePath)
			if err != nil {
				return err
			}
			defer file.Close()
			digest := sha256.New()
			if _, err := io.Copy(digest, file); err != nil {
				return err
			}
			database, _, err := openDB(cmd, activeCfg)
			if err != nil {
				return err
			}
			defer database.Close()
			if err := database.ResolvePublishSubmitNotPublished(
				jobID, evidencePath, fmt.Sprintf("%x", digest.Sum(nil)),
			); err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]interface{}{
				"job_id": jobID, "submit_outcome": "verified_not_published",
			})
		},
	}
	command.Flags().Int64("job", 0, "Prior submit-attempted publish job")
	command.Flags().String("evidence", "", "Creator-backend screenshot showing no matching item")
	command.Flags().Bool("confirm", false, "Confirm the negative creator-backend evidence")
	_ = command.MarkFlagRequired("job")
	_ = command.MarkFlagRequired("evidence")
	return command
}

func newPublishResolveScheduledCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "resolve-scheduled",
		Short: "Record one verified native test schedule and its cancellation",
		RunE: func(cmd *cobra.Command, _ []string) error {
			jobID, _ := cmd.Flags().GetInt64("job")
			evidencePath, _ := cmd.Flags().GetString("evidence")
			cancelledPath, _ := cmd.Flags().GetString("cancelled-evidence")
			confirm, _ := cmd.Flags().GetBool("confirm")
			if !confirm {
				return fmt.Errorf("PUBLISH_SCHEDULED_RESOLUTION_CONFIRMATION_REQUIRED: pass --confirm")
			}
			if jobID <= 0 || evidencePath == "" || cancelledPath == "" {
				return fmt.Errorf("--job, --evidence and --cancelled-evidence are required")
			}
			fileDigest := func(path string) (string, error) {
				file, err := os.Open(path)
				if err != nil {
					return "", err
				}
				defer file.Close()
				digest := sha256.New()
				if _, err := io.Copy(digest, file); err != nil {
					return "", err
				}
				return fmt.Sprintf("%x", digest.Sum(nil)), nil
			}
			evidenceDigest, err := fileDigest(evidencePath)
			if err != nil {
				return err
			}
			cancelledDigest, err := fileDigest(cancelledPath)
			if err != nil {
				return err
			}
			database, _, err := openDB(cmd, activeCfg)
			if err != nil {
				return err
			}
			defer database.Close()
			if err := database.ResolvePublishSubmitScheduled(
				jobID,
				evidencePath, evidenceDigest,
				cancelledPath, cancelledDigest,
			); err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]interface{}{
				"job_id": jobID, "submit_outcome": "scheduled_verified",
			})
		},
	}
	command.Flags().Int64("job", 0, "Prior submit-attempted publish job")
	command.Flags().String("evidence", "", "Creator-backend screenshot showing the native schedule")
	command.Flags().String("cancelled-evidence", "", "Creator-backend screenshot after cancelling the test schedule")
	command.Flags().Bool("confirm", false, "Confirm both creator-backend evidence files")
	_ = command.MarkFlagRequired("job")
	_ = command.MarkFlagRequired("evidence")
	_ = command.MarkFlagRequired("cancelled-evidence")
	return command
}

func freezeInputFromPlan(source *publishplan.Plan, publishAt time.Time) (publishplan.FreezeInput, error) {
	if source == nil || source.ID <= 0 || len(source.Snapshot.Targets) != 1 {
		return publishplan.FreezeInput{}, fmt.Errorf("PUBLISH_SINGLE_TARGET_PLAN_REQUIRED")
	}
	target := source.Snapshot.Targets[0]
	input := publishplan.FreezeInput{
		ContentID:        source.ContentID,
		MediaMode:        source.MediaMode,
		CompliancePolicy: source.Snapshot.CompliancePolicy,
		Concurrency:      1,
		Targets: []publishplan.TargetInput{{
			AccountID: target.AccountID,
			VariantID: target.VariantID, SettingsJSON: target.SettingsJSON,
		}},
	}
	if publishplan.SupportsNativeSchedule(target.PlatformCode, source.MediaMode) {
		input.PublishAt = publishAt.UTC().Truncate(time.Minute).Format(time.RFC3339)
	}
	if target.AccountPlatformCode == "meta" {
		input.Targets[0].AssetID = target.AssetID
	}
	for _, item := range source.Snapshot.Media {
		if item.Role == "cover" {
			if source.MediaMode == "article" {
				input.MediaIDs = append(input.MediaIDs, item.ID)
				continue
			}
			if input.CoverMediaID != 0 {
				return publishplan.FreezeInput{}, fmt.Errorf("PUBLISH_COVER_AMBIGUOUS")
			}
			input.CoverMediaID = item.ID
			continue
		}
		input.MediaIDs = append(input.MediaIDs, item.ID)
	}
	if source.MediaMode == "video" && input.CoverMediaID == 0 {
		input.NativeVideoCover = true
	}
	if source.MediaMode != "article" && len(input.MediaIDs) == 0 {
		return publishplan.FreezeInput{}, fmt.Errorf("PUBLISH_MEDIA_REQUIRED")
	}
	return input, nil
}

func runFrozenPublish(cmd *cobra.Command, sourcePlanID int64, after time.Duration, confirmSubmit bool) error {
	if sourcePlanID <= 0 {
		return fmt.Errorf("--from-plan is required")
	}
	if !confirmSubmit {
		return fmt.Errorf("PUBLISH_SUBMIT_CONFIRMATION_REQUIRED: pass --confirm-submit")
	}
	if after < 15*time.Minute || after > maxFrozenPublishDelay {
		return fmt.Errorf("--after must be between 15m and 168h")
	}

	facade, database, _, err := openFacade(activeCfg)
	if err != nil {
		return err
	}
	defer database.Close()

	service := &publishplan.Service{DB: database}
	source, err := service.Get(sourcePlanID)
	if err != nil {
		return err
	}
	input, err := freezeInputFromPlan(source, time.Now().UTC().Add(after))
	if err != nil {
		return err
	}
	plan, err := service.Freeze(input)
	if err != nil {
		return err
	}

	executionContext, cancel := context.WithTimeout(cmd.Context(), 20*time.Minute)
	defer cancel()
	executor := &publishplan.Executor{DB: database, Browsers: facade, Worker: publishworker.Runner{}}
	const sessionID = "cli-background-frozen-run"

	prepareGrant, err := service.ApproveForAction(
		plan.ID, plan.SnapshotHash, sessionID, publishplan.PublishPlanActionPrepare, 5*time.Minute,
	)
	if err != nil {
		return err
	}
	if _, err := service.ConsumeApprovalForAction(
		plan.ID, plan.SnapshotHash, prepareGrant.ID, prepareGrant.Nonce, publishplan.PublishPlanActionPrepare,
	); err != nil {
		return err
	}
	prepared, err := executor.Prepare(executionContext, plan.ID)
	if err != nil {
		return err
	}
	if err := printFrozenExecution(cmd, plan, "prepare", prepared); err != nil {
		return err
	}

	submitGrant, err := service.ApproveForAction(
		plan.ID, plan.SnapshotHash, sessionID, publishplan.PublishPlanActionSubmit, 5*time.Minute,
	)
	if err != nil {
		return fmt.Errorf("PUBLISH_PLAN_NOT_READY_TO_SUBMIT: %w", err)
	}
	if _, err := service.ConsumeApprovalForAction(
		plan.ID, plan.SnapshotHash, submitGrant.ID, submitGrant.Nonce, publishplan.PublishPlanActionSubmit,
	); err != nil {
		return err
	}
	submitted, err := executor.Submit(executionContext, plan.ID)
	if err != nil {
		return err
	}
	return printFrozenExecution(cmd, plan, "submit", submitted)
}

func runPreparedFrozenSubmit(cmd *cobra.Command, planID int64, confirmSubmit bool) error {
	if planID <= 0 {
		return fmt.Errorf("--plan is required")
	}
	if !confirmSubmit {
		return fmt.Errorf("PUBLISH_SUBMIT_CONFIRMATION_REQUIRED: pass --confirm-submit")
	}
	facade, database, _, err := openFacade(activeCfg)
	if err != nil {
		return err
	}
	defer database.Close()
	service := &publishplan.Service{DB: database}
	plan, err := service.Get(planID)
	if err != nil {
		return err
	}
	if len(plan.Snapshot.Targets) != 1 {
		return fmt.Errorf("PUBLISH_SINGLE_TARGET_PLAN_REQUIRED")
	}
	grant, err := service.ApproveForAction(
		plan.ID, plan.SnapshotHash, "cli-background-frozen-submit",
		publishplan.PublishPlanActionSubmit, 5*time.Minute,
	)
	if err != nil {
		return err
	}
	if _, err := service.ConsumeApprovalForAction(
		plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce, publishplan.PublishPlanActionSubmit,
	); err != nil {
		return err
	}
	executionContext, cancel := context.WithTimeout(cmd.Context(), 20*time.Minute)
	defer cancel()
	execution, err := (&publishplan.Executor{
		DB: database, Browsers: facade, Worker: publishworker.Runner{},
	}).Submit(executionContext, plan.ID)
	if err != nil {
		return err
	}
	return printFrozenExecution(cmd, plan, "submit", execution)
}

func printFrozenExecution(cmd *cobra.Command, plan *publishplan.Plan, phase string, execution *publishplan.Execution) error {
	payload := struct {
		Phase        string                 `json:"phase"`
		PlanID       int64                  `json:"plan_id"`
		SnapshotHash string                 `json:"snapshot_hash"`
		PublishAt    string                 `json:"publish_at"`
		Execution    *publishplan.Execution `json:"execution"`
	}{
		Phase: phase, PlanID: plan.ID, SnapshotHash: plan.SnapshotHash,
		PublishAt: plan.Snapshot.Targets[0].PublishAt, Execution: execution,
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(payload)
}
