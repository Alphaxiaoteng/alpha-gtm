package observability

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	DefaultMaxBytes int64 = 10 << 20
	DefaultMaxFiles       = 5
)

var ErrRecordTooLarge = errors.New("observability record exceeds rotation limit")

type FileOptions struct {
	Options
	MaxBytes int64
	MaxFiles int
}

// FileEmitter writes JSONL to a 0600 file and keeps at most MaxFiles files,
// including the active file.
type FileEmitter struct {
	json   *JSONEmitter
	writer *rotatingWriter
}

func NewFileEmitter(path string, opts FileOptions) (*FileEmitter, error) {
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	maxFiles := opts.MaxFiles
	if maxFiles <= 0 {
		maxFiles = DefaultMaxFiles
	}
	w, err := openRotatingWriter(path, maxBytes, maxFiles)
	if err != nil {
		return nil, err
	}
	return &FileEmitter{json: NewJSONEmitter(w, opts.Options), writer: w}, nil
}

func (e *FileEmitter) Emit(ctx context.Context, event Event) error {
	return e.json.Emit(ctx, event)
}

func (e *FileEmitter) Close() error {
	return e.writer.Close()
}

type rotatingWriter struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	maxFiles int
	file     *os.File
	size     int64
}

func openRotatingWriter(path string, maxBytes int64, maxFiles int) (*rotatingWriter, error) {
	if path == "" {
		return nil, fmt.Errorf("log path required")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	w := &rotatingWriter{path: path, maxBytes: maxBytes, maxFiles: maxFiles}
	if err := w.openLocked(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *rotatingWriter) openLocked() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open diagnostic log: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return fmt.Errorf("secure diagnostic log: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("stat diagnostic log: %w", err)
	}
	// A previous crash or older configuration may leave an oversized active
	// file. Truncate it before accepting writes so every managed file starts
	// within the configured bound; rotating it would merely create an oversized
	// backup that can never shrink.
	if info.Size() > w.maxBytes {
		if err := f.Truncate(0); err != nil {
			_ = f.Close()
			return fmt.Errorf("truncate oversized diagnostic log: %w", err)
		}
		info, err = f.Stat()
		if err != nil {
			_ = f.Close()
			return fmt.Errorf("stat truncated diagnostic log: %w", err)
		}
	}
	w.file = f
	w.size = info.Size()
	return nil
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return 0, fmt.Errorf("diagnostic log is closed")
	}
	if int64(len(p)) > w.maxBytes {
		return 0, fmt.Errorf("%w: %d bytes", ErrRecordTooLarge, len(p))
	}
	if w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotateLocked(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingWriter) rotateLocked() error {
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("close diagnostic log for rotation: %w", err)
	}
	w.file = nil

	var rotateErr error
	if w.maxFiles == 1 {
		rotateErr = removeIfExists(w.path)
	} else {
		rotateErr = removeIfExists(rotationPath(w.path, w.maxFiles-1))
		for i := w.maxFiles - 2; rotateErr == nil && i >= 1; i-- {
			rotateErr = renameIfExists(rotationPath(w.path, i), rotationPath(w.path, i+1))
		}
		if rotateErr == nil {
			rotateErr = renameIfExists(w.path, rotationPath(w.path, 1))
		}
	}
	openErr := w.openLocked()
	if rotateErr != nil {
		return fmt.Errorf("rotate diagnostic log: %w", rotateErr)
	}
	return openErr
}

func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func rotationPath(path string, n int) string {
	return fmt.Sprintf("%s.%d", path, n)
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func renameIfExists(from, to string) error {
	if err := removeIfExists(to); err != nil {
		return err
	}
	err := os.Rename(from, to)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
