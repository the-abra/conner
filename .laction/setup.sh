#!/usr/bin/env sh
# .laction/setup.sh — Shared setup and logging for laction scripts

# Colors for output (only if terminal supports it)
if [ -t 1 ]; then
    BLUE='\033[0;34m'
    GREEN='\033[0;32m'
    YELLOW='\033[0;33m'
    RED='\033[0;31m'
    NC='\033[0m' # No Color
else
    BLUE=''
    GREEN=''
    YELLOW=''
    RED=''
    NC=''
fi

log_info() {
    printf "${BLUE}[INFO]${NC} %s\n" "$1"
}

log_success() {
    printf "${GREEN}[OK]${NC} %s\n" "$1"
}

log_warn() {
    printf "${YELLOW}[WARN]${NC} %s\n" "$1" >&2
}

log_error() {
    printf "${RED}[ERROR]${NC} %s\n" "$1" >&2
}

# Ensure required packages are installed (Alpine)
setup_packages() {
    log_info "Ensuring system dependencies..."
    apk add --no-cache gcc musl-dev git >/dev/null 2>&1 || true
}

# Ensure Go modules are ready
setup_go_mods() {
    log_info "Downloading Go modules..."
    go mod download >/dev/null 2>&1
    go mod verify >/dev/null 2>&1
}

# Ensure GOPATH bin is in PATH
export PATH="$(go env GOPATH)/bin:$PATH"

# Default setup
setup_packages
setup_go_mods
