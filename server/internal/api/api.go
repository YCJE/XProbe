// Package api wires HTTP, authentication, agent enrollment and the read-only
// agent WebSocket together.
package api

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"probe/server/internal/alert"
	"probe/server/internal/config"
	"probe/server/internal/model"
	"probe/server/internal/monitor"
	"probe/server/internal/notify"
	"probe/server/internal/proxy"
	"probe/server/internal/ratelimit"
	"probe/server/internal/repository"
	"probe/server/internal/security"
	"probe/server/internal/webui"
)

const Version = "0.3.0"

const (
	maxJSONBody   = 16 * 1024
	setupTokenTTL = 30 * time.Minute
)

type Server struct {
	cfg         *config.Config
	db          *sql.DB
	logger      *slog.Logger
	agents      *repository.AgentRepo
	admins      *repository.AdminRepo
	sessions    *repository.SessionRepo
	codes       *repository.RegisterCodeRepo
	settings    *repository.SettingsRepo
	targets     *repository.PingTargetRepo
	history     *repository.HistoryRepo
	auditRepo   *repository.AuditRepo
	alerts      *repository.AlertRepo
	notifyRepo  *repository.NotifyRepo
	notifier    *notify.Sender
	alertEngine *alert.Engine
	hub         *monitor.Hub
	recorder    *monitor.MetricRecorder
	resolver    *proxy.Resolver

	login    *ratelimit.LoginThrottle
	register *ratelimit.Window
	setup    *ratelimit.Window

	setupMu        sync.Mutex
	setupTokenPath string
	dummyHash      string
	passwordGate   chan struct{}
	secretBox      *security.SecretBox
	now            func() time.Time
}

