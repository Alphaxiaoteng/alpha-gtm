package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"alpha-nexus-egolite/internal/tools"
)

func callStudioScheduleTool(cmd *cobra.Command, name string, args map[string]interface{}) error {
	result, err := tools.Default.Call(cmd.Context(), toolsRuntime(), name, args)
	if result != nil {
		if encodeErr := json.NewEncoder(cmd.OutOrStdout()).Encode(result); encodeErr != nil && err == nil {
			return encodeErr
		}
	}
	return err
}

func addStudioScheduleHandoffCmd(root *cobra.Command) {
	studioCmd := &cobra.Command{Use: "studio", Short: "Studio editorial handoffs"}
	handoffCmd := &cobra.Command{Use: "handoff", Short: "Hand reviewed Studio content to Media without local scheduling"}
	createCmd := &cobra.Command{
		Use: "create", Short: "Save one reviewed content and IANA wall-clock handoff",
		RunE: func(cmd *cobra.Command, _ []string) error {
			workflowID, _ := cmd.Flags().GetString("workflow-id")
			contentID, _ := cmd.Flags().GetInt64("content-id")
			scriptID, _ := cmd.Flags().GetInt64("script-id")
			localTime, _ := cmd.Flags().GetString("original-local-time")
			timezone, _ := cmd.Flags().GetString("timezone")
			if strings.TrimSpace(workflowID) == "" || contentID <= 0 || strings.TrimSpace(localTime) == "" || strings.TrimSpace(timezone) == "" {
				return fmt.Errorf("workflow-id, content-id, original-local-time and timezone are required")
			}
			return callStudioScheduleTool(cmd, "studio.schedule-handoff.create", map[string]interface{}{
				"workflow_id": workflowID, "content_id": contentID, "script_id": scriptID, "original_local_time": localTime, "timezone": timezone,
			})
		},
	}
	createCmd.Flags().String("workflow-id", "", "Creation workflow ID")
	createCmd.Flags().Int64("content-id", 0, "Reviewed content ID")
	createCmd.Flags().Int64("script-id", 0, "Optional reviewed Studio script ID")
	createCmd.Flags().String("original-local-time", "", "YYYY-MM-DDTHH:MM wall-clock time")
	createCmd.Flags().String("timezone", "", "IANA timezone, for example Asia/Shanghai")

	getCmd := &cobra.Command{
		Use: "get", Short: "Read one Studio schedule handoff",
		RunE: func(cmd *cobra.Command, _ []string) error {
			id, _ := cmd.Flags().GetInt64("handoff-id")
			if id <= 0 {
				return fmt.Errorf("handoff-id is required")
			}
			return callStudioScheduleTool(cmd, "studio.schedule-handoff.get", map[string]interface{}{"handoff_id": id})
		},
	}
	getCmd.Flags().Int64("handoff-id", 0, "Studio handoff ID")

	listCmd := &cobra.Command{
		Use: "list", Short: "List Studio schedule handoffs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			limit, _ := cmd.Flags().GetInt("limit")
			return callStudioScheduleTool(cmd, "studio.schedule-handoff.list", map[string]interface{}{"limit": limit})
		},
	}
	listCmd.Flags().Int("limit", 50, "Maximum rows (1-200)")

	cancelCmd := &cobra.Command{
		Use: "cancel", Short: "Cancel one ready Studio schedule handoff",
		RunE: func(cmd *cobra.Command, _ []string) error {
			id, _ := cmd.Flags().GetInt64("handoff-id")
			if id <= 0 {
				return fmt.Errorf("handoff-id is required")
			}
			return callStudioScheduleTool(cmd, "studio.schedule-handoff.cancel", map[string]interface{}{"handoff_id": id})
		},
	}
	cancelCmd.Flags().Int64("handoff-id", 0, "Studio handoff ID")

	handoffCmd.AddCommand(createCmd, getCmd, listCmd, cancelCmd)
	studioCmd.AddCommand(handoffCmd)
	root.AddCommand(studioCmd)
}

func addPublishPlanHandoffCmd(publish *cobra.Command) {
	planCmd := &cobra.Command{Use: "plan", Short: "Freeze publish plans from reviewed handoffs"}
	freezeCmd := &cobra.Command{
		Use: "freeze-handoff", Short: "Freeze a publish plan from a Studio handoff",
		RunE: func(cmd *cobra.Command, _ []string) error {
			handoffID, _ := cmd.Flags().GetInt64("handoff-id")
			targetsJSON, _ := cmd.Flags().GetString("targets")
			policyJSON, _ := cmd.Flags().GetString("compliance-policy")
			concurrency, _ := cmd.Flags().GetInt("concurrency")
			if handoffID <= 0 || strings.TrimSpace(targetsJSON) == "" {
				return fmt.Errorf("handoff-id and targets are required")
			}
			var targets interface{}
			if err := json.Unmarshal([]byte(targetsJSON), &targets); err != nil {
				return fmt.Errorf("targets must be a JSON array: %w", err)
			}
			args := map[string]interface{}{"handoff_id": handoffID, "targets": targets, "concurrency": concurrency}
			if strings.TrimSpace(policyJSON) != "" {
				var policy interface{}
				if err := json.Unmarshal([]byte(policyJSON), &policy); err != nil {
					return fmt.Errorf("compliance-policy must be a JSON object: %w", err)
				}
				args["compliance_policy"] = policy
			}
			return callStudioScheduleTool(cmd, "publish.plan.freeze-handoff", args)
		},
	}
	freezeCmd.Flags().Int64("handoff-id", 0, "Ready Studio handoff ID")
	freezeCmd.Flags().String("targets", "", "JSON array: [{account_id,asset_id?,variant_id?,settings_json?}]")
	freezeCmd.Flags().String("compliance-policy", "", "Optional CompliancePolicy JSON object")
	freezeCmd.Flags().Int("concurrency", 1, "Strict serial handoff execution; must be 1")
	planCmd.AddCommand(freezeCmd)
	publish.AddCommand(planCmd)
}
