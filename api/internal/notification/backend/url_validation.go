package backend

import (
	"context"
	"fmt"
	"net"
	"net/url"
)

// blockedIPNets contains IP ranges that are not allowed for external webhooks.
var blockedIPNets []*net.IPNet

func init() {
	cidrs := []string{
		"127.0.0.0/8",    // IPv4 loopback
		"10.0.0.0/8",     // RFC1918 private
		"172.16.0.0/12",  // RFC1918 private
		"192.168.0.0/16", // RFC1918 private
		"169.254.0.0/16", // IPv4 link-local
		"::1/128",        // IPv6 loopback
		"fe80::/10",      // IPv6 link-local
	}
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("invalid CIDR %q: %v", cidr, err))
		}
		blockedIPNets = append(blockedIPNets, ipNet)
	}
}

// validateExternalURL checks that rawURL is a safe external HTTP(S) URL.
// It blocks internal/private IP ranges to prevent SSRF attacks.
func validateExternalURL(ctx context.Context, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url scheme must be http or https, got %q", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("url must contain a hostname")
	}

	var resolver net.Resolver
	ips, err := resolver.LookupHost(ctx, host)
	if err != nil {
		return fmt.Errorf("cannot resolve hostname %q: %w", host, err)
	}

	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		for _, blocked := range blockedIPNets {
			if blocked.Contains(ip) {
				return fmt.Errorf("url host %q resolves to blocked address %s (%s)", host, ipStr, blocked)
			}
		}
	}

	return nil
}
