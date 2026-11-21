package agents

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type PrimitiveToolFunc func(ctx context.Context, workDir string, args map[string]any) (string, error)

// PrimitiveRegistry holds the 4 minimalist core tools.
var PrimitiveRegistry = map[string]PrimitiveToolFunc{
	"read_file":    primitiveReadFile,
	"write_file":   primitiveWriteFile,
	"replace_file": primitiveReplaceFile,
	"run_command":  primitiveRunCommand,
}

func primitiveReadFile(_ context.Context, workDir string, args map[string]any) (string, error) {
	relPath, _ := args["path"].(string)
	if strings.TrimSpace(relPath) == "" {
		return "", fmt.Errorf("path is required")
	}
	absPath := filepath.Join(workDir, relPath)
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("read file failed: %w", err)
	}

	startLine := 1
	if v, ok := args["start_line"].(float64); ok && v > 0 {
		startLine = int(v)
	}
	endLine := 0
	if v, ok := args["end_line"].(float64); ok && v > 0 {
		endLine = int(v)
	}

	lines := strings.Split(string(data), "\n")
	if endLine <= 0 || endLine > len(lines) {
		endLine = len(lines)
	}
	if startLine < 1 {
		startLine = 1
	}
	if startLine > len(lines) {
		return "", nil
	}

	var b strings.Builder
	for i := startLine - 1; i < endLine; i++ {
		b.WriteString(fmt.Sprintf("%4d | %s\n", i+1, lines[i]))
	}
	return b.String(), nil
}

func primitiveWriteFile(_ context.Context, workDir string, args map[string]any) (string, error) {
	relPath, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if strings.TrimSpace(relPath) == "" {
		return "", fmt.Errorf("path is required")
	}
	absPath := filepath.Join(workDir, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return "", fmt.Errorf("mkdir failed: %w", err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write file failed: %w", err)
	}
	return fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), relPath), nil
}

func primitiveReplaceFile(_ context.Context, workDir string, args map[string]any) (string, error) {
	relPath, _ := args["path"].(string)
	oldText, _ := args["old_text"].(string)
	newText, _ := args["new_text"].(string)
	if strings.TrimSpace(relPath) == "" || oldText == "" {
		return "", fmt.Errorf("path and old_text are required")
	}
	absPath := filepath.Join(workDir, relPath)
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("read file failed: %w", err)
	}

	src := string(data)
	if !strings.Contains(src, oldText) {
		return "", fmt.Errorf("old_text not found in file %s", relPath)
	}
	replaced := strings.Replace(src, oldText, newText, 1)
	if err := os.WriteFile(absPath, []byte(replaced), 0644); err != nil {
		return "", fmt.Errorf("write file failed: %w", err)
	}
	return fmt.Sprintf("Successfully replaced text in %s", relPath), nil
}

func primitiveRunCommand(ctx context.Context, workDir string, args map[string]any) (string, error) {
	cmdStr, _ := args["command"].(string)
	if strings.TrimSpace(cmdStr) == "" {
		return "", fmt.Errorf("command is required")
	}

	timeout := 30 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = workDir
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	output := outBuf.String()
	stderrStr := errBuf.String()

	if stderrStr != "" {
		output += "\nSTDERR:\n" + stderrStr
	}
	if err != nil {
		return output, fmt.Errorf("command failed: %w", err)
	}
	return output, nil
}
