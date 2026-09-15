//go:build !cgo

package tor

import (
	"context"
	"fmt"
)

// EmbeddedTor is a stub when CGO is off — use system Tor (--pt system-tor).
type EmbeddedTor struct {
	SocksAddr string
}

func StartEmbedded(ctx context.Context) (*EmbeddedTor, error) {
	return nil, fmt.Errorf("this binary was built without CGO (no embedded Tor)\n  rebuild: CGO_ENABLED=1 go build -o conner ./cmd/conner\n  or:      ./conner --server --tor --pt system-tor   # needs tor SOCKS 127.0.0.1:9050")
}

func (et *EmbeddedTor) Stop() {}

func (et *EmbeddedTor) CreateServerOnion(ctx context.Context, tcpPort, httpPort int) (string, error) {
	return "", fmt.Errorf("embedded Tor requires CGO")
}
