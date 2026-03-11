package backend

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
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

// isBlockedIP returns true if the IP falls within a blocked CIDR range.
func isBlockedIP(ip net.IP) bool {
	for _, blocked := range blockedIPNets {
		if blocked.Contains(ip) {
			return true
		}
	}
	return false
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
		return fmt.Errorf("cannot resolve hostname %q", host)
	}

	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if isBlockedIP(ip) {
			return fmt.Errorf("url host %q resolves to a blocked internal address", host)
		}
	}

	return nil
}

// ssrfSafeTransport returns an http.Transport with a custom dialer that blocks
// connections to internal/private IP ranges at connect time. This prevents DNS
// rebinding attacks where validation resolves to a public IP but the actual
// connection resolves to a private IP.
func ssrfSafeTransport() *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("invalid address %q: %w", addr, err)
			}

			var resolver net.Resolver
			ips, err := resolver.LookupHost(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("cannot resolve %q: %w", host, err)
			}

			for _, ipStr := range ips {
				ip := net.ParseIP(ipStr)
				if ip != nil && isBlockedIP(ip) {
					return nil, fmt.Errorf("connection to %q blocked: resolves to internal address", host)
				}
			}

			dialer := &net.Dialer{Timeout: 10 * time.Second}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0], port))
		},
	}
}

// newSSRFSafeClient returns an http.Client that blocks connections to
// internal/private IP addresses at the transport level.
func newSSRFSafeClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: ssrfSafeTransport(),
	}
}
