#!/bin/sh
set -e

. "$(dirname "$0")/color.sh"
. "$(dirname "$0")/cc.sh"

info "[1/4] Modules (locked go.sum, no tidy)..."
go mod download
go mod verify

info "[2/4] go vet..."
CGO_ENABLED=0 go vet ./...

info "[3/4] nocgo binary (LAN / --pt system-tor)..."
CGO_ENABLED=0 go build -trimpath -buildvcs=false -o /tmp/conner-nocgo ./cmd/conner

info "[4/4] CGO binary (embedded Tor)..."
if ensure_cc; then
	CGO_ENABLED=1 CGO_CFLAGS="-w" go build -trimpath -buildvcs=false -ldflags="-s -w" -o conner ./cmd/conner
else
	printf '  no C compiler; skip embedded-Tor (use --pt system-tor)\n'
	cp /tmp/conner-nocgo conner
fi

ok "Build & vet ok."
