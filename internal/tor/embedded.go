package tor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/alexballas/go-libtor"
	"github.com/cretz/bine/control"
	"github.com/cretz/bine/tor"
	"net"
	"time"
)

type EmbeddedTor struct {
	Instance  *tor.Tor
	SocksAddr string
}

func findFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func StartEmbedded(ctx context.Context) (*EmbeddedTor, error) {
	// Use our local data directory
	wd, _ := os.Getwd()
	dataDir := filepath.Join(wd, ".conner_data", "tor_embedded")
	_ = os.MkdirAll(dataDir, 0700)

	// Proactively find a free port to avoid conflicts with system Tor (9050)
	freePort, err := findFreePort()
	if err != nil {
		freePort = 9050 // Last resort
	}
	socksAddr := fmt.Sprintf("127.0.0.1:%d", freePort)

	// Start Tor using go-libtor as the process creator
	t, err := tor.Start(ctx, &tor.StartConf{
		ProcessCreator: libtor.Creator,
		DataDir:        dataDir,
		// DebugWriter:    os.Stderr, // Silence for production
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start embedded tor: %w", err)
	}

	// Use the controller to set the SOCKS port explicitly
	fmt.Printf("[*] Configuring Tor SOCKS on %s...\n", socksAddr)
	err = t.Control.SetConf(&control.KeyVal{Key: "SocksPort", Val: socksAddr})
	if err != nil {
		return nil, fmt.Errorf("failed to configure socks port via controller: %w", err)
	}

	// IMPORTANT: bine starts Tor with DisableNetwork=1. We must enable it to bootstrap.
	fmt.Println("[*] Activating Tor network...")
	err = t.Control.SetConf(&control.KeyVal{Key: "DisableNetwork", Val: "0"})
	if err != nil {
		return nil, fmt.Errorf("failed to enable tor network: %w", err)
	}

	// Wait a moment for the listener to be established
	time.Sleep(2 * time.Second)

	fmt.Printf("[*] Tor SOCKS Listener: %s (Dynamic)\n", socksAddr)

	return &EmbeddedTor{
		Instance:  t,
		SocksAddr: socksAddr,
	}, nil
}

func (et *EmbeddedTor) Stop() {
	if et.Instance != nil {
		_ = et.Instance.Close()
	}
}

func (et *EmbeddedTor) CreateServerOnion(ctx context.Context, tcpPort int) (string, error) {
	// ADD_ONION NEW:BEST Port=6666,127.0.0.1:tcp
	k, _ := control.KeyFromString("NEW:BEST")
	obs, err := et.Instance.Control.AddOnion(&control.AddOnionRequest{
		Key: k,
		Ports: []*control.KeyVal{
			control.NewKeyVal(fmt.Sprintf("%d", tcpPort), fmt.Sprintf("127.0.0.1:%d", tcpPort)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to add onion: %w", err)
	}
	return obs.ServiceID + ".onion", nil
}
