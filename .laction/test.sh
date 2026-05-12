#!/bin/sh
set -e

# Colors
CYAN='\033[0;36m'
GREEN='\033[0;32m'
NC='\033[0m'

echo "${CYAN}===> Running tests with Race Detector...${NC}"
go test -v -race ./...

echo "${GREEN}✓ Tests completed successfully.${NC}"
