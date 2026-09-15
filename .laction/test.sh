#!/bin/sh
set -e

. "$(dirname "$0")/pkgs.sh"
. "$(dirname "$0")/color.sh"
. "$(dirname "$0")/cc.sh"

note "Unit tests (count=1)..."
CGO_ENABLED=0 go test -count=1 $CONNER_TEST_PKGS

if ensure_cc; then
	note "Race (no Tor/CGO packages)..."
	CGO_ENABLED=1 go test -count=1 -race $CONNER_RACE_PKGS
else
	printf '  skip -race (no C compiler)\n'
fi

ok "Tests ok."
