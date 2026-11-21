//go:build !legacy_chromium

package ui

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func persistAccountAvatarSnapshot(dataDir string, accountID int64, payload []byte) (string, string, error) {
	if accountID <= 0 {
		return "", "", fmt.Errorf("account id must be positive")
	}
	if len(payload) == 0 || len(payload) > 2<<20 {
		return "", "", fmt.Errorf("avatar snapshot size is invalid")
	}
	config, err := png.DecodeConfig(bytes.NewReader(payload))
	if err != nil || config.Width < 16 || config.Height < 16 || config.Width > 2048 || config.Height > 2048 {
		return "", "", fmt.Errorf("avatar snapshot is not a supported PNG")
	}
	ratio := float64(config.Width) / float64(config.Height)
	if ratio < 0.65 || ratio > 1.55 {
		return "", "", fmt.Errorf("avatar snapshot aspect ratio is invalid")
	}
	normalized, err := normalizeAvatarPNG(payload)
	if err != nil {
		return "", "", fmt.Errorf("normalize avatar snapshot: %w", err)
	}
	payload = normalized
	if !avatarSnapshotHasVisualContent(payload) {
		return "", "", fmt.Errorf("avatar snapshot is a uniform neutral placeholder")
	}
	digest := sha256.Sum256(payload)
	sha := hex.EncodeToString(digest[:])
	directory, err := safeAvatarDirectory(dataDir, true)
	if err != nil {
		return "", "", err
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return "", "", fmt.Errorf("restrict avatar directory: %w", err)
	}
	name := fmt.Sprintf("account-%d-%s.png", accountID, sha[:16])
	destination := filepath.Join(directory, name)
	if info, err := os.Lstat(destination); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", "", fmt.Errorf("avatar target is not a regular managed file")
		}
		existing, readErr := os.ReadFile(destination)
		if readErr != nil {
			return "", "", readErr
		}
		existingDigest := sha256.Sum256(existing)
		if existingDigest != digest {
			return "", "", fmt.Errorf("avatar target content does not match its digest")
		}
		return filepath.ToSlash(filepath.Join("account-avatars", name)), sha, nil
	} else if !os.IsNotExist(err) {
		return "", "", err
	}
	temporary, err := os.CreateTemp(directory, ".avatar-*.tmp")
	if err != nil {
		return "", "", err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return "", "", err
	}
	if _, err := temporary.Write(payload); err != nil {
		_ = temporary.Close()
		return "", "", err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return "", "", err
	}
	if err := temporary.Close(); err != nil {
		return "", "", err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return "", "", err
	}
	return filepath.ToSlash(filepath.Join("account-avatars", name)), sha, nil
}

// recoverLatestValidAccountAvatarSnapshot keeps a verified identity usable
// when its creator page temporarily exposes only a neutral loading image.
// It never crosses account IDs and accepts only digest-named managed PNG files.
func recoverLatestValidAccountAvatarSnapshot(dataDir string, accountID int64) (string, string, error) {
	if accountID <= 0 {
		return "", "", fmt.Errorf("account id must be positive")
	}
	directory, err := safeAvatarDirectory(dataDir, false)
	if err != nil {
		return "", "", err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", "", err
	}
	prefix := fmt.Sprintf("account-%d-", accountID)
	var recoveredRef, recoveredSHA string
	var recoveredAt time.Time
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".png") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > 2<<20 {
			continue
		}
		payload, readErr := os.ReadFile(filepath.Join(directory, name))
		if readErr != nil {
			continue
		}
		normalized, normalizeErr := normalizeAvatarPNG(payload)
		if normalizeErr != nil || !avatarSnapshotHasVisualContent(normalized) {
			continue
		}
		digest := sha256.Sum256(payload)
		sha := hex.EncodeToString(digest[:])
		if name != prefix+sha[:16]+".png" || (!recoveredAt.IsZero() && !info.ModTime().After(recoveredAt)) {
			continue
		}
		recoveredAt = info.ModTime()
		recoveredRef = filepath.ToSlash(filepath.Join("account-avatars", name))
		recoveredSHA = sha
	}
	if recoveredRef == "" {
		return "", "", fmt.Errorf("no valid historical avatar snapshot found")
	}
	return recoveredRef, recoveredSHA, nil
}

