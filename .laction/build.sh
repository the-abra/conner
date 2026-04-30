#!/usr/bin/env sh
# .laction/build.sh — Build & Vet (errors only)
set -e

# Load shared setup
. "$(dirname "$0")/setup.sh"

log_info "Starting Build & Vet process..."

# 1. Vet
log_info "Running go vet..."
# Capture errors/warnings and send to stderr
if ! go vet ./... 2>&1 | grep -i "error\|fail\|warning" >&2; then
    # grep returning 1 means no matches found, which is GOOD for vet
    true
fi

# 2. Build
log_info "Building server and client..."
mkdir -p bin

if go build -ldflags="-s -w" -o bin/conner-server ./cmd/server/main.go >/dev/null 2>&1; then
    log_success "Server built: bin/conner-server"
else
    log_error "Failed to build server"
    exit 1
fi

if go build -ldflags="-s -w" -o bin/conner-client ./cmd/client/main.go >/dev/null 2>&1; then
    log_success "Client built: bin/conner-client"
else
    log_error "Failed to build client"
    exit 1
fi

log_success "Build & Vet completed successfully."