func New(cfg *config.Config, db *sql.DB, logger *slog.Logger) (*Server, error) {
	resolver, err := proxy.New(cfg.Proxy.Mode, cfg.Proxy.TrustedCIDRs)
	if err != nil {
		return nil, err
	}
	dummyHash, err := security.HashPassword("dummy-password-not-used")
	if err != nil {
		return nil, fmt.Errorf("dummy password hash: %w", err)
	}
	secretBox, err := security.LoadOrCreateKey(filepath.Join(cfg.DataDir, "totp.key"))
	if err != nil {
		return nil, err
	}
	history := repository.NewHistoryRepo(db)
	agents := repository.NewAgentRepo(db)
	hub := monitor.NewHub()
	alerts := repository.NewAlertRepo(db)
	notifyRepo := repository.NewNotifyRepo(db)
	sender := notify.New(secretBox)
	return &Server{
		cfg:            cfg,
		db:             db,
		logger:         logger,
		agents:         agents,
		admins:         repository.NewAdminRepo(db),
		sessions:       repository.NewSessionRepo(db),
		codes:          repository.NewRegisterCodeRepo(db),
		settings:       repository.NewSettingsRepo(db),
		targets:        repository.NewPingTargetRepo(db),
		history:        history,
		auditRepo:      repository.NewAuditRepo(db),
		alerts:         alerts,
		notifyRepo:     notifyRepo,
		notifier:       sender,
		alertEngine:    alert.NewEngine(alerts, agents, notifyRepo, hub, sender, logger),
		hub:            hub,
		recorder:       monitor.NewMetricRecorder(history, time.Duration(cfg.Monitor.AggregationSecond)*time.Second),
		resolver:       resolver,
		login:          ratelimit.NewLoginThrottle(cfg.Auth.LoginRateLimit, 10, 15*time.Minute),
		register:       ratelimit.NewWindow(5, time.Minute),
		setup:          ratelimit.NewWindow(5, time.Minute),
		setupTokenPath: filepath.Join(cfg.DataDir, "setup_token"),
		dummyHash:      dummyHash,
		secretBox:      secretBox,
		passwordGate:   make(chan struct{}, 1),
		now:            time.Now,
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /api/v1/setup/status", s.setupStatus)
	mux.HandleFunc("POST /api/v1/setup", s.setupAdmin)
	mux.HandleFunc("POST /api/v1/auth/login", s.loginHandler)
	mux.HandleFunc("POST /api/v1/auth/logout", s.requireAdmin(s.logoutHandler))
	mux.HandleFunc("GET /api/v1/auth/me", s.requireAdmin(s.meHandler))
	mux.HandleFunc("GET /api/v1/servers", s.requireAdmin(s.listServers))
	mux.HandleFunc("GET /api/v1/servers/{id}", s.requireAdmin(s.serverDetail))
	mux.HandleFunc("PUT /api/v1/servers/{id}/meta", s.requireAdmin(s.updateServerMeta))
	mux.HandleFunc("GET /api/v1/servers/{id}/history", s.requireAdmin(s.serverHistory))
	mux.HandleFunc("GET /ws/dashboard", s.requireAdmin(s.dashboardWS))
	mux.HandleFunc("POST /api/v1/agents/register-codes", s.requireAdmin(s.createRegisterCode))
	mux.HandleFunc("GET /api/v1/agents/register-codes", s.requireAdmin(s.listRegisterCodes))
	mux.HandleFunc("DELETE /api/v1/agents/register-codes/{id}", s.requireAdmin(s.deleteRegisterCode))
	mux.HandleFunc("GET /api/v1/alerts", s.requireAdmin(s.listAlerts))
	mux.HandleFunc("GET /api/v1/alerts/history", s.requireAdmin(s.listAlertHistory))
	mux.HandleFunc("POST /api/v1/alerts", s.requireAdmin(s.createAlert))
	mux.HandleFunc("PUT /api/v1/alerts/{id}", s.requireAdmin(s.updateAlert))
	mux.HandleFunc("DELETE /api/v1/alerts/{id}", s.requireAdmin(s.deleteAlert))
	mux.HandleFunc("GET /api/v1/notify/channels", s.requireAdmin(s.listChannels))
	mux.HandleFunc("POST /api/v1/notify/channels", s.requireAdmin(s.createChannel))
	mux.HandleFunc("DELETE /api/v1/notify/channels/{id}", s.requireAdmin(s.deleteChannel))
	mux.HandleFunc("POST /api/v1/notify/channels/{id}/test", s.requireAdmin(s.testChannel))
	mux.HandleFunc("GET /api/v1/config/settings", s.requireAdmin(s.getSettings))
	mux.HandleFunc("PUT /api/v1/config/settings", s.requireAdmin(s.saveSettings))
	mux.HandleFunc("GET /api/v1/config/ping-targets", s.requireAdmin(s.listPingTargets))
	mux.HandleFunc("POST /api/v1/config/ping-targets", s.requireAdmin(s.createPingTarget))
	mux.HandleFunc("PUT /api/v1/config/ping-targets/{id}", s.requireAdmin(s.updatePingTarget))
	mux.HandleFunc("DELETE /api/v1/config/ping-targets/{id}", s.requireAdmin(s.deletePingTarget))
	mux.HandleFunc("GET /api/v1/sessions", s.requireAdmin(s.listSessions))
	mux.HandleFunc("DELETE /api/v1/sessions/{id}", s.requireAdmin(s.deleteSession))
	mux.HandleFunc("GET /api/v1/audit", s.requireAdmin(s.listAudit))
	mux.HandleFunc("POST /api/v1/auth/totp/setup", s.requireAdmin(s.totpSetup))
	mux.HandleFunc("POST /api/v1/auth/totp/enable", s.requireAdmin(s.totpEnable))
	mux.HandleFunc("POST /api/v1/auth/totp/disable", s.requireAdmin(s.totpDisable))
	mux.HandleFunc("GET /api/v1/public/{shareId}", s.publicDashboard)
	mux.HandleFunc("POST /api/v1/agent/register", s.registerAgent)
	mux.HandleFunc("GET /api/v1/agent/config", s.agentConfig)
	mux.HandleFunc("GET /api/v1/agent/report", s.agentWS)
	mux.HandleFunc("GET /install-agent.sh", s.installAgentScript)
	mux.HandleFunc("GET /download/agent/{arch}", s.downloadAgent)
	mux.HandleFunc("GET /download/agent/{arch}/{file}", s.downloadAgent)
	mux.Handle("GET /", webui.Handler())
	return s.withMeta(s.securityHeaders(s.sameOriginWrites(mux)))
}

// EnsureSetupToken creates or returns the one-time bootstrap token when no
// admin exists, and removes stale files once setup is complete.
func (s *Server) EnsureSetupToken(ctx context.Context) (string, error) {
	n, err := s.admins.Count(ctx)
	if err != nil {
		return "", err
	}
	if n > 0 {
		_ = os.Remove(s.setupTokenPath)
		return "", nil
	}
	info, statErr := os.Stat(s.setupTokenPath)
	if statErr == nil && s.now().Sub(info.ModTime()) <= setupTokenTTL {
		if b, err := os.ReadFile(s.setupTokenPath); err == nil {
			if token := strings.TrimSpace(string(b)); token != "" {
				return token, nil
			}
		}
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("stat setup token: %w", statErr)
	}
	token, err := security.GenerateToken()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(s.setupTokenPath), 0o700); err != nil {
		return "", fmt.Errorf("create data directory: %w", err)
	}
	if err := os.WriteFile(s.setupTokenPath, []byte(token+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write setup token: %w", err)
	}
	return token, nil
}

func (s *Server) setupAdmin(w http.ResponseWriter, r *http.Request) {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()

	n, err := s.admins.Count(r.Context())
	if err != nil {
		s.internal(w, "check setup state", err)
		return
	}
	if n > 0 {
		http.NotFound(w, r)
		return
	}
	if !s.setup.Allow(ipKey(r.Context())) {
		writeError(w, http.StatusTooManyRequests, "too many setup attempts")
		return
	}
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	info, err := os.Stat(s.setupTokenPath)
	if err != nil {
		s.logger.Error("setup token missing", "error", err)
		writeError(w, http.StatusInternalServerError, "setup is unavailable")
		return
	}
	if s.now().Sub(info.ModTime()) > setupTokenTTL {
		_ = os.Remove(s.setupTokenPath)
		writeError(w, http.StatusUnauthorized, "setup token 已过期，请重启服务生成新 token")
		return
	}
	expected, err := os.ReadFile(s.setupTokenPath)
	if err != nil {
		s.logger.Error("setup token missing", "error", err)
		writeError(w, http.StatusInternalServerError, "setup is unavailable")
		return
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(string(expected))), []byte(strings.TrimSpace(req.Token))) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid setup token")
		return
	}
	if err := security.ValidatePassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := s.hashPassword(r.Context(), req.Password)
	if err != nil {
		s.internal(w, "hash password", err)
		return
	}
	_, err = s.admins.Create(r.Context(), &model.Admin{Username: "admin", PasswordHash: hash, CreatedAt: s.now()})
	if err != nil {
		s.internal(w, "create admin", err)
		return
	}
	if err := os.Remove(s.setupTokenPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.logger.Warn("could not remove setup token", "error", err)
	}
	if err := s.auditRepo.Write(r.Context(), s.now(), "setup", "setup.admin", "admin", ""); err != nil {
		s.logger.Warn("audit setup", "error", err)
	}
	s.logger.Info("admin account created")
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": Version})
}

