#!/usr/bin/env sh
# .laction/test.sh — Test (errors only)
set -e

apk add --no-cache gcc musl-dev git >/dev/null 2>&1 || true
go mod download >/dev/null 2>&1

# -v output piped — only failed test lines reach stderr
go test -race -coverprofile=coverage.out ./... 2>&1 \
    | grep -E "^(FAIL|---\s+FAIL|panic)" >&2 || true

