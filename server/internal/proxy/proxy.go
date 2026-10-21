// Package proxy resolves the real client address and request scheme when the
// server sits behind Cloudflare. Trust is never based on a header alone: the
// immediate peer must be loopback (Tunnel) or inside an explicitly configured
// CIDR (orange-cloud origin).
package proxy

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

const (
	ModeNone   = "none"
	ModeTunnel = "cloudflare_tunnel"
	ModeProxy  = "cloudflare_proxy"
)

type Resolver struct {
	mode    string
	trusted []netip.Prefix
}

func New(mode string, cidrs []string) (*Resolver, error) {
	r := &Resolver{mode: mode}
	for _, raw := range cidrs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("parse trusted CIDR %q: %w", raw, err)
		}
		r.trusted = append(r.trusted, prefix)
	}
	switch mode {
	case ModeNone, ModeTunnel, ModeProxy:
		return r, nil
	default:
		return nil, fmt.Errorf("unknown proxy mode %q", mode)
	}
}

type Info struct {
	IP      netip.Addr
	Scheme  string
	Trusted bool
}

func (r *Resolver) Resolve(req *http.Request) Info {
	peer := peerAddr(req.RemoteAddr)
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}
	trusted := r.isTrusted(peer)
	if !trusted {
		return Info{IP: peer, Scheme: scheme}
	}

	ip := peer
	if raw := strings.TrimSpace(req.Header.Get("CF-Connecting-IP")); raw != "" {
		if parsed, err := netip.ParseAddr(raw); err == nil {
			ip = parsed.Unmap()
		}
	}
	if forwarded := forwardedScheme(req); forwarded != "" {
		scheme = forwarded
	}
	return Info{IP: ip, Scheme: scheme, Trusted: true}
}

func (r *Resolver) isTrusted(peer netip.Addr) bool {
	if !peer.IsValid() {
		return false
	}
	switch r.mode {
	case ModeTunnel:
		return peer.IsLoopback()
	case ModeProxy:
		for _, prefix := range r.trusted {
			if prefix.Contains(peer) {
				return true
			}
		}
	}
	return false
}

func forwardedScheme(req *http.Request) string {
	var visitor struct {
		Scheme string `json:"scheme"`
	}
	if raw := strings.TrimSpace(req.Header.Get("CF-Visitor")); raw != "" {
		if json.Unmarshal([]byte(raw), &visitor) == nil {
			switch strings.ToLower(visitor.Scheme) {
			case "http", "https":
				return strings.ToLower(visitor.Scheme)
			}
		}
	}
	raw := strings.TrimSpace(req.Header.Get("X-Forwarded-Proto"))
	if raw == "" {
		return ""
	}
	first := strings.ToLower(strings.TrimSpace(strings.SplitN(raw, ",", 2)[0]))
	switch first {
	case "http", "https":
		return first
	default:
		return ""
	}
}

func peerAddr(remote string) netip.Addr {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}
