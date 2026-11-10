package main

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestAllowed(t *testing.T) {
	patterns := []string{"api.anthropic.com", "*.github.com", ""}
	for host, want := range map[string]bool{
		"api.anthropic.com":      true,
		"evil-api.anthropic.com": false,
		"anthropic.com":          false,
		"api.github.com":         true,
		"a.b.github.com":         true,
		"github.com":             false,
		"notgithub.com":          false,
		"":                       false,
	} {
		if got := allowed(patterns, host); got != want {
			t.Errorf("allowed(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestIsPublic(t *testing.T) {
	for addr, want := range map[string]bool{
		"1.1.1.1":           true,
		"2606:4700::1111":   true,
		"127.0.0.1":         false,
		"10.0.0.1":          false,
		"172.17.0.2":        false,
		"192.168.5.2":       false,
		"169.254.169.254":   false,
		"100.100.100.100":   false,
		"::1":               false,
		"fd00::1":           false,
		"::ffff:10.0.0.1":   false,
		"0.0.0.0":           false,
		"224.0.0.1":         false,
		"::ffff:8.8.8.8":    true,
		"fe80::1":           false,
		"198.51.100.1":      true,
		"203.0.113.7":       true,
		"192.0.2.1":         true,
		"255.255.255.255":   false,
		"240.0.0.1":         true,
		"2001:db8::1":       true,
		"64:ff9b::808:808":  true,
		"100.63.255.255":    true,
		"100.128.0.0":       true,
		"172.32.0.1":        true,
		"172.15.255.255":    true,
		"11.0.0.1":          true,
		"9.255.255.255":     true,
		"192.169.0.1":       true,
		"192.167.255.255":   true,
		"126.255.255.255":   true,
		"128.0.0.1":         true,
		"169.253.255.255":   true,
		"169.255.0.0":       true,
		"fc00::1":           false,
		"fe7f:ffff::1":      true,
		"febf:ffff::1":      false,
		"ff02::1":           false,
		"::":                false,
		"::ffff:127.0.0.1":  false,
		"::ffff:100.64.0.1": false,
	} {
		if got := isPublic(netip.MustParseAddr(addr)); got != want {
			t.Errorf("isPublic(%s) = %v, want %v", addr, got, want)
		}
	}
}

// connect sends a CONNECT request through the proxy and returns the response
// status and the connection.
func connect(t *testing.T, proxyURL, target string) (int, net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.Dial("tcp", strings.TrimPrefix(proxyURL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	io.WriteString(conn, "CONNECT "+target+" HTTP/1.1\r\nHost: "+target+"\r\n\r\n")
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, conn, br
}

func TestProxy(t *testing.T) {
	// An echo server stands in for the upstream host.
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	go func() {
		for {
			c, err := upstream.Accept()
			if err != nil {
				return
			}
			go func() { io.Copy(c, c); c.Close() }()
		}
	}()

	p := &proxy{
		allow: []string{"allowed.example"},
		dial: func(ctx context.Context, host, port string) (net.Conn, error) {
			return net.Dial("tcp", upstream.Addr().String())
		},
	}
	srv := httptest.NewServer(p)
	defer srv.Close()

	status, conn, br := connect(t, srv.URL, "allowed.example:443")
	if status != http.StatusOK {
		t.Fatalf("allowed host: status %d", status)
	}
	io.WriteString(conn, "ping\n")
	if line, err := br.ReadString('\n'); err != nil || line != "ping\n" {
		t.Errorf("tunnel echoed %q, %v", line, err)
	}

	for _, target := range []string{"blocked.example:443", "allowed.example:22"} {
		if status, _, _ := connect(t, srv.URL, target); status != http.StatusForbidden {
			t.Errorf("CONNECT %s: status %d, want 403", target, status)
		}
	}

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("plain HTTP: status %d, want 403", resp.StatusCode)
	}
}
