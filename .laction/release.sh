#!/usr/bin/env sh
# .laction/release.sh — Cross-Platform Release (errors only)
set -e

apk add --no-cache gcc musl-dev git >/dev/null 2>&1 || true
go mod download >/dev/null 2>&1
mkdir -p bin

build_target() {
    GOOS="$1" GOARCH="$2"
    SUFFIX="$3"
    EXT=""; [ "$GOOS" = "windows" ] && EXT=".exe"

    GOOS=$GOOS GOARCH=$GOARCH CGO_ENABLED=0 \
        go build -ldflags="-s -w" \
        -o "bin/conner-server-${SUFFIX}${EXT}" ./cmd/server/main.go >/dev/null \
        || { echo "[ERROR] server ${SUFFIX}" >&2; exit 1; }

    GOOS=$GOOS GOARCH=$GOARCH CGO_ENABLED=0 \
        go build -ldflags="-s -w" \
        -o "bin/conner-client-${SUFFIX}${EXT}" ./cmd/client/main.go >/dev/null \
        || { echo "[ERROR] client ${SUFFIX}" >&2; exit 1; }
}

build_target linux   amd64  linux-amd64
build_target linux   arm64  linux-arm64
build_target darwin  amd64  darwin-amd64
build_target darwin  arm64  darwin-arm64
build_target windows amd64  windows-amd64

