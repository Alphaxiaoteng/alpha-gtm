package agents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrimitiveReadFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.txt")
	_ = os.WriteFile(filePath, []byte("line1\nline2\nline3\n"), 0644)

	res, err := primitiveReadFile(context.Background(), dir, map[string]any{
		"path": "test.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "line1") || !strings.Contains(res, "line3") {
		t.Fatalf("unexpected output: %s", res)
	}
}

func TestPrimitiveWriteAndReplaceFile(t *testing.T) {
	dir := t.TempDir()
	_, err := primitiveWriteFile(context.Background(), dir, map[string]any{
		"path":    "hello.py",
		"content": "print('hello')",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = primitiveReplaceFile(context.Background(), dir, map[string]any{
		"path":     "hello.py",
		"old_text": "hello",
		"new_text": "world",
	})
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "hello.py"))
	if string(data) != "print('world')" {
		t.Fatalf("unexpected file content: %s", string(data))
	}
}

func TestPrimitiveRunCommand(t *testing.T) {
	dir := t.TempDir()
	res, err := primitiveRunCommand(context.Background(), dir, map[string]any{
		"command": "echo 'pi agent fast'",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "pi agent fast") {
		t.Fatalf("unexpected output: %s", res)
	}
}
