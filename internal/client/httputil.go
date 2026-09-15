package client

import (
	"context"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/proxy"
)

func (c *Client) httpClient(timeout time.Duration) *http.Client {
	if !c.UseTor {
		return &http.Client{Timeout: timeout}
	}
	dialer, err := proxy.SOCKS5("tcp", c.SocksAddr, nil, proxy.Direct)
	if err != nil {
		return &http.Client{Timeout: timeout}
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			},
		},
	}
}
