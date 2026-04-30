#!/usr/bin/env sh
# .laction/security.sh — gosec Scan (errors only)
set -e

apk add --no-cache gcc musl-dev git >/dev/null 2>&1 || true
go mod download >/dev/null 2>&1
go install github.com/securego/gosec/v2/cmd/gosec@latest >/dev/null 2>&1

# gosec: only show severity HIGH/MEDIUM findings; suppress info noise
gosec -exclude=G204,G304 -severity medium ./... 2>&1 \
    | grep -E "^\[" >&2 || true


