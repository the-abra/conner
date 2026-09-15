# Building and deployment

Go **1.26+**. Module is locked (`go.sum`); CI does **not** run `go mod tidy`.

Laction / Docker build image is **`golang:1.26-bookworm`** (glibc + gcc) so `--server --tor` can embed go-libtor. Alpine musl is a poor fit for that CGO stack; Arch is not pinned to Go 1.26.

## Binary

```bash
# Onion engine (go-libtor)
CGO_ENABLED=1 go build -trimpath -buildvcs=false -ldflags="-s -w" -o conner ./cmd/conner

# Tests / LAN without embedding Tor
CGO_ENABLED=0 go build -o conner ./cmd/conner
```

`CGO_ENABLED=0` (or a nocgo `laction` leftover binary) **cannot** start embedded Tor — both hub and client hit the same stub. Rebuild with CGO, or `--pt system-tor`.

Embedded Tor data dir is `~/.conner/tor` (not cwd `.conner_data`). Bootstrap waits up to 90s. SOCKS uses an ephemeral localhost port so it does not fight a system `tor` on 9050. Clients dial that port when `EmbeddedTor` is passed in.

## Tests

```bash
CGO_ENABLED=0 go test ./internal/crypto ./internal/invite ./internal/protocol \
  ./internal/rooms ./internal/appdir ./internal/store ./internal/client ./internal/client/tui \
  ./internal/clipx ./internal/vaultui ./internal/filesync ./internal/server

# race (needs CGO; skip Tor packages)
CGO_ENABLED=1 go test -race ./internal/crypto ./internal/invite ./internal/protocol ./internal/rooms ./internal/store ./internal/appdir ./internal/clipx
```

Package lists live in `.laction/pkgs.sh`. Color codes are only emitted when stdout is a TTY (`FORCE_COLOR=1` to force; `NO_COLOR` to disable). Laction captures logs as a pipe, so default output is plain text — that is expected.

```
`laction .            # bookworm image: vet + nocgo + CGO/embedded Tor
laction . test       # unit tests; -race if compiler present
laction . release    # linux amd64 nocgo + CGO artifacts, SBOM.txt
laction . security   # gosec → security-report.sarif (non-fatal)
```

GitHub: `.github/workflows/ci.yml` (no `go mod tidy`). `.laction/static.sh` (host-specific `/opt` copy) was removed.

## Docker

```bash
docker build -t conner:latest .
docker run --name conner-hub conner --server --tor
```

No `go mod tidy` in the Dockerfile. `NET_ADMIN` is **not** required unless you add your own iptables.

## Release

`.laction/release.sh` always builds `conner-linux-amd64-nocgo`, plus CGO linux/amd64 (and arm64 if the host can) when a C compiler exists. `SBOM.txt` is `go version` + `go list -m all`. Windows/Darwin CGO from Linux is not supported.
