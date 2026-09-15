#!/bin/sh
set -e

. "$(dirname "$0")/color.sh"
. "$(dirname "$0")/cc.sh"

mkdir -p bin/release

hdr "Release artifacts..."
printf '  • linux/amd64 nocgo (always)\n'
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags="-s -w" -o bin/release/conner-linux-amd64-nocgo ./cmd/conner

if ensure_cc; then
	printf '  • linux/amd64 CGO (embedded Tor)\n'
	CGO_ENABLED=1 CGO_CFLAGS="-w" GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags="-s -w" -o bin/release/conner-linux-amd64 ./cmd/conner
	printf '  • linux/arm64 CGO (may skip on this host)\n'
	CGO_ENABLED=1 CGO_CFLAGS="-w" GOOS=linux GOARCH=arm64 go build -trimpath -buildvcs=false -ldflags="-s -w" -o bin/release/conner-linux-arm64 ./cmd/conner || printf 'skip arm64 cgo\n'
else
	printf '  skip CGO artifacts (no compiler)\n'
fi

{
	echo "CONNER SBOM (module list, not SPDX)"
	go version
	echo "module conner"
	go list -m all
} > bin/release/SBOM.txt

ok "bin/release/"
ls -lh bin/release/ || true
