#!/bin/sh
set -e

# Colors
BLUE='\033[0;34m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

echo "${BLUE}===> [1/3] Preparing environment...${NC}"
if [ -f /etc/alpine-release ]; then apk add --no-cache build-base; fi
go mod tidy

echo "${BLUE}===> [2/3] Running go vet...${NC}"
go vet ./...

echo "${BLUE}===> [3/3] Compilation check...${NC}"
CGO_ENABLED=1 go build -o conner ./cmd/conner/main.go

echo "${GREEN}✓ Build & Vet completed successfully.${NC}"