// normalizeAvatarPNG removes a large one-sided margin, or excessive solid
// canvas around very small artwork. The result remains square so every UI size
// sees the same centered portrait and object-fit never crops a second time.
// ponytail: this is deliberately color-boundary based; it does not attempt
// face/logo detection.
func normalizeAvatarPNG(payload []byte) ([]byte, error) {
	source, err := png.Decode(bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	bounds := source.Bounds()
	if bounds.Dx() != bounds.Dy() {
		return payload, nil
	}
	background := color.NRGBAModel.Convert(source.At(bounds.Min.X, bounds.Min.Y)).(color.NRGBA)
	if !sameAvatarBackground(source.At(bounds.Max.X-1, bounds.Min.Y), background) ||
		!sameAvatarBackground(source.At(bounds.Min.X, bounds.Max.Y-1), background) ||
		!sameAvatarBackground(source.At(bounds.Max.X-1, bounds.Max.Y-1), background) {
		return payload, nil
	}

	content, found := avatarContentBounds(source, bounds, background)
	if !found || content == bounds {
		return payload, nil
	}
	left, right := content.Min.X-bounds.Min.X, bounds.Max.X-content.Max.X
	top, bottom := content.Min.Y-bounds.Min.Y, bounds.Max.Y-content.Max.Y
	minimumMargin := bounds.Dx() / 12
	if minimumMargin < 4 {
		minimumMargin = 4
	}
	oneSided := (left >= minimumMargin && right == 0 && top == 0 && bottom == 0) ||
		(right >= minimumMargin && left == 0 && top == 0 && bottom == 0) ||
		(top >= minimumMargin && bottom == 0 && left == 0 && right == 0) ||
		(bottom >= minimumMargin && top == 0 && left == 0 && right == 0)
	ratio := float64(content.Dx()) / float64(content.Dy())
	oneSided = oneSided && content.Dx() >= 16 && content.Dy() >= 16 && ratio >= 0.8 && ratio <= 1.25
	canvasArea := float64(bounds.Dx() * bounds.Dy())
	contentArea := float64(content.Dx() * content.Dy())
	maxContentSide := content.Dx()
	if content.Dy() > maxContentSide {
		maxContentSide = content.Dy()
	}
	sparseSolidCanvas := maxContentSide >= 16 &&
		content.Dx() >= 4 && content.Dy() >= 4 &&
		float64(maxContentSide)/float64(bounds.Dx()) <= 0.75 &&
		contentArea/canvasArea <= 0.12
	if !oneSided && !sparseSolidCanvas {
		return payload, nil
	}

	side := maxContentSide
	if sparseSolidCanvas {
		side += maxContentSide / 4
	}
	result := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.Draw(result, result.Bounds(), &image.Uniform{C: background}, image.Point{}, draw.Src)
	offset := image.Pt((side-content.Dx())/2, (side-content.Dy())/2)
	destination := image.Rectangle{Min: offset, Max: offset.Add(content.Size())}
	draw.Draw(result, destination, source, content.Min, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, result); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

// avatarSnapshotHasVisualContent rejects the neutral loading placeholder used
// by creator consoles before an account image finishes painting. A uniform
// saturated brand/avatar color is retained; real photos and logos naturally
// have channel variation.
func avatarSnapshotHasVisualContent(payload []byte) bool {
	source, err := png.Decode(bytes.NewReader(payload))
	if err != nil {
		return false
	}
	bounds := source.Bounds()
	if bounds.Empty() {
		return false
	}
	minimum := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	maximum := color.NRGBA{}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			current := color.NRGBAModel.Convert(source.At(x, y)).(color.NRGBA)
			if current.R < minimum.R {
				minimum.R = current.R
			}
			if current.G < minimum.G {
				minimum.G = current.G
			}
			if current.B < minimum.B {
				minimum.B = current.B
			}
			if current.A < minimum.A {
				minimum.A = current.A
			}
			if current.R > maximum.R {
				maximum.R = current.R
			}
			if current.G > maximum.G {
				maximum.G = current.G
			}
			if current.B > maximum.B {
				maximum.B = current.B
			}
			if current.A > maximum.A {
				maximum.A = current.A
			}
		}
	}
	if avatarChannelDistance(minimum.R, maximum.R) > 8 ||
		avatarChannelDistance(minimum.G, maximum.G) > 8 ||
		avatarChannelDistance(minimum.B, maximum.B) > 8 ||
		avatarChannelDistance(minimum.A, maximum.A) > 8 {
		return true
	}
	base := color.NRGBAModel.Convert(source.At(bounds.Min.X, bounds.Min.Y)).(color.NRGBA)
	channelMin, channelMax := base.R, base.R
	for _, value := range []uint8{base.G, base.B} {
		if value < channelMin {
			channelMin = value
		}
		if value > channelMax {
			channelMax = value
		}
	}
	return avatarChannelDistance(channelMin, channelMax) > 12
}

