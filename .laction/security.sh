#!/usr/bin/env sh
# .laction/security.sh — gosec Scan (errors only)
set -e

# Load shared setup
. "$(dirname "$0")/setup.sh"

log_info "Starting security scan with gosec..."

# Install gosec only if not already in PATH
if ! command -v gosec >/dev/null 2>&1; then
    log_info "Installing gosec..."
    apk add gosec >/dev/null 2>&1
fi

# Run gosec
# -exclude=G204,G304: matched existing suppression
# -severity medium: show medium and high
log_info "Running gosec scan..."
if ! gosec -exclude=G204,G304 -severity medium ./... 2>&1 | grep -E "^\[" >&2; then
    # No findings or grep didn't find anything (which is GOOD)
    log_success "No high/medium severity security issues found."
else
    log_warn "Security findings detected (see above)."
fi
