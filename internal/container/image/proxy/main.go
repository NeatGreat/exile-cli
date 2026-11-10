// Command exile-proxy is the agent container's only way out: an HTTPS proxy
// that tunnels connections to allowlisted hosts on port 443 and rejects
// everything else. It runs in a sidecar container attached to both the
// agent's internal network and the outside world.
//
// The allowlist is read from EXILE_ALLOW, a comma-separated list of host
// names; "*.example.com" matches all subdomains of example.com.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"time"
)

func main() {
	listen := flag.String("listen", ":3128", "address to listen on")
	flag.Parse()
	log.SetFlags(0)

	p := &proxy{allow: strings.Split(os.Getenv("EXILE_ALLOW"), ","), dial: dialPublic}
	srv := &http.Server{Addr: *listen, Handler: p, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

type proxy struct {
	allow []string
	dial  func(ctx context.Context, host, port string) (net.Conn, error)
}

func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodConnect {
		deny(w, r.URL.Hostname(), "only HTTPS is allowed")
		return
	}
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil {
		http.Error(w, "exile: bad CONNECT target", http.StatusBadRequest)
		return
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	switch {
	case port != "443":
		deny(w, host, "only port 443 is allowed")
		return
	case !allowed(p.allow, host):
		deny(w, host, "host is not allowed")
		return
	}

	upstream, err := p.dial(r.Context(), host, port)
	if err != nil {
		log.Printf("error %s %v", host, err)
		http.Error(w, "exile: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer upstream.Close()
	client, buf, err := http.NewResponseController(w).Hijack()
	if err != nil {
		http.Error(w, "exile: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer client.Close()
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	log.Printf("allow %s", host)

	done := make(chan struct{})
	go func() {
		io.Copy(upstream, buf.Reader)
		if c, ok := upstream.(interface{ CloseWrite() error }); ok {
			c.CloseWrite()
		}
		close(done)
	}()
	io.Copy(client, upstream)
	client.Close()
	<-done
}

// deny rejects a request. The "deny" log lines are collected by exile and
// shown to the user when the session ends.
func deny(w http.ResponseWriter, host, reason string) {
	log.Printf("deny %s", host)
	msg := fmt.Sprintf("exile: connection to %s blocked (%s); allow it on the host with: exile allow %s", host, reason, host)
	http.Error(w, msg, http.StatusForbidden)
}

func allowed(patterns []string, host string) bool {
	for _, p := range patterns {
		if suffix, ok := strings.CutPrefix(p, "*."); ok {
			if strings.HasSuffix(host, "."+suffix) {
				return true
			}
		} else if p != "" && host == p {
			return true
		}
	}
	return false
}

// cgnat is the carrier-grade NAT range, also used by VPNs such as Tailscale.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// isPublic reports whether a is a public unicast address. Connections to
// the host, the local network or other containers are never tunneled, even
// if an allowed name resolves to them.
func isPublic(a netip.Addr) bool {
	a = a.Unmap()
	return a.IsGlobalUnicast() && !a.IsPrivate() && !cgnat.Contains(a)
}

func dialPublic(ctx context.Context, host, port string) (net.Conn, error) {
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	d := net.Dialer{Timeout: 10 * time.Second}
	err = fmt.Errorf("%s resolves to private addresses only", host)
	for _, a := range addrs {
		if !isPublic(a) {
			continue
		}
		conn, dialErr := d.DialContext(ctx, "tcp", net.JoinHostPort(a.Unmap().String(), port))
		if dialErr == nil {
			return conn, nil
		}
		err = dialErr
	}
	return nil, err
}
