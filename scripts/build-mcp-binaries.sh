#!/usr/bin/env bash
# Cross-compile the VigilAgent MCP server binary for all supported platforms.
#
# Output (repo root):
#   dist/vigilagent-mcp-<os>-<arch>[.exe]
#
# These exact asset names are what mcp-server/scripts/install.js downloads
# from GitHub Releases, so keep the naming in sync.
set -euo pipefail

cd "$(dirname "$0")/.."

OUT="dist"
mkdir -p "$OUT"

export CGO_ENABLED=0

build() {
  local os="$1" arch="$2"
  local exe=""
  [ "$os" = "windows" ] && exe=".exe"
  local name="vigilagent-mcp-${os}-${arch}${exe}"
  echo "==> building ${name}"
  GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags="-s -w" -o "${OUT}/${name}" ./cmd/mcp
}

build linux amd64
build linux arm64
build darwin amd64
build darwin arm64
build windows amd64
build windows arm64

echo "==> done:"
ls -la "$OUT"
