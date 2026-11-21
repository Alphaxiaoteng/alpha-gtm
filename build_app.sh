#!/bin/bash
set -e

# Add Go bin to PATH
export PATH=$PATH:$(go env GOPATH)/bin

echo "1. Building Web Frontend..."
(
    cd ui-web
    npm run build
)

echo "2. Copying frontend assets to project root dist/..."
rm -rf dist
cp -r ui-web/dist dist

echo "3. Running Wails Build..."
# Build native macOS app for Apple Silicon (faster local build)
wails build -platform darwin/arm64 -o Alpha-Nexus EgoLite

echo "✅ Wails desktop app built successfully!"
echo "App bundle: ./build/bin/Alpha-Nexus EgoLite.app"
