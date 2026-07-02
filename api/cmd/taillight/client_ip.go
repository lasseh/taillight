package main

import (
	"net/http"
	"net/netip"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/lasseh/taillight/internal/config"
)

// clientIPMiddleware selects how the real client IP is resolved into the
// request context (read downstream via middleware.GetClientIP). When
// cfg.RealIPHeader is set, the proxy-supplied header is trusted; otherwise the
// TCP peer is used so that a directly-exposed deployment cannot be spoofed via
// forwarded headers. Replaces the deprecated, spoofable middleware.RealIP.
//
// When cfg.TrustedProxies is also set, the header is honored only for requests
// whose raw TCP peer falls inside one of the listed CIDRs; any other peer
// resolves to its own address, so a client that reaches the API without going
// through the proxy cannot forge its IP. Empty keeps the previous behavior
// (header trusted from any peer).
func clientIPMiddleware(cfg config.Config) func(http.Handler) http.Handler {
	if cfg.RealIPHeader == "" {
		return middleware.ClientIPFromRemoteAddr
	}
	fromHeader := middleware.ClientIPFromHeader(cfg.RealIPHeader)
	if len(cfg.TrustedProxies) == 0 {
		return fromHeader
	}
	return func(next http.Handler) http.Handler {
		trusted := fromHeader(next)
		untrusted := middleware.ClientIPFromRemoteAddr(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if peerIsTrustedProxy(r.RemoteAddr, cfg.TrustedProxies) {
				trusted.ServeHTTP(w, r)
				return
			}
			untrusted.ServeHTTP(w, r)
		})
	}
}

// peerIsTrustedProxy reports whether the raw TCP peer (a host:port RemoteAddr)
// falls inside one of the trusted proxy CIDRs. An unparseable peer is
// untrusted, failing closed to peer-based resolution.
func peerIsTrustedProxy(remoteAddr string, proxies []netip.Prefix) bool {
	ap, err := netip.ParseAddrPort(remoteAddr)
	if err != nil {
		return false
	}
	// Fold v4-mapped v6 to plain v4 and drop any zone so a dual-stack peer
	// (::ffff:172.18.0.5) matches an IPv4 CIDR (172.18.0.0/16).
	addr := ap.Addr().Unmap().WithZone("")
	for _, p := range proxies {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
