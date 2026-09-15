# Shared package lists for laction / CI. Sourced, not executed.
# Keep in sync with .github/workflows/ci.yml and docs/BUILDING.md.

# Unit tests (CGO_ENABLED=0 is fine; no embedded Tor)
CONNER_TEST_PKGS="./internal/crypto ./internal/invite ./internal/protocol ./internal/rooms ./internal/appdir ./internal/store ./internal/client ./internal/client/tui ./internal/clipx ./internal/vaultui ./internal/filesync ./internal/server"

# Race detector: avoid go-libtor / CGO Tor packages
CONNER_RACE_PKGS="./internal/crypto ./internal/invite ./internal/protocol ./internal/rooms ./internal/store ./internal/appdir ./internal/clipx"
