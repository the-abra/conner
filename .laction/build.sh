#!/usr/bin/env sh
# .laction/build.sh — Build & Vet (errors only)
set -e

apk add --no-cache gcc musl-dev git >/dev/null 2>&1 || true
go mod download >/dev/null 2>&1
go mod verify >/dev/null 2>&1
go vet ./... 2>&1 | grep -i "error\|fail\|warning" >&2 || true
go build -ldflags="-s -w" -o bin/conner-server ./cmd/server/main.go >/dev/null
go build -ldflags="-s -w" -o bin/conner-client ./cmd/client/main.go >/dev/null

