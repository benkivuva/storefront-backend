package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func mustPrefix(t *testing.T, s string) netip.Prefix {
	t.Helper()
	p, err := netip.ParsePrefix(s)
	if err != nil {
		t.Fatalf("ParsePrefix(%q): %v", s, err)
	}
	return p
}

func reqWith(t *testing.T, remoteAddr, xff string) *http.Request {
	t.Helper()
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = remoteAddr
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	return r
}

func TestClientIP_UntrustedPeerUsesRemoteAddr(t *testing.T) {
	r := reqWith(t, "1.2.3.4:5678", "")
	if got := ClientIP(r, nil); got.String() != "1.2.3.4" {
		t.Fatalf("got %v", got)
	}
}

func TestClientIP_UntrustedPeerIgnoresXFF(t *testing.T) {
	r := reqWith(t, "1.2.3.4:5678", "9.9.9.9")
	if got := ClientIP(r, nil); got.String() != "1.2.3.4" {
		t.Fatalf("got %v, want 1.2.3.4 (XFF ignored)", got)
	}
}

func TestClientIP_TrustedPeerHonorsXFF(t *testing.T) {
	trusted := []netip.Prefix{mustPrefix(t, "10.0.0.0/8")}
	r := reqWith(t, "10.0.0.5:5678", "9.9.9.9")
	if got := ClientIP(r, trusted); got.String() != "9.9.9.9" {
		t.Fatalf("got %v", got)
	}
}

func TestClientIP_TrustedPeerSkipsSpoofPrefix(t *testing.T) {
	trusted := []netip.Prefix{mustPrefix(t, "10.0.0.0/8")}
	r := reqWith(t, "10.0.0.5:5678", "6.6.6.6, 9.9.9.9")
	if got := ClientIP(r, trusted); got.String() != "9.9.9.9" {
		t.Fatalf("got %v, want 9.9.9.9 (rightmost untrusted)", got)
	}
}

func TestClientIP_TrustedPeerSkipsTrustedSuffix(t *testing.T) {
	trusted := []netip.Prefix{
		mustPrefix(t, "10.0.0.0/8"),
		mustPrefix(t, "10.1.0.0/16"),
	}
	r := reqWith(t, "10.1.0.5:5678", "9.9.9.9, 10.0.0.7")
	if got := ClientIP(r, trusted); got.String() != "9.9.9.9" {
		t.Fatalf("got %v", got)
	}
}

func TestClientIP_TrustedPeerNoXFF_UsesPeer(t *testing.T) {
	trusted := []netip.Prefix{mustPrefix(t, "10.0.0.0/8")}
	r := reqWith(t, "10.0.0.5:5678", "")
	if got := ClientIP(r, trusted); got.String() != "10.0.0.5" {
		t.Fatalf("got %v", got)
	}
}

func TestClientIP_IPv4MappedUnmapped(t *testing.T) {
	r := reqWith(t, "[::ffff:1.2.3.4]:5678", "")
	if got := ClientIP(r, nil); got.String() != "1.2.3.4" {
		t.Fatalf("got %v, want 1.2.3.4", got)
	}
}

func TestClientIP_BadXFFSkipped(t *testing.T) {
	trusted := []netip.Prefix{mustPrefix(t, "10.0.0.0/8")}
	r := reqWith(t, "10.0.0.5:5678", "not-an-ip, 9.9.9.9")
	if got := ClientIP(r, trusted); got.String() != "9.9.9.9" {
		t.Fatalf("got %v", got)
	}
}

func TestClientIP_InvalidRemoteAddr(t *testing.T) {
	r := reqWith(t, "", "")
	if got := ClientIP(r, nil); got.IsValid() {
		t.Fatalf("got %v, want invalid", got)
	}
}

func TestClientPrefix_IPv4(t *testing.T) {
	r := reqWith(t, "192.168.1.42:5678", "")
	if got := ClientPrefix(r, nil); got != "192.168.1.0/24" {
		t.Fatalf("got %q", got)
	}
}

func TestClientPrefix_IPv6(t *testing.T) {
	r := reqWith(t, "[2001:db8::1]:5678", "")
	if got := ClientPrefix(r, nil); got != "2001:db8::/64" {
		t.Fatalf("got %q", got)
	}
}

func TestClientPrefix_Invalid(t *testing.T) {
	r := reqWith(t, "", "")
	if got := ClientPrefix(r, nil); got != "unknown" {
		t.Fatalf("got %q, want unknown", got)
	}
}
