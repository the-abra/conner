#!/usr/bin/env sh
# .laction/release.sh — Cross-Platform Release (errors only)
set -e

# Load shared setup
. "$(dirname "$0")/setup.sh"

log_info "Starting cross-platform release build..."
mkdir -p bin

build_target() {
    GOOS="$1"
    GOARCH="$2"
    SUFFIX="$3"
    EXT=""
    [ "$GOOS" = "windows" ] && EXT=".exe"

    log_info "Building for ${GOOS}/${GOARCH} (${SUFFIX})..."

    # Server
    if ! GOOS=$GOOS GOARCH=$GOARCH CGO_ENABLED=0 \
        go build -ldflags="-s -w" \
        -o "bin/conner-server-${SUFFIX}${EXT}" ./cmd/server/main.go >/dev/null 2>&1; then
        log_error "Failed to build server for ${SUFFIX}"
        return 1
    fi

    # Client
    if ! GOOS=$GOOS GOARCH=$GOARCH CGO_ENABLED=0 \
        go build -ldflags="-s -w" \
        -o "bin/conner-client-${SUFFIX}${EXT}" ./cmd/client/main.go >/dev/null 2>&1; then
        log_error "Failed to build client for ${SUFFIX}"
        return 1
    fi
    
    log_success "Built ${SUFFIX} binaries"
}

# Define targets
build_target linux   amd64  linux-amd64
build_target linux   arm64  linux-arm64
build_target darwin  amd64  darwin-amd64
build_target darwin  arm64  darwin-arm64
build_target windows amd64  windows-amd64

log_success "All release binaries generated in bin/"
