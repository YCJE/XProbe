package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// dashboardWS pushes a full snapshot every two seconds. It is intentionally a
// snapshot rather than an event stream: one admin, a few dozen nodes.
func (s *Server) dashboardWS(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" && !s.dashboardOriginAllowed(r, origin) {
		writeError(w, http.StatusForbidden, "origin refused")
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		s.logger.Warn("dashboard websocket accept", "error", err)
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1024)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	readErr := make(chan error, 1)
	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				readErr <- err
				return
			}
		}
	}()

	send := func() error {
		servers, err := s.serverSummaries(ctx)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(map[string]any{"servers": servers})
		if err != nil {
			return err
		}
		writeCtx, writeCancel := context.WithTimeout(ctx, 5*time.Second)
		defer writeCancel()
		return conn.Write(writeCtx, websocket.MessageText, payload)
	}
	if err := send(); err != nil {
		return
	}
	session := sessionFrom(r.Context())
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	authTicker := time.NewTicker(30 * time.Second)
	defer authTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-readErr:
			return
		case <-ticker.C:
			if err := send(); err != nil {
				return
			}
		case <-authTicker.C:
			if session == nil {
				_ = conn.Close(websocket.StatusPolicyViolation, "session unavailable")
				return
			}
			current, err := s.sessions.GetByTokenHash(ctx, session.TokenHash)
			now := s.now()
			if err != nil || current.Revoked || !now.Before(current.ExpiresAt) || !now.Before(current.IdleExpiresAt) {
				_ = conn.Close(websocket.StatusPolicyViolation, "session expired")
				return
			}
			// Watching the dashboard is activity; keep the idle window open,
			// but never past the absolute expiry.
			idle := now.Add(s.cfg.Auth.SessionIdle.Duration)
			if idle.After(current.ExpiresAt) {
				idle = current.ExpiresAt
			}
			if err := s.sessions.Touch(ctx, current.ID, idle, now); err != nil {
				s.logger.Warn("touch dashboard session", "error", err)
			}
		}
	}
}

func (s *Server) dashboardOriginAllowed(r *http.Request, origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	secure := s.secureRequest(r)
	if strings.EqualFold(u.Host, r.Host) {
		return u.Scheme == "https" || !secure
	}
	for _, raw := range []string{s.cfg.PublicURL, s.cfg.AdminURL} {
		if raw == "" {
			continue
		}
		allowed, err := url.Parse(raw)
		if err == nil && strings.EqualFold(u.Host, allowed.Host) &&
			strings.EqualFold(u.Scheme, allowed.Scheme) {
			return true
		}
	}
	// Vite development server on loopback. Only when the hub itself is a
	// loopback development instance; a public hub must not accept localhost
	// origins merely because the browser is local.
	if !secure && isLoopbackName(u.Hostname()) {
		public, err := url.Parse(s.cfg.PublicURL)
		if err == nil && isLoopbackName(public.Hostname()) {
			return true
		}
	}
	return false
}

func isLoopbackName(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}
