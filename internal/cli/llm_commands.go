package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"alpha-nexus-egolite/internal/llm"
)

func addLLMCmds(root *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "llm",
		Short: "固定豆包 API 状态和单次烟测",
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "显示固定豆包 API 是否已配置",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := llm.ResolveDoubaoConfig()
			b, _ := json.MarshalIndent(map[string]any{
				"model":          cfg.Model,
				"api_configured": cfg.APIKey != "",
			}, "", "  ")
			cmd.Println(string(b))
			return nil
		},
	}

	completeCmd := &cobra.Command{
		Use:   "complete",
		Short: "固定豆包 API 单次严格聊天烟测",
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt, _ := cmd.Flags().GetString("prompt")
			system, _ := cmd.Flags().GetString("system")
			if prompt == "" {
				prompt = "Reply with exactly one short word: ok"
			}
			if system == "" {
				system = "You are a concise assistant."
			}
			g := llm.NewDoubaoGateway(90 * time.Second)
			if g == nil {
				return fmt.Errorf("DOUBAO_API_NOT_CONFIGURED")
			}
			g.Strict = true
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			out, err := g.Complete(ctx, system, prompt)
			if err != nil {
				return err
			}
			if llm.IsLocalTemplate(out) {
				return fmt.Errorf("got offline local-template fallback; check key/base")
			}
			if strings.TrimSpace(out) == "" {
				return fmt.Errorf("empty assistant text")
			}
			// machine-readable for smoke capture
			_ = json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
				"ok":     true,
				"model":  g.Model,
				"output": out,
			})
			return nil
		},
	}
	completeCmd.Flags().String("prompt", "", "User prompt")
	completeCmd.Flags().String("system", "", "System prompt")
	cmd.AddCommand(statusCmd, completeCmd)
	root.AddCommand(cmd)
}
