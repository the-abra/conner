#!/bin/sh
set -e

# Colors
MAGENTA='\033[0;35m'
GREEN='\033[0;32m'
NC='\033[0m'

mkdir -p bin/release

echo "${MAGENTA}===> Starting Cross-Platform Release Build...${NC}"
if [ -f /etc/alpine-release ]; then apk add --no-cache build-base; fi

# Linux amd64
echo "  • Building linux/amd64..."
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o bin/release/conner-linux-amd64 ./cmd/conner/main.go

# Linux arm64
echo "  • Building linux/arm64..."
CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -o bin/release/conner-linux-arm64 ./cmd/conner/main.go

# Windows amd64 (Note: Requires mingw-w64 for CGO cross-build)
echo "  • Building windows/amd64..."
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 go build -o bin/release/conner-windows-amd64.exe ./cmd/conner/main.go

# Darwin amd64 (Note: Requires macOS SDK for CGO cross-build)
echo "  • Building darwin/amd64..."
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build -o bin/release/conner-darwin-amd64 ./cmd/conner/main.go

# Darwin arm64
echo "  • Building darwin/arm64..."
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -o bin/release/conner-darwin-arm64 ./cmd/conner/main.go

echo "${GREEN}✓ Release artifacts generated in bin/release/:${NC}"
ls -lh bin/release/
