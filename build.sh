#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEB_DIR="$ROOT_DIR/web"
EMBED_DIR="$ROOT_DIR/internal/webdist/dist"
OUTPUT="${1:-${OUTPUT:-$ROOT_DIR/dist/nekode}}"

VERSION="${VERSION:-dev}"
COMMIT="${COMMIT:-$(git -C "$ROOT_DIR" rev-parse --short HEAD 2>/dev/null || printf unknown)}"
BUILD_TIME="${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
CGO_ENABLED="${CGO_ENABLED:-0}"

if ! command -v go >/dev/null 2>&1; then
  echo "go is required to build the Nekode binary" >&2
  exit 1
fi
if ! command -v npm >/dev/null 2>&1; then
  echo "npm is required to build the Web console" >&2
  exit 1
fi

GOOS="${GOOS:-$(go env GOOS)}"
GOARCH="${GOARCH:-$(go env GOARCH)}"

echo "Building Nekode Web console"
cd "$WEB_DIR"
if [ "${SKIP_NPM_INSTALL:-0}" != "1" ]; then
  npm ci
fi
npm run build
mkdir -p "$EMBED_DIR"
find "$EMBED_DIR" -mindepth 1 -maxdepth 1 ! -name .gitkeep -exec rm -rf {} +
cp -R "$WEB_DIR/dist/." "$EMBED_DIR/"

echo "Building Nekode binary"
cd "$ROOT_DIR"
mkdir -p "$(dirname "$OUTPUT")"

ldflags="-s -w"
ldflags="$ldflags -X github.com/ca-x/nekode/internal/version.Version=$VERSION"
ldflags="$ldflags -X github.com/ca-x/nekode/internal/version.Commit=$COMMIT"
ldflags="$ldflags -X github.com/ca-x/nekode/internal/version.BuildTime=$BUILD_TIME"

CGO_ENABLED="$CGO_ENABLED" GOOS="$GOOS" GOARCH="$GOARCH" go build \
  -trimpath \
  -ldflags "$ldflags" \
  -o "$OUTPUT" ./cmd/nekode

echo "Built $OUTPUT"
echo "Version: $VERSION"
echo "Commit: $COMMIT"
echo "Build time: $BUILD_TIME"
