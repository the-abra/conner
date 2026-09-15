//go:build cgo

package tor

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"conner/internal/appdir"
	"conner/internal/config"

	"github.com/alexballas/go-libtor"
	"github.com/cretz/bine/control"
	binetor "github.com/cretz/bine/tor"
)

type EmbeddedTor struct {
	Instance  *binetor.Tor
	SocksAddr string
}

func StartEmbedded(ctx context.Context) (*EmbeddedTor, error) {
	dataDir := appdir.TorData()
	_ = os.MkdirAll(dataDir, 0o700)

	// Let bine pick SocksPort/ControlPort. Do not pass SocksPort 0 — that
	// conflicts with bine's nonzero default and Tor refuses to start.
	t, err := binetor.Start(ctx, &binetor.StartConf{
		ProcessCreator: libtor.Creator,
		DataDir:        dataDir,
		EnableNetwork:  false,
		NoHush:         true,
	})
	if err != nil {
		return nil, fmt.Errorf("embedded tor process: %w", err)
	}

	boot, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if err := t.EnableNetwork(boot, true); err != nil {
		_ = t.Close()
		return nil, fmt.Errorf("tor bootstrap (network blocked or slow): %w", err)
	}

	socksAddr := "127.0.0.1:9050"
	if info, err := t.Control.GetInfo("net/listeners/socks"); err == nil && len(info) > 0 {
		if v := strings.TrimSpace(info[0].Val); v != "" {
			socksAddr = normalizeSocks(v)
		}
	}

	config.TorSocksAddr = socksAddr
	fmt.Println("Embedded Tor SOCKS:", socksAddr)
	return &EmbeddedTor{Instance: t, SocksAddr: socksAddr}, nil
}

func (et *EmbeddedTor) Stop() {
	if et != nil && et.Instance != nil {
		_ = et.Instance.Close()
	}
}

func (et *EmbeddedTor) CreateServerOnion(ctx context.Context, tcpPort, httpPort int) (string, error) {
	k, err := control.KeyFromString("NEW:BEST")
	if err != nil {
		return "", err
	}
	ports := []*control.KeyVal{
		control.NewKeyVal(fmt.Sprintf("%d", tcpPort), fmt.Sprintf("127.0.0.1:%d", tcpPort)),
	}
	if httpPort > 0 {
		ports = append(ports, control.NewKeyVal("80", fmt.Sprintf("127.0.0.1:%d", httpPort)))
	}
	obs, err := et.Instance.Control.AddOnion(&control.AddOnionRequest{
		Key:   k,
		Ports: ports,
	})
	if err != nil {
		return "", fmt.Errorf("add_onion: %w", err)
	}
	return obs.ServiceID + ".onion", nil
}
