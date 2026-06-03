package backend

import (
	"context"
	"errors"
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
		"0.0.0.0/8",      // IPv4 "this host" — 0.0.0.0 routes to localhost on Linux
		"127.0.0.0/8",    // IPv4 loopback
		"10.0.0.0/8",     // RFC1918 private
		"172.16.0.0/12",  // RFC1918 private
		"192.168.0.0/16", // RFC1918 private
		"169.254.0.0/16", // IPv4 link-local
		"::1/128",        // IPv6 loopback
		"fe80::/10",      // IPv6 link-local
		"fc00::/7",       // IPv6 unique local addresses (RFC 4193)
	}
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("invalid CIDR %q: %v", cidr, err))
		}
		blockedIPNets = append(blockedIPNets, ipNet)
	}
}

// isBlockedIP returns true if the IP is internal/private and must not be
// reached by an outbound webhook. It combines stdlib address classification
// (which also normalises IPv4-mapped IPv6 such as ::ffff:127.0.0.1) with the
// explicit CIDR list above as defence in depth.
func isBlockedIP(ip net.IP) bool {
	if ip.IsUnspecified() || ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() {
		return true
	}
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

// redactURLError strips the request URL from a *url.Error, keeping only
// scheme://host. Transport errors from http.Client.Do embed the full target
// URL, which for Slack/webhook channels can carry a secret token in the path,
// userinfo, or query. That error string is persisted to notification_log and
// shipped via slog, so redacting at the source keeps every downstream copy safe
// while leaving the failure diagnosable (host + underlying network error).
func redactURLError(err error) error {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return err
	}
	safe := ue.URL
	if u, perr := url.Parse(ue.URL); perr == nil && u.Host != "" {
		safe = u.Scheme + "://" + u.Host
	}
	return &url.Error{Op: ue.Op, URL: safe, Err: ue.Err}
}

// newSSRFSafeClient returns an http.Client that blocks connections to
// internal/private IP addresses at the transport level.
func newSSRFSafeClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: ssrfSafeTransport(),
	}
}
