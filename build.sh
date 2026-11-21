#!/bin/bash
set -e

VERSION="0.2.0"
BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

LDFLAGS="-s -w -X 'alpha-nexus-egolite/internal/cli.Version=$VERSION' -X 'alpha-nexus-egolite/internal/cli.BuildDate=$BUILD_DATE' -X 'alpha-nexus-egolite/internal/cli.GitCommit=$GIT_COMMIT'"

# 发布产物与仓库里的 dist/ 是两回事：后者装着被 git 跟踪的品牌与平台资源
# （57 个文件）。早先这里 rm -rf ./dist 会把它们一并删掉，所以产物改放到 release/。
OUTPUT_DIR="./release"
rm -rf "$OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"

echo "Building Web Frontend..."
(
    cd ui-web
    npm run build
)
rm -rf internal/ui/dist
cp -r ui-web/dist internal/ui/dist
echo "Frontend built successfully!"
echo ""

echo "Building Alpha-Nexus EgoLite v$VERSION..."
echo "  Commit: $GIT_COMMIT"
echo "  Date:   $BUILD_DATE"
echo ""

build_target() {
    local GOOS=$1
    local GOARCH=$2
    local EXT=$3
    local NAME="alpha-nexus-egolite-${VERSION}-${GOOS}-${GOARCH}${EXT}"
    echo "  -> $NAME"
    GOOS=$GOOS GOARCH=$GOARCH go build -ldflags="$LDFLAGS" -trimpath -o "$OUTPUT_DIR/$NAME" ./cmd/alpha-nexus-egolite/
}

# macOS (darwin)
build_target darwin arm64 ""
build_target darwin amd64 ""

# Windows (disabled due to Unix-specific syscall dependency in internal/browser)
# build_target windows amd64 ".exe"
# build_target windows arm64 ".exe"

# Linux
build_target linux amd64 ""
build_target linux arm64 ""

echo ""
echo "Creating universal macOS binary..."
lipo -create -output "$OUTPUT_DIR/alpha-nexus-egolite" \
    "$OUTPUT_DIR/alpha-nexus-egolite-${VERSION}-darwin-arm64" \
    "$OUTPUT_DIR/alpha-nexus-egolite-${VERSION}-darwin-amd64"

chmod +x "$OUTPUT_DIR/alpha-nexus-egolite"

echo ""
echo "Generating checksums..."
cd "$OUTPUT_DIR"
shasum -a 256 * > SHA256SUMS.txt
cd ..

echo ""
echo "✅ Build complete! Output: $OUTPUT_DIR/"
ls -lh "$OUTPUT_DIR/"

# 构建完成后清理 Go 增量编译缓存，保留已生成的发布产物。
go clean -cache
echo "🧹 已自动清理 Go 编译缓存！"
