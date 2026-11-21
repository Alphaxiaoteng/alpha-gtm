package ui

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

// jpegish builds a payload large enough to pass the size gate. It is not a
// decodable image: these tests cover the Go plumbing, not the Rust codec.
func jpegish(size int) []byte {
	payload := bytes.Repeat([]byte{0xAB}, size)
	payload[0], payload[1] = 0xFF, 0xD8
	return payload
}

// fakeEncoder writes a shell script that stands in for the Rust filter.
func fakeEncoder(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "screen-encode")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func withEncoder(t *testing.T, path string) {
	t.Helper()
	previous := screenEncoderPath
	screenEncoderPath = path
	t.Cleanup(func() { screenEncoderPath = previous })
}

func TestShrinkScreenFrameReturnsInputWithoutEncoder(t *testing.T) {
	withEncoder(t, "")
	original := base64.StdEncoding.EncodeToString(jpegish(screenEncoderMinBytes + 1))
	if got := shrinkScreenFrameJPEG(original); got != original {
		t.Fatal("without an encoder the original frame must be returned unchanged")
	}
}

func TestShrinkScreenFrameIgnoresNonJPEGOutput(t *testing.T) {
	withEncoder(t, fakeEncoder(t, "printf 'not a jpeg'"))
	original := base64.StdEncoding.EncodeToString(jpegish(screenEncoderMinBytes + 1))
	if got := shrinkScreenFrameJPEG(original); got != original {
		t.Fatal("non-JPEG encoder output must not replace the frame")
	}
}

func TestShrinkScreenFrameKeepsSmallerJPEG(t *testing.T) {
	// 一个比输入小、且以 SOI 开头的输出。
	withEncoder(t, fakeEncoder(t, `printf '\377\330small'`))
	original := base64.StdEncoding.EncodeToString(jpegish(screenEncoderMinBytes + 1))
	got := shrinkScreenFrameJPEG(original)
	if got == original {
		t.Fatal("smaller JPEG output should replace the frame")
	}
	raw, err := base64.StdEncoding.DecodeString(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) >= screenEncoderMinBytes || raw[0] != 0xFF || raw[1] != 0xD8 {
		t.Fatalf("unexpected shrunk payload: %d bytes", len(raw))
	}
}

func TestShrinkScreenFrameSkipsSmallFrames(t *testing.T) {
	// 小于阈值的帧不值得起一个进程：即使编码器存在也必须原样返回。
	called := filepath.Join(t.TempDir(), "called")
	withEncoder(t, fakeEncoder(t, "touch "+called+"; printf '\\377\\330x'"))
	original := base64.StdEncoding.EncodeToString(jpegish(1024))
	if got := shrinkScreenFrameJPEG(original); got != original {
		t.Fatal("small frames must bypass the encoder")
	}
	if _, err := os.Stat(called); err == nil {
		t.Fatal("encoder must not run for small frames")
	}
}

func TestShrinkScreenFrameSurvivesFailingEncoder(t *testing.T) {
	withEncoder(t, fakeEncoder(t, "exit 1"))
	original := base64.StdEncoding.EncodeToString(jpegish(screenEncoderMinBytes + 1))
	if got := shrinkScreenFrameJPEG(original); got != original {
		t.Fatal("a failing encoder must not break capture")
	}
}
