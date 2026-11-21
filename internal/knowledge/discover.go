package knowledge

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxDirectoryFiles = 10000
	maxDirectoryBytes = int64(512 << 20)
)

var supportedKnowledgeExtensions = map[string]struct{}{
	".md": {}, ".markdown": {}, ".txt": {}, ".html": {}, ".htm": {}, ".pdf": {}, ".docx": {}, ".zip": {},
}

var skippedKnowledgeDirectories = map[string]struct{}{
	".git": {}, ".hg": {}, ".svn": {}, "node_modules": {}, "npm-cache": {},
	"dist": {}, "build": {}, "release": {}, "cache": {}, ".cache": {},
	"1key": {}, "credentials": {}, "credential": {}, "secrets": {}, "secret": {},
	"账号与key": {}, "密钥": {},
}

var sensitiveKnowledgeNameTokens = []string{
	".env", "api-key", "api_key", "apikey", "credentials", "credential",
	"password", "passwd", "secret", "token", "账号", "密码", "密钥", "凭证",
}

func sensitiveKnowledgeName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, token := range sensitiveKnowledgeNameTokens {
		if strings.Contains(name, token) {
			return true
		}
	}
	return false
}

// DiscoverDirectory returns stable, relative source names without following
// nested symlinks or indexing generated dependency/build trees.
func DiscoverDirectory(root string) ([]InputFile, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "." || root == "" {
		return nil, fmt.Errorf("knowledge directory is required")
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("knowledge source is not a directory")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	files := make([]InputFile, 0, 128)
	var totalBytes int64
	err = filepath.WalkDir(absRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == absRoot {
				return walkErr
			}
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path == absRoot {
			return nil
		}
		name := strings.ToLower(entry.Name())
		if entry.IsDir() {
			if _, skip := skippedKnowledgeDirectories[name]; skip || strings.HasPrefix(name, ".") || sensitiveKnowledgeName(name) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if sensitiveKnowledgeName(name) {
			return nil
		}
		if _, ok := supportedKnowledgeExtensions[strings.ToLower(filepath.Ext(name))]; !ok {
			return nil
		}
		fileInfo, err := entry.Info()
		if err != nil || fileInfo.Size() <= 0 {
			return nil
		}
		if len(files) >= maxDirectoryFiles {
			return fmt.Errorf("knowledge directory exceeds %d supported files", maxDirectoryFiles)
		}
		totalBytes += fileInfo.Size()
		if totalBytes > maxDirectoryBytes {
			return fmt.Errorf("knowledge directory exceeds %d bytes", maxDirectoryBytes)
		}
		relative, err := filepath.Rel(absRoot, path)
		if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil
		}
		files = append(files, InputFile{Name: filepath.ToSlash(relative), Path: path})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(left, right int) bool { return files[left].Name < files[right].Name })
	return files, nil
}
