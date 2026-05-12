#!/bin/sh
set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

echo "${YELLOW}===> Initiating Security Scan (gosec)...${NC}"

# Check if gosec is installed
if ! command -v gosec >/dev/null 2>&1; then
    echo "  • ${YELLOW}gosec not found. Installing...${NC}"
    # Install to GOBIN
    go install github.com/securego/gosec/v2/cmd/gosec@latest
fi

echo "  • Running scan..."
# Run gosec on the current directory
# -exclude-dir=.laction excludes this script directory
gosec -fmt=sarif -out=security-report.sarif -exclude-dir=.laction ./...

echo "${GREEN}✓ Security scan completed successfully.${NC}"
