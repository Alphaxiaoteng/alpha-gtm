package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"alpha-nexus-egolite/internal/terminal"
)

var codingExecCommand = exec.Command

// addCodingCmds — user-facing wrapper around coding agents (no raw shell).
// Inspired by OpenCode Desktop (anomalyco/opencode packages/desktop): Electron shell + agent CLI.
func addCodingCmds(root *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "coding",
		Short: "Coding Agent 封装（OpenCode / Claude Code，无裸 Shell）",
		Long: `简化本机 Coding Agent 使用（参考 OpenCode Desktop 体验）：

  alpha-nexus coding status
  alpha-nexus coding start --engine opencode
  alpha-nexus coding start --engine claude
  alpha-nexus coding open --engine opencode   # 打开系统桌面 / Terminal

不提供 shell 入口；仅 opencode | claude。`,
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "查看 OpenCode / Claude Code 是否安装",
		RunE: func(c *cobra.Command, args []string) error {
			p := terminal.Probe()
			w := tabwriter.NewWriter(c.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ENGINE\tREADY\tBINARY")
			for _, key := range []string{"opencode", "claude"} {
				m, _ := p[key].(map[string]any)
				ready := false
				bin := ""
				if m != nil {
					if v, ok := m["available"].(bool); ok {
						ready = v
					}
					if v, ok := m["binary"].(string); ok {
						bin = v
					}
				}
				mark := "no"
				if ready {
					mark = "yes"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", key, mark, bin)
			}
			_ = w.Flush()
			// machine-readable second block
			if jsonOut, _ := c.Flags().GetBool("json"); jsonOut {
				b, _ := json.MarshalIndent(map[string]any{
					"opencode": p["opencode"],
					"claude":   p["claude"],
					"note":     "shell intentionally omitted",
				}, "", "  ")
				c.Println(string(b))
			}
			return nil
		},
	}
	statusCmd.Flags().Bool("json", false, "Also print JSON")

	startCmd := &cobra.Command{
		Use:   "start",
		Short: "前台启动 Coding Agent CLI（接管当前终端）",
		RunE: func(c *cobra.Command, args []string) error {
			engine, _ := c.Flags().GetString("engine")
			dir, _ := c.Flags().GetString("dir")
			engine = strings.ToLower(strings.TrimSpace(engine))
			if engine != "opencode" && engine != "claude" {
				return fmt.Errorf("--engine must be opencode or claude (no shell)")
			}
			if engine == "opencode" {
				return terminal.ErrOpenCodeTUIDisabled
			}
			bin, ok := terminal.ResolveClaude()
			if !ok {
				return fmt.Errorf("%s not found on PATH", engine)
			}
			if dir == "" {
				dir, _ = os.Getwd()
			}
			c.Printf("→ %s\n  bin: %s\n  cwd: %s\n\n", engine, bin, dir)
			cmd := codingExecCommand(bin, args...)
			cmd.Dir = dir
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Env = os.Environ()
			return cmd.Run()
		},
	}
	startCmd.Flags().String("engine", "opencode", "opencode | claude")
	startCmd.Flags().String("dir", "", "Working directory")

	openCmd := &cobra.Command{
		Use:   "open",
		Short: "打开系统桌面版 OpenCode 或在 Terminal 中启动 CLI",
		RunE: func(c *cobra.Command, args []string) error {
			engine, _ := c.Flags().GetString("engine")
			engine = strings.ToLower(strings.TrimSpace(engine))
			if engine != "opencode" && engine != "claude" {
				return fmt.Errorf("--engine must be opencode or claude")
			}
			if engine == "opencode" {
				return terminal.ErrOpenCodeTUIDisabled
			}
			if runtime.GOOS == "darwin" {
				script := `tell application "Terminal" to do script "claude"`
				return codingExecCommand("osascript", "-e", script).Start()
			}
			return codingExecCommand("claude").Start()
		},
	}
	openCmd.Flags().String("engine", "opencode", "opencode | claude")

	chatCmd := &cobra.Command{
		Use:   "chat",
		Short: "非交互对话一轮（聊天框同款：opencode run / claude -p）",
		RunE: func(c *cobra.Command, args []string) error {
			engine, _ := c.Flags().GetString("engine")
			msg, _ := c.Flags().GetString("message")
			dir, _ := c.Flags().GetString("dir")
			cont, _ := c.Flags().GetBool("continue")
			if msg == "" && len(args) > 0 {
				msg = strings.Join(args, " ")
			}
			if msg == "" {
				return fmt.Errorf("--message or args required")
			}
			res, err := terminal.Chat(c.Context(), terminal.ChatRequest{
				Engine:   engine,
				Message:  msg,
				WorkDir:  dir,
				Continue: cont,
			})
			if res != nil {
				b, _ := json.MarshalIndent(res, "", "  ")
				c.Println(string(b))
			}
			if err != nil && (res == nil || !res.OK) {
				return err
			}
			return nil
		},
	}
	chatCmd.Flags().String("engine", "opencode", "opencode | claude")
	chatCmd.Flags().String("message", "", "User message")
	chatCmd.Flags().String("dir", "", "Working directory")
	chatCmd.Flags().Bool("continue", false, "Continue last session")

	cmd.AddCommand(statusCmd, startCmd, openCmd, chatCmd)
	root.AddCommand(cmd)
}
