package container

import (
	"fmt"
	"strings"
)

// NetworkMode controls the agent's network access.
type NetworkMode string

const (
	// NetworkFiltered puts the agent on an internal network whose only way
	// out is a proxy that allows HTTPS to allowlisted hosts.
	NetworkFiltered NetworkMode = "filtered"
	// NetworkNone gives the agent no network at all.
	NetworkNone NetworkMode = "none"
	// NetworkOpen gives the agent unrestricted network access.
	NetworkOpen NetworkMode = "open"
)

const (
	proxyAlias = "proxy"
	proxyURL   = "http://" + proxyAlias + ":3128"
)

// NormalizeHost validates an allowlist entry, a host name or "*.domain",
// and returns it in lower case.
func NormalizeHost(h string) (string, error) {
	h = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(h), "."))
	name := strings.TrimPrefix(h, "*.")
	labels := strings.Split(name, ".")
	if len(labels) < 2 {
		return "", fmt.Errorf("invalid host %q: want a domain such as example.com or *.example.com", h)
	}
	for _, l := range labels {
		valid := l != "" && len(l) <= 63 && l[0] != '-' && l[len(l)-1] != '-'
		for _, r := range l {
			valid = valid && (r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-')
		}
		if !valid {
			return "", fmt.Errorf("invalid host %q", h)
		}
	}
	return h, nil
}
