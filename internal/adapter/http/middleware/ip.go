package middleware

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ClientIP honors X-Forwarded-For only when the immediate peer is trusted.
// When trusted, it walks the header right-to-left skipping trusted IPs,
// returning the first untrusted one. A client that pre-sets XFF cannot
// spoof: their entry is to the left of the trusted proxy's appended one.
func ClientIP(r *http.Request, trusted []netip.Prefix) netip.Addr {
	peer, err := peerAddr(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	if !isTrusted(peer, trusted) {
		return peer
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return peer
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		s := strings.TrimSpace(parts[i])
		if s == "" {
			continue
		}
		ip, err := netip.ParseAddr(s)
		if err != nil {
			continue
		}
		ip = ip.Unmap()
		if !isTrusted(ip, trusted) {
			return ip
		}
	}
	return peer
}

// ClientPrefix returns the /24 (IPv4) or /64 (IPv6) bucket, or "unknown".
func ClientPrefix(r *http.Request, trusted []netip.Prefix) string {
	return prefixOf(ClientIP(r, trusted))
}

func prefixOf(a netip.Addr) string {
	if !a.IsValid() {
		return "unknown"
	}
	bits := 64
	if a.Is4() {
		bits = 24
	}
	p, err := a.Prefix(bits)
	if err != nil {
		return "unknown"
	}
	return p.String()
}

func peerAddr(remoteAddr string) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, err
	}
	return a.Unmap(), nil
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	if !a.IsValid() {
		return false
	}
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