func avatarContentBounds(source image.Image, bounds image.Rectangle, background color.NRGBA) (image.Rectangle, bool) {
	content := image.Rectangle{Min: bounds.Max, Max: bounds.Min}
	found := false
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if sameAvatarBackground(source.At(x, y), background) {
				continue
			}
			if !found {
				content = image.Rect(x, y, x+1, y+1)
				found = true
				continue
			}
			if x < content.Min.X {
				content.Min.X = x
			}
			if y < content.Min.Y {
				content.Min.Y = y
			}
			if x >= content.Max.X {
				content.Max.X = x + 1
			}
			if y >= content.Max.Y {
				content.Max.Y = y + 1
			}
		}
	}
	return content, found
}

func sameAvatarBackground(value color.Color, background color.NRGBA) bool {
	current := color.NRGBAModel.Convert(value).(color.NRGBA)
	return avatarChannelDistance(current.R, background.R) <= 4 &&
		avatarChannelDistance(current.G, background.G) <= 4 &&
		avatarChannelDistance(current.B, background.B) <= 4 &&
		avatarChannelDistance(current.A, background.A) <= 4
}

func avatarChannelDistance(left, right uint8) uint8 {
	if left > right {
		return left - right
	}
	return right - left
}

func managedAvatarPath(dataDir, reference string) (string, error) {
	reference = filepath.ToSlash(strings.TrimSpace(reference))
	if reference == "" || filepath.IsAbs(reference) || !strings.HasPrefix(reference, "account-avatars/") || strings.Contains(reference, "../") {
		return "", fmt.Errorf("avatar is not a managed reference")
	}
	root, err := safeAvatarDirectory(dataDir, false)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(filepath.FromSlash(reference))
	resolved := filepath.Clean(filepath.Join(dataDir, clean))
	info, err := os.Lstat(resolved)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("avatar target is not a regular managed file")
	}
	realTarget, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve avatar target: %w", err)
	}
	if realTarget == root || !strings.HasPrefix(realTarget, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("avatar reference escapes managed directory")
	}
	return realTarget, nil
}

func safeAvatarDirectory(dataDir string, create bool) (string, error) {
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return "", fmt.Errorf("resolve data directory: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve data directory: %w", err)
	}
	directory := filepath.Join(root, "account-avatars")
	info, err := os.Lstat(directory)
	if os.IsNotExist(err) && create {
		if err := os.Mkdir(directory, 0700); err != nil {
			return "", fmt.Errorf("create avatar directory: %w", err)
		}
		info, err = os.Lstat(directory)
	}
	if err != nil {
		return "", fmt.Errorf("inspect avatar directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("avatar directory must be a real directory")
	}
	realDirectory, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return "", fmt.Errorf("resolve avatar directory: %w", err)
	}
	expected := filepath.Join(realRoot, "account-avatars")
	if realDirectory != expected {
		return "", fmt.Errorf("avatar directory escapes data directory")
	}
	return realDirectory, nil
}
