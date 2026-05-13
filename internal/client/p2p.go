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
	"net/url"
	"os"
	"strings"
	"time"

	"conner/internal/config"
	"golang.org/x/net/proxy"
)

// P2PService handles the persistent hidden service for file sharing
type P2PService struct {
	OnionAddr string
	Token     string
	Port      int
	Listener  net.Listener
}

func GenerateToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// StartP2PService initializes a single onion service for the entire session
func StartP2PService(uploadDir string) (*P2PService, error) {
	token := GenerateToken()
	
	// 1. Start local server serving the upload directory
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	fileServer := http.FileServer(http.Dir(uploadDir))
	
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Simple Token Auth
		if r.URL.Query().Get("t") != token {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		// Security: Prevent directory listing or path traversal
		if r.URL.Path == "/" || strings.Contains(r.URL.Path, "..") {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	go http.Serve(listener, mux)

	// 2. Register with Tor
	onion, err := createOnion(port)
	if err != nil {
		listener.Close()
		return nil, err
	}

	return &P2PService{
		OnionAddr: onion,
		Token:     token,
		Port:      port,
		Listener:  listener,
	}, nil
}

func createOnion(localPort int) (string, error) {
	conn, err := net.DialTimeout("tcp", config.TorControlAddr, 5*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	cookieBytes, _ := os.ReadFile(config.TorCookiePath)
	reader := bufio.NewReader(conn)
	fmt.Fprintf(conn, "AUTHENTICATE %s\r\n", hex.EncodeToString(cookieBytes))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "250") {
		return "", fmt.Errorf("tor auth failed")
	}

	fmt.Fprintf(conn, "ADD_ONION NEW:BEST Port=80,127.0.0.1:%d\r\n", localPort)
	
	var serviceID string
	for {
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "250-ServiceID=") {
			serviceID = strings.TrimPrefix(line, "250-ServiceID=")
		}
		if line == "250 OK" {
			break
		}
	}
	return serviceID + ".onion", nil
}

func DownloadFile(onionAddr, fileName, token, destPath string) error {
	_ = ensureTorRunning()

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer, _ := proxy.SOCKS5("tcp", "127.0.0.1:9050", nil, proxy.Direct)
			return dialer.Dial(network, addr)
		},
	}
	client := &http.Client{Transport: transport, Timeout: 1 * time.Hour}

	// URL format: http://onion/filename?t=token
	url := fmt.Sprintf("http://%s/%s?t=%s", onionAddr, url.PathEscape(fileName), token)
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}
