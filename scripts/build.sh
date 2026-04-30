#!/bin/bash
# scripts/build.sh
set -e

echo "Building CONNER Server and Client..."

# Create bin directory
mkdir -p bin

# Build Server
echo "Building Server (Linux)..."
GOOS=linux GOARCH=amd64 go build -o bin/conner-server-linux-amd64 cmd/server/main.go
echo "Building Server (Windows)..."
GOOS=windows GOARCH=amd64 go build -o bin/conner-server-windows-amd64.exe cmd/server/main.go
echo "Building Server (macOS)..."
GOOS=darwin GOARCH=arm64 go build -o bin/conner-server-darwin-arm64 cmd/server/main.go

# Build Client
echo "Building Client (Linux)..."
GOOS=linux GOARCH=amd64 go build -o bin/conner-client-linux-amd64 cmd/client/main.go
echo "Building Client (Windows)..."
GOOS=windows GOARCH=amd64 go build -o bin/conner-client-windows-amd64.exe cmd/client/main.go
echo "Building Client (macOS)..."
GOOS=darwin GOARCH=arm64 go build -o bin/conner-client-darwin-arm64 cmd/client/main.go

echo "Build complete! Check the bin/ directory."
