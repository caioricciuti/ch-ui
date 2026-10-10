package middleware

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
)

const clientKey contextKey = "client"

type clientInfo struct {
	ip              string
	viaTrustedProxy bool
}

// RealIP resolves the client address once per request for ClientIP and
// FromTrustedProxy. Forwarding headers are believed only when the TCP peer is
// inside one of the trusted prefixes. X-Forwarded-For is then walked from the
// right, skipping trusted hops, so a client cannot prepend a fake address and
// dodge per-IP limits. Requests from any other peer use the peer address and
// their forwarding headers are ignored (logged once per peer, so a forgotten
// trusted_proxies setting shows up in the log).
func RealIP(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			info := resolveClient(r, trusted)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientKey, info)))
		})
	}
}

// ClientIP returns the client address resolved by RealIP, or the TCP peer
// address when the middleware did not run (tests, standalone handlers).
func ClientIP(r *http.Request) string {
	if info, ok := r.Context().Value(clientKey).(clientInfo); ok {
		return info.ip
	}
	return peerIP(r.RemoteAddr)
}

// FromTrustedProxy reports whether the request arrived through a trusted
// reverse proxy, i.e. whether its X-Forwarded-* headers may be believed.
func FromTrustedProxy(r *http.Request) bool {
	info, ok := r.Context().Value(clientKey).(clientInfo)
	return ok && info.viaTrustedProxy
}

func resolveClient(r *http.Request, trusted []netip.Prefix) clientInfo {
	peer := peerIP(r.RemoteAddr)
	peerAddr, ok := parseAddr(peer)
	if !ok || !inPrefixes(peerAddr, trusted) {
		if r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("X-Real-IP") != "" {
			warnUntrustedForwarding(peer)
		}
		return clientInfo{ip: peer}
	}

	var chain []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		for _, part := range strings.Split(v, ",") {
			if p := strings.TrimSpace(part); p != "" {
				chain = append(chain, p)
			}
		}
	}

	// Right to left: the rightmost hops were appended by our proxies, the
	// first address that is not a trusted proxy is the client.
	var lastTrusted netip.Addr
	for i := len(chain) - 1; i >= 0; i-- {
		hop, ok := parseAddr(chain[i])
		if !ok {
			// A malformed hop: nothing to its left can be believed. Fall back
			// to the nearest trusted hop on its right, else the peer.
			if lastTrusted.IsValid() {
				return clientInfo{ip: lastTrusted.String(), viaTrustedProxy: true}
			}
			return clientInfo{ip: peer, viaTrustedProxy: true}
		}
		if !inPrefixes(hop, trusted) {
			return clientInfo{ip: hop.String(), viaTrustedProxy: true}
		}
		lastTrusted = hop
	}
	if lastTrusted.IsValid() {
		// Every hop is a trusted address: the client itself sits on a trusted
		// network, and the leftmost entry is it.
		return clientInfo{ip: lastTrusted.String(), viaTrustedProxy: true}
	}

	if xri, ok := parseAddr(strings.TrimSpace(r.Header.Get("X-Real-IP"))); ok {
		return clientInfo{ip: xri.String(), viaTrustedProxy: true}
	}
	return clientInfo{ip: peer, viaTrustedProxy: true}
}

// peerIP strips the port from a RemoteAddr; a value without a port is
// returned unchanged.
func peerIP(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

// parseAddr accepts "ip", "[ip6]", "ip:port" and "[ip6]:port", drops any
// zone and unmaps IPv4-mapped IPv6 so prefix matching works on the real
// address.
func parseAddr(s string) (netip.Addr, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return netip.Addr{}, false
	}
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().Unmap().WithZone(""), true
	}
	s = strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return a.Unmap().WithZone(""), true
}

func inPrefixes(a netip.Addr, prefixes []netip.Prefix) bool {
	for _, p := range prefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

var (
	untrustedWarned      sync.Map
	untrustedWarnedCount atomic.Int64
)

func warnUntrustedForwarding(peer string) {
	if untrustedWarnedCount.Load() >= 1024 {
		return
	}
	if _, seen := untrustedWarned.LoadOrStore(peer, struct{}{}); seen {
		return
	}
	untrustedWarnedCount.Add(1)
	slog.Warn("Ignoring X-Forwarded-For from an address that is not a trusted proxy; if it is your reverse proxy, add it to trusted_proxies", "peer", peer)
}
