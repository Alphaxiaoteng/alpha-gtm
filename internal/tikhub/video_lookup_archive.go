package tikhub

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func VideoLookupFolder(dataDir string) string {
	return filepath.Join(dataDir, "trend-radar", "video-search")
}

func ArchiveVideoLookupSnapshot(dataDir, rawPath string, snapshot CallSnapshot) (string, error) {
	if snapshot.Platform != "douyin" || snapshot.Capability != "content_detail" {
		return "", nil
	}
	root := VideoLookupFolder(dataDir)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return "", err
	}
	label := strings.TrimSpace(fmt.Sprint(snapshot.Parameters["aweme_id"]))
	if label == "" || label == "<nil>" {
		shareURL := strings.TrimSpace(fmt.Sprint(snapshot.Parameters["share_url"]))
		sum := sha256.Sum256([]byte(shareURL))
		label = "share_" + hex.EncodeToString(sum[:6])
	}
	name := strconv.FormatInt(snapshot.CallID, 10) + "_" + safeSnapshotName(label) + ".json"
	target := filepath.Join(root, name)
	if err := os.Link(rawPath, target); err != nil {
		return "", err
	}
	dir, err := os.Open(root)
	if err != nil {
		return "", err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return "", err
	}
	return root, nil
}
