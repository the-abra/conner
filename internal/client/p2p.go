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
	"sync"
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
	files     map[string]string // map[file_id]filepath
	mu        sync.Mutex
}

func GenerateToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func GetOutboundIP() net.IP {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return net.ParseIP("127.0.0.1")
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP
}

// StartP2PService initializes a single onion service (or direct service) for the entire session
func StartP2PService(useTor bool) (*P2PService, error) {
	if useTor && !isTorRunning() {
		return nil, fmt.Errorf("tor is not running")
	}

	token := GenerateToken()
	svc := &P2PService{
		Token: token,
		files: make(map[string]string),
	}
	
	// 1. Start local server
	listenAddr := "127.0.0.1:0"
	if !useTor {
		listenAddr = "0.0.0.0:0"
	}
	
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, err
	}
	svc.Port = listener.Addr().(*net.TCPAddr).Port
	svc.Listener = listener

	mux := http.NewServeMux()
	
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		// Simple Token Auth
		if r.URL.Query().Get("t") != svc.Token {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		
		fileID := r.URL.Query().Get("f")
		svc.mu.Lock()
		filePath, exists := svc.files[fileID]
		svc.mu.Unlock()

		if !exists {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}

		http.ServeFile(w, r, filePath)
	})

	go http.Serve(listener, mux)

	// 2. Register with Tor OR use public IP
	if useTor {
		onion, err := createOnion(svc.Port)
		if err != nil {
			listener.Close()
			return nil, err
		}
		svc.OnionAddr = onion
	} else {
		outboundIP := GetOutboundIP()
		svc.OnionAddr = fmt.Sprintf("%s:%d", outboundIP.String(), svc.Port)
	}

	return svc, nil
}

func (s *P2PService) ShareFile(filePath string) (string, error) {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return "", err
	}
	fileID := GenerateToken()[:8] // Short ID
	s.mu.Lock()
	s.files[fileID] = filePath
	s.mu.Unlock()
	return fileID, nil
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

func DownloadFile(address, fileName, token, destPath string) error {
	var client *http.Client

	if strings.HasSuffix(address, ".onion") {
		_ = ensureTorRunning()
		transport := &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				dialer, _ := proxy.SOCKS5("tcp", "127.0.0.1:9050", nil, proxy.Direct)
				return dialer.Dial(network, addr)
			},
		}
		client = &http.Client{Transport: transport, Timeout: 1 * time.Hour}
	} else {
		client = &http.Client{Timeout: 1 * time.Hour}
	}

	// URL format: http://address/download?f=filename&t=token
	url := fmt.Sprintf("http://%s/download?f=%s&t=%s", address, url.QueryEscape(fileName), token)
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
