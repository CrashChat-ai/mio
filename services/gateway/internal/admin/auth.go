// Package admin implements the control-plane connect-go server.
//
// Auth posture: this server is internal-only for v1. The auth.go middleware
// allowlists loopback (127.0.0.1 / ::1) and any operator-supplied CIDRs in
// MIO_ADMIN_ALLOW_CIDRS. Anything else returns 403. Non-loopback deploys
// MUST front this with a reverse-proxy that terminates TLS and enforces
// mTLS / SSO — the loopback check alone is not real authn/authz.
package admin

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
)

// AllowedNet is the parsed allowlist. Loopback is always included; the
// operator-supplied CIDRs come from MIO_ADMIN_ALLOW_CIDRS (comma-separated).
type AllowedNet struct {
	cidrs []*net.IPNet
}

// ParseAllowedCIDRs parses comma-separated CIDR notations into an AllowedNet.
// Returns an error if any entry is malformed; the entire admin boot fails so
// operators catch typos at startup rather than at 403 time.
//
// Loopback (127.0.0.1/8, ::1/128) is always included implicitly — explicit
// loopback entries are accepted but redundant.
func ParseAllowedCIDRs(spec string) (*AllowedNet, error) {
	out := &AllowedNet{}
	loopback4 := mustParseCIDR("127.0.0.0/8")
	loopback6 := mustParseCIDR("::1/128")
	out.cidrs = append(out.cidrs, loopback4, loopback6)
	for _, raw := range strings.Split(spec, ",") {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("admin: parse allow CIDR %q: %w", s, err)
		}
		out.cidrs = append(out.cidrs, n)
	}
	return out, nil
}

func mustParseCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic("admin: bad builtin CIDR: " + s)
	}
	return n
}

// Contains returns true if ip matches any allowed CIDR.
func (a *AllowedNet) Contains(ip net.IP) bool {
	for _, n := range a.cidrs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ErrForbidden is returned by the middleware when the caller's remote address
// is outside the allowed CIDRs. Surfaced to the caller as HTTP 403.
var ErrForbidden = errors.New("admin: caller not in allowed CIDRs")

// AuthMiddleware returns an http.Handler that rejects requests from outside
// the allowed CIDRs with 403. It must wrap BOTH the connect handler and the
// /oauth/callback route — they share the same auth posture.
//
// The remote address is parsed via net.SplitHostPort to handle the
// "127.0.0.1:54321" form. Any malformed RemoteAddr also yields 403 so a
// listener misconfiguration cannot accidentally expose the admin surface.
func AuthMiddleware(allowed *AllowedNet, logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				// Some test harnesses set RemoteAddr to a bare host. Try that.
				host = r.RemoteAddr
			}
			ip := net.ParseIP(host)
			if ip == nil || !allowed.Contains(ip) {
				logger.Warn("admin: caller rejected",
					"remote_addr", r.RemoteAddr,
					"path", r.URL.Path)
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
