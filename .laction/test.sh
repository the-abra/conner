#!/usr/bin/env sh
# .laction/test.sh — Test (errors only)
set -e

# Load shared setup
. "$(dirname "$0")/setup.sh"

log_info "Starting tests (race detector enabled)..."

# Run tests, capture failure patterns
# We use a temporary file to capture output so we can check if anything failed
TEST_LOG=$(mktemp)
trap 'rm -f "$TEST_LOG"' EXIT

if go test -race -coverprofile=coverage.out ./... > "$TEST_LOG" 2>&1; then
    log_success "All tests passed!"
else
    log_error "Tests failed. Showing relevant errors:"
    grep -E "^(FAIL|---\s+FAIL|panic|#)" "$TEST_LOG" >&2
    exit 1
fi
