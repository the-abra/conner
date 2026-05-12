package client

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// GenerateToken creates a random authentication token for the ephemeral service.
func GenerateToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// StartEphemeralService starts a local HTTP server on a random port and uses the Tor Control Port
// to create an ephemeral hidden service (.onion) pointing to it.
// It returns the .onion address and any error encountered.
func StartEphemeralService(filePath string, token string) (string, error) {
	// 1. Start local HTTP server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("failed to start local server: %w", err)
	}
	localPort := listener.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+token {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		http.ServeFile(w, r, filePath)
	})

	go func() {
		_ = http.Serve(listener, mux)
	}()

	// 2. Connect to Tor Control Port
	conn, err := net.DialTimeout("tcp", "127.0.0.1:9051", 5*time.Second)
	if err != nil {
		return "", fmt.Errorf("could not connect to Tor Control Port: %w", err)
	}
	defer conn.Close()

	// 3. Authenticate using Cookie
	cookiePaths := []string{
		"/var/lib/tor/control_auth_cookie",
		"/var/run/tor/control.auth_cookie",
		"/tmp/tor-conner/control_auth_cookie",
	}
	
	var cookieBytes []byte
	var cookieErr error
	for _, p := range cookiePaths {
		cookieBytes, cookieErr = os.ReadFile(p)
		if cookieErr == nil {
			break
		}
	}
	
	if cookieErr != nil {
		return "", fmt.Errorf("could not read tor auth cookie: %w", cookieErr)
	}
	cookieHex := hex.EncodeToString(cookieBytes)

	reader := bufio.NewReader(conn)

	_, err = fmt.Fprintf(conn, "AUTHENTICATE %s\r\n", cookieHex)
	if err != nil {
		return "", err
	}
	resp, err := reader.ReadString('\n')
	if err != nil || !strings.HasPrefix(resp, "250") {
		return "", fmt.Errorf("tor auth failed: %s", strings.TrimSpace(resp))
	}

	// 4. Create Ephemeral Hidden Service
	// We use NEW:BEST to get a v3 onion address. Port=80,127.0.0.1:<localPort>
	_, err = fmt.Fprintf(conn, "ADD_ONION NEW:BEST Port=80,127.0.0.1:%d\r\n", localPort)
	if err != nil {
		return "", err
	}

	var serviceID string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("error reading ADD_ONION response: %w", err)
		}
		line = strings.TrimSpace(line)
		
		if strings.HasPrefix(line, "250-ServiceID=") {
			serviceID = strings.TrimPrefix(line, "250-ServiceID=")
		}
		if line == "250 OK" {
			break
		}
		if strings.HasPrefix(line, "512") || strings.HasPrefix(line, "552") {
			return "", fmt.Errorf("ADD_ONION failed: %s", line)
		}
	}

	if serviceID == "" {
		return "", fmt.Errorf("did not receive ServiceID from Tor")
	}

	return serviceID + ".onion", nil
}

// DownloadFile downloads a file from an onion service using the provided token.
// It saves the file to the specified destPath.
func DownloadFile(onionAddr, token, destPath string) error {
	err := ensureTorRunning()
	if err != nil {
		return err
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer, err := proxy.SOCKS5("tcp", "127.0.0.1:9050", nil, proxy.Direct)
			if err != nil {
				return nil, err
			}
			return dialer.Dial(network, addr)
		},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Minute, // Large timeout for Tor downloads
	}

	req, err := http.NewRequest("GET", "http://"+onionAddr, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to save file: %w", err)
	}

	return nil
}