func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	n, err := s.admins.Count(r.Context())
	if err != nil {
		s.internal(w, "setup status", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"needs_setup": n == 0})
}

func (s *Server) withMeta(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := s.resolver.Resolve(r)
		ctx := context.WithValue(r.Context(), metaKey{}, info)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/") || r.URL.Path == "/healthz" {
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		} else {
			connect := []string{"'self'"}
			seen := map[string]bool{"'self'": true}
			for _, raw := range []string{s.cfg.PublicURL, s.cfg.AdminURL} {
				if raw == "" {
					continue
				}
				u, err := url.Parse(raw)
				if err != nil || u.Host == "" {
					continue
				}
				scheme := "ws"
				if u.Scheme == "https" {
					scheme = "wss"
				}
				source := scheme + "://" + u.Host
				if !seen[source] {
					seen[source] = true
					connect = append(connect, source)
				}
			}
			ownScheme := "ws"
			if s.secureRequest(r) {
				ownScheme = "wss"
			}
			// Only add a syntactically valid Host; a malformed header must not
			// be spliced into the CSP.
			if validHostHeader(r.Host) {
				u, err := url.Parse("http://" + r.Host)
				if err != nil || u.Host != r.Host {
					u = nil
				}
				if u != nil {
					own := ownScheme + "://" + r.Host
					if !seen[own] {
						seen[own] = true
						connect = append(connect, own)
					}
				}
			}
			csp := "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
				"img-src 'self' data:; connect-src " + strings.Join(connect, " ") + "; " +
				"font-src 'self' data:; object-src 'none'; frame-src 'none'; " +
				"base-uri 'none'; frame-ancestors 'none'; form-action 'self'"
			h.Set("Content-Security-Policy", csp)
		}
		if s.secureRequest(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// sameOriginWrites rejects browser cross-site state changes. CLI clients send
// neither Sec-Fetch-Site nor Origin and are allowed; the admin session cookie
// is SameSite=Strict as a second line.
func (s *Server) sameOriginWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
			writeError(w, http.StatusForbidden, "cross-site write refused")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !s.originAllowed(r, origin) {
			writeError(w, http.StatusForbidden, "origin refused")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originAllowed(r *http.Request, origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.Scheme == "null" {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) && strings.EqualFold(u.Scheme, metaFrom(r.Context()).Scheme) {
		return true
	}
	for _, raw := range []string{s.cfg.PublicURL, s.cfg.AdminURL} {
		if raw == "" {
			continue
		}
		allowed, err := url.Parse(raw)
		if err == nil && strings.EqualFold(u.Scheme, allowed.Scheme) && strings.EqualFold(u.Host, allowed.Host) {
			return true
		}
	}
	return false
}

// validHostHeader keeps untrusted Host bytes from being spliced into a CSP
// source list; only characters that can appear in a DNS name, IPv6 literal or
// port are accepted.
func validHostHeader(host string) bool {
	if host == "" || len(host) > 300 {
		return false
	}
	for i := 0; i < len(host); i++ {
		c := host[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '-', c == ':', c == '_', c == '[', c == ']':
		default:
			return false
		}
	}
	return true
}

func (s *Server) hashPassword(ctx context.Context, password string) (string, error) {
	select {
	case s.passwordGate <- struct{}{}:
		defer func() { <-s.passwordGate }()
		return security.HashPassword(password)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (s *Server) verifyPassword(ctx context.Context, password, hash string) (bool, error) {
	select {
	case s.passwordGate <- struct{}{}:
		defer func() { <-s.passwordGate }()
		return security.VerifyPassword(password, hash), nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

type metaKey struct{}

func metaFrom(ctx context.Context) proxy.Info {
	info, _ := ctx.Value(metaKey{}).(proxy.Info)
	return info
}

// secureRequest treats the request as HTTPS when the public URL is HTTPS even
// if the hop to this process is plain HTTP (a TLS-terminating reverse proxy
// that is not one of the configured Cloudflare modes).
func (s *Server) secureRequest(r *http.Request) bool {
	if metaFrom(r.Context()).Scheme == "https" {
		return true
	}
	u, err := url.Parse(s.cfg.PublicURL)
	return err == nil && u.Scheme == "https"
}

func ipKey(ctx context.Context) string {
	ip := metaFrom(ctx).IP
	if !ip.IsValid() {
		return "unknown"
	}
	return ip.String()
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("multiple JSON values")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (s *Server) internal(w http.ResponseWriter, what string, err error) {
	s.logger.Error(what, "error", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}
