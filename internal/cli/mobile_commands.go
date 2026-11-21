package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"alpha-nexus-egolite/internal/mobileui"

	"github.com/spf13/cobra"
)

// addMobileCmd 注册 `mobile serve`：
//
//	alpha-nexus-egolite mobile serve --lan
//
// 它做两件事：在本机 UI 已经在跑的前提下，起一个只托管手机页面的网关；
// 打印出手机能直接打开的局域网地址。
//
// 刻意不做的事：不自动启动本机 UI（那会两套进程抢同一个 SQLite），不开放
// 任何写接口，不申请公网证书或隧道。
func addMobileCmd(root *cobra.Command) {
	mobileCmd := &cobra.Command{
		Use:   "mobile",
		Short: "Phone-friendly read-only console (LAN)",
	}

	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the mobile page and proxy /api to the local UI",
		Long: `Serve the mobile page on the LAN and proxy /api/* to the local UI.

The page is deliberately read-only: it shows account login state, content and
publish counts, unread notifications, and how fresh the metrics are. Publishing
and every other write action still has to happen on the machine itself.

Examples:
  alpha-nexus-egolite ui serve                 # 本机界面先起来
  alpha-nexus-egolite mobile serve             # 手机页面，打印局域网地址
  alpha-nexus-egolite mobile serve --port 8099 # 换个端口`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			port, _ := cmd.Flags().GetInt("port")
			uiPort, _ := cmd.Flags().GetInt("ui-port")
			bind, _ := cmd.Flags().GetString("bind")
			allowPublic, _ := cmd.Flags().GetBool("allow-public")

			if port <= 0 || port > 65535 {
				return fmt.Errorf("--port must be between 1 and 65535")
			}
			if uiPort <= 0 || uiPort > 65535 {
				return fmt.Errorf("--ui-port must be between 1 and 65535")
			}
			if port == uiPort {
				return fmt.Errorf("--port must differ from --ui-port")
			}
			if allowPublic {
				return fmt.Errorf("手机端不支持对公网开放；需要外网请用隧道工具自行把控边界")
			}

			upstream := "http://127.0.0.1:" + strconv.Itoa(uiPort)
			server, err := mobileui.New(mobileui.Options{
				Listen:           bind + ":" + strconv.Itoa(port),
				Upstream:         upstream,
				AllowPrivateOnly: true,
			})
			if err != nil {
				return err
			}
			listener, err := net.Listen("tcp", bind+":"+strconv.Itoa(port))
			if err != nil {
				return fmt.Errorf("listen on %s:%d: %w", bind, port, err)
			}

			// 上游没起来时给出可操作的提示，而不是让手机看到 502。
			if !upstreamReachable(uiPort) {
				cmd.Printf("提醒：%s 现在连不上。请先在另一个终端运行：\n", upstream)
				cmd.Printf("  alpha-nexus-egolite ui serve --port %d\n\n", uiPort)
			}

			cmd.Printf("手机端已启动（只读）\n")
			cmd.Printf("  本机访问  http://127.0.0.1:%d\n", port)
			for _, addr := range mobileui.LANAddresses() {
				cmd.Printf("  手机访问  http://%s:%d\n", addr, port)
			}
			cmd.Printf("  数据来源  %s\n", upstream)
			cmd.Printf("  退出      Ctrl+C\n\n")

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			go func() {
				<-ctx.Done()
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = server.Shutdown(shutdownCtx)
			}()
			if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
				return err
			}
			return nil
		},
	}
	serveCmd.Flags().Int("port", 17422, "Port for the mobile page")
	serveCmd.Flags().Int("ui-port", 17421, "Local UI port to proxy /api to")
	serveCmd.Flags().String("bind", "0.0.0.0", "Bind address (LAN by default)")
	serveCmd.Flags().Bool("allow-public", false, "Refused: the mobile console never listens on public interfaces intentionally")
	serveCmd.Flags().String("data-dir", "", "Data directory (only used for the identity hint)")

	mobileCmd.AddCommand(serveCmd)
	root.AddCommand(mobileCmd)
}

// upstreamReachable 只做一次很短的探测，不影响启动。
func upstreamReachable(port int) bool {
	client := &http.Client{Timeout: 800 * time.Millisecond}
	resp, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/api/auth/status")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
