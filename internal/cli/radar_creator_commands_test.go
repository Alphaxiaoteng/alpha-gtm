package cli

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/tikhub"
)

func executeRadarCreatorArchive(t *testing.T, args ...string) (string, error) {
	t.Helper()
	command := NewRootCmd()
	buf := new(bytes.Buffer)
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs(args)
	err := command.Execute()
	return buf.String(), err
}

func TestRadarCreatorArchiveRejectsNonDouyinPlatform(t *testing.T) {
	_, err := executeRadarCreatorArchive(t,
		"--data-dir", t.TempDir(),
		"radar", "creator", "archive",
		"--user-id", "sec_user_1",
		"--platform", "xhs",
	)
	if err == nil || !strings.Contains(err.Error(), "douyin only") {
		t.Fatalf("Execute() error = %v, want douyin-only rejection", err)
	}
}

func TestRadarCreatorArchiveRequiresUserID(t *testing.T) {
	output, err := executeRadarCreatorArchive(t,
		"--data-dir", t.TempDir(),
		"radar", "creator", "archive",
	)
	if err == nil {
		t.Fatal("Execute() error = nil, want missing --user-id rejection")
	}
	if !strings.Contains(err.Error(), "user-id") && !strings.Contains(output, "user-id") {
		t.Fatalf("Execute() = %q, error = %v, want user-id requirement", output, err)
	}
}

// 打错一个字符不该白烧一次真实 TikHub 调用，也不该在数据目录里留下空文件夹：
// 身份核对必须发生在任何 provider 调用与目录创建之前。
func TestRadarCreatorArchiveRejectsCreatorOutsideWatchlist(t *testing.T) {
	dataDir := t.TempDir()
	output, err := executeRadarCreatorArchive(t,
		"--data-dir", dataDir,
		"radar", "creator", "archive",
		"--user-id", "typo-user-id",
	)
	if err == nil {
		t.Fatalf("Execute() = %q, want a watchlist rejection", output)
	}
	if !strings.Contains(err.Error(), "not in the local watchlist") {
		t.Fatalf("Execute() error = %v, want a watchlist rejection", err)
	}
	if _, statErr := os.Stat(tikhub.CreatorFolder(dataDir, "douyin", "typo-user-id")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("rejected archive touched the creator folder: stat error = %v", statErr)
	}
}
