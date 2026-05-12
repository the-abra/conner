#!/bin/sh
set -e

# Colors
MAGENTA='\033[0;35m'
GREEN='\033[0;32m'
NC='\033[0m'

mkdir -p bin/release

echo "${MAGENTA}===> Starting Cross-Platform Release Build...${NC}"

# Linux amd64
echo "  • Building linux/amd64..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/release/conner-linux-amd64 ./cmd/conner/main.go

# Linux arm64
echo "  • Building linux/arm64..."
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o bin/release/conner-linux-arm64 ./cmd/conner/main.go

# Windows amd64
echo "  • Building windows/amd64..."
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o bin/release/conner-windows-amd64.exe ./cmd/conner/main.go

# Darwin amd64 (Intel Mac)
echo "  • Building darwin/amd64..."
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o bin/release/conner-darwin-amd64 ./cmd/conner/main.go

# Darwin arm64 (Apple Silicon)
echo "  • Building darwin/arm64..."
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o bin/release/conner-darwin-arm64 ./cmd/conner/main.go

echo "${GREEN}✓ Release artifacts generated in bin/release/:${NC}"
ls -lh bin/release/
