package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/tools"
)

func addToolsCmd(root *cobra.Command) {
	toolsCmd := &cobra.Command{
		Use:   "tools",
		Short: "Application tool surface for Tool Agent / workflows",
		Long: `Single capability registry shared by CLI, UI, and future Tool Agent.

  alpha-nexus tools list
  alpha-nexus tools list --category tikhub
  alpha-nexus tools schema
  alpha-nexus tools call ego.space.list
  alpha-nexus tools call tikhub.config.status
  alpha-nexus tools call tikhub.rank.daily --args '{"confirm":false}'

Domain work stays in packages; tools only dispatch. See docs/AGENT_WORKFLOW.md.`,
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List tools (optional --category)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cat, _ := cmd.Flags().GetString("category")
			for _, d := range tools.Default.ListByCategory(cat) {
				cmd.Printf("%-28s  %-10s  %s\n", d.Name, d.Category, d.Description)
			}
			return nil
		},
	}
	listCmd.Flags().String("category", "", "ego|account|publish|social|knowledge|tikhub|workflow|media|agent|shop")

	schemaCmd := &cobra.Command{
		Use:   "schema",
		Short: "Print JSON tool schemas for agent binding",
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := tools.Default.SchemaJSON()
			if err != nil {
				return err
			}
			cmd.Println(string(b))
			return nil
		},
	}

	callCmd := &cobra.Command{
		Use:   "call [tool.name]",
		Short: "Invoke a tool by name with JSON args",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, _ := cmd.Flags().GetString("args")
			var m map[string]interface{}
			if raw != "" {
				if err := json.Unmarshal([]byte(raw), &m); err != nil {
					return fmt.Errorf("--args must be JSON object: %w", err)
				}
			}
			rt := toolsRuntime()
			// 归档这类长任务把进度按 NDJSON 写到 stderr，stdout 只留最终结果 JSON：
			// 这样 `tools call` 不再是一个几十秒的纯黑盒，也不会污染结果管道。
			ctx := tools.WithProgress(context.Background(), tools.NewCreatorProgressWriter(cmd.ErrOrStderr()).OnProgress)
			res, err := tools.Default.Call(ctx, rt, args[0], m)
			if res != nil {
				b, _ := json.MarshalIndent(res, "", "  ")
				cmd.Println(string(b))
			}
			return err
		},
	}
	callCmd.Flags().String("args", "{}", "JSON object of tool arguments")

	wfCmd := &cobra.Command{
		Use:   "workflow",
		Short: "Run multi-step tool workflow (Tool Agent primitive)",
		Long: `Example:
  alpha-nexus tools workflow --args '{
    "steps":[
      {"tool":"tikhub.config.status","args":{}},
      {"tool":"account.list","args":{}},
      {"tool":"ego.space.list","args":{}}
    ]
  }'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, _ := cmd.Flags().GetString("args")
			var m map[string]interface{}
			if raw == "" {
				return fmt.Errorf("--args required with steps array")
			}
			if err := json.Unmarshal([]byte(raw), &m); err != nil {
				return err
			}
			res, err := tools.Default.Call(context.Background(), toolsRuntime(), "workflow.run", m)
			if res != nil {
				b, _ := json.MarshalIndent(res, "", "  ")
				cmd.Println(string(b))
			}
			return err
		},
	}
	wfCmd.Flags().String("args", "", "JSON: {steps:[{tool,args}], stop_on_error?:bool}")

	toolsCmd.AddCommand(listCmd, schemaCmd, callCmd, wfCmd)
	root.AddCommand(toolsCmd)
}

func toolsRuntime() *tools.Runtime {
	cfg := activeCfg
	if cfg.DataDir == "" {
		cfg = config.DefaultConfig()
	}
	return egoToolsRuntime(cfg)
}

func egoToolsRuntime(cfg config.Config) *tools.Runtime {
	dir, _ := cfg.ResolvedDataDir()
	return &tools.Runtime{
		DataDir:             dir,
		EgoBrowserCommand:   cfg.EgoBrowserCommand,
		MaxConcurrentSpaces: cfg.MaxConcurrentSpaces,
	}
}
