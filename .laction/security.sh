#!/bin/sh
set -e

. "$(dirname "$0")/color.sh"

warn "gosec..."
if ! command -v gosec >/dev/null 2>&1; then
	go install github.com/securego/gosec/v2/cmd/gosec@latest
fi

gosec -fmt=sarif -out=security-report.sarif \
	-exclude-dir=.laction \
	-exclude-generated \
	./... || true

ok "gosec wrote security-report.sarif (warnings do not fail the job)."
