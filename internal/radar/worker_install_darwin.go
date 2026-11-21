//go:build darwin

package radar

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func InstallWorker(ctx context.Context, dataDir string) error {
	source, err := os.Executable()
	if err != nil {
		return err
	}
	// 临时数据目录（测试、e2e、一次性试跑）绝不能改写用户真实的 LaunchAgent。
	// 实测过：跑一次带 NewServer 的测试，就把 ~/Library/LaunchAgents 里那个
	// 真实任务的 ProgramArguments 指向了 t.TempDir()，任务当场失效且无人察觉。
	if isEphemeralDataDir(dataDir) {
		return nil
	}
	binDir := filepath.Join(dataDir, "trend-radar", "bin")
	target := filepath.Join(binDir, "alpha-nexus-radar-worker")
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	launchDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(launchDir, 0o700); err != nil {
		return err
	}
	logDir := filepath.Join(dataDir, "trend-radar", "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return err
	}
	// label 按数据目录区分：两套数据目录（比如默认目录与自定义目录）同时存在
	// 时，各自持有自己的定时任务，不会互相 bootout。
	label := workerLabel(dataDir)
	plistPath := filepath.Join(launchDir, label+".plist")
	domain := "gui/" + strconv.Itoa(os.Getuid())
	// 已安装且已加载还不够：必须确认装的是当前这份可执行文件。
	// 早先这里无条件 return，导致 worker 永远停在首次安装的版本——
	// 实测线上 worker 是 11 月 4 日的构建，连自动归档代码都不存在，
	// 而界面上那个「每 24 小时自动更新」开关照样能开。
	if _, targetErr := os.Stat(target); targetErr == nil {
		if _, plistErr := os.Stat(plistPath); plistErr == nil {
			if exec.CommandContext(ctx, "launchctl", "print", domain+"/"+label).Run() == nil {
				if workerBinaryMatches(source, target) {
					return nil
				}
				// 版本不一致：先卸载旧的，装新的，再加载。
				_ = exec.CommandContext(ctx, "launchctl", "bootout", domain+"/"+label).Run()
			}
		}
	}
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		return err
	}
	if err := copyWorkerExecutable(source, target); err != nil {
		return err
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key><array>
    <string>%s</string><string>--data-dir</string><string>%s</string>
    <string>radar</string><string>worker</string><string>run-once</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>StartInterval</key><integer>60</integer>
  <key>ProcessType</key><string>Background</string>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict></plist>
`,
		html.EscapeString(label), html.EscapeString(target), html.EscapeString(dataDir),
		html.EscapeString(filepath.Join(logDir, "worker.stdout.log")),
		html.EscapeString(filepath.Join(logDir, "worker.stderr.log")),
	)
	if err := writeWorkerFile(plistPath, []byte(plist), 0o600); err != nil {
		return err
	}
	_ = exec.CommandContext(ctx, "launchctl", "bootout", domain+"/"+label).Run()
	if output, err := exec.CommandContext(ctx, "launchctl", "bootstrap", domain, plistPath).CombinedOutput(); err != nil {
		return fmt.Errorf("install radar LaunchAgent: %w: %s", err, output)
	}
	return nil
}

// 默认数据目录沿用历史上那个 label，升级时不会留下孤儿任务。
const defaultWorkerLabel = "com.alpha-nexus.radar-worker"

// workerLabel 让每个数据目录拥有自己的 LaunchAgent。默认目录保持原 label，
// 其它目录用数据目录路径的短哈希后缀。
func workerLabel(dataDir string) string {
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		abs = dataDir
	}
	home, err := os.UserHomeDir()
	if err == nil && abs == filepath.Join(home, ".alpha-nexus-egolite") {
		return defaultWorkerLabel
	}
	sum := sha256.Sum256([]byte(abs))
	return fmt.Sprintf("%s-%x", defaultWorkerLabel, sum[:4])
}

// isEphemeralDataDir 判断这个数据目录是不是一次性的。
//
// 判据是「位于系统临时目录下」，而不是「名字里带 test」——e2e 与手动试跑同样
// 会落在 $TMPDIR 里，它们和测试一样不该碰真实定时任务。
func isEphemeralDataDir(dataDir string) bool {
	if strings.TrimSpace(dataDir) == "" {
		return true
	}
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		abs = dataDir
	}
	for _, root := range []string{os.TempDir(), "/tmp", "/var/folders"} {
		if root == "" {
			continue
		}
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if abs == rootAbs || strings.HasPrefix(abs, rootAbs+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func copyWorkerExecutable(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	return writeWorkerFileFrom(target, 0o700, input)
}

func writeWorkerFile(target string, body []byte, mode os.FileMode) error {
	return writeWorkerFileFrom(target, mode, bytes.NewReader(body))
}

func writeWorkerFileFrom(target string, mode os.FileMode, input io.Reader) error {
	temp, err := os.CreateTemp(filepath.Dir(target), ".worker-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := io.Copy(temp, input); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, target)
}

// workerBinaryMatches 判断已安装的 worker 是否就是当前这份可执行文件。
//
// 用大小 + 修改时间：worker 由本进程自身复制而来，同一次构建大小一致；
// 时间戳能捕捉到重新构建但大小恰巧相同的情况。两者都相同时认定无需更新。
func workerBinaryMatches(source, target string) bool {
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return false
	}
	targetInfo, err := os.Stat(target)
	if err != nil {
		return false
	}
	if sourceInfo.Size() != targetInfo.Size() {
		return false
	}
	return !sourceInfo.ModTime().After(targetInfo.ModTime())
}
