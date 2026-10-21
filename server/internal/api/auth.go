package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pquerna/otp/totp"
	"probe/server/internal/model"
	"probe/server/internal/repository"
	"probe/server/internal/security"
)

const sessionCookie = "probe_session"

type adminKey struct{}
type sessionKey struct{}

func adminFrom(ctx context.Context) *model.Admin {
	a, _ := ctx.Value(adminKey{}).(*model.Admin)
	return a
}

func sessionFrom(ctx context.Context) *model.Session {
	s, _ := ctx.Value(sessionKey{}).(*model.Session)
	return s
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || cookie.Value == "" {
			writeError(w, http.StatusUnauthorized, "未登录")
			return
		}
		session, err := s.sessions.GetByTokenHash(r.Context(), security.HashToken(cookie.Value))
		if errors.Is(err, repository.ErrSessionNotFound) {
			s.clearSessionCookie(w, r)
			writeError(w, http.StatusUnauthorized, "会话已失效")
			return
		}
		if err != nil {
			s.internal(w, "lookup session", err)
			return
		}
		now := s.now()
		if session.Revoked || !now.Before(session.ExpiresAt) || !now.Before(session.IdleExpiresAt) {
			_ = s.sessions.Revoke(r.Context(), session.ID)
			s.clearSessionCookie(w, r)
			writeError(w, http.StatusUnauthorized, "会话已过期")
			return
		}
		admin, err := s.admins.GetByID(r.Context(), session.AdminID)
		if errors.Is(err, repository.ErrAdminNotFound) {
			s.clearSessionCookie(w, r)
			writeError(w, http.StatusUnauthorized, "账号不存在")
			return
		}
		if err != nil {
			s.internal(w, "lookup admin", err)
			return
		}
		idle := now.Add(s.cfg.Auth.SessionIdle.Duration)
		if idle.After(session.ExpiresAt) {
			idle = session.ExpiresAt
		}
		if session.LastSeenAt == nil || now.Sub(*session.LastSeenAt) >= time.Minute {
			if err := s.sessions.Touch(r.Context(), session.ID, idle, now); err != nil {
				s.logger.Warn("touch session", "error", err)
			}
		}
		ctx := context.WithValue(r.Context(), adminKey{}, admin)
		ctx = context.WithValue(ctx, sessionKey{}, session)
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) loginHandler(w http.ResponseWriter, r *http.Request) {
	ip := ipKey(r.Context())
	if !s.login.Allow(ip) {
		writeError(w, http.StatusTooManyRequests, "尝试过于频繁，请稍后再试")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "用户名和密码不能为空")
		return
	}

	admin, err := s.admins.GetByUsername(r.Context(), req.Username)
	if errors.Is(err, repository.ErrAdminNotFound) {
		admin = nil
	} else if err != nil {
		s.internal(w, "lookup admin", err)
		return
	}
	hash := s.dummyHash
	if admin != nil {
		hash = admin.PasswordHash
	}
	ok, err := s.verifyPassword(r.Context(), req.Password, hash)
	if err != nil {
		s.internal(w, "verify password", err)
		return
	}
	if admin == nil || !ok {
		s.login.Failure(ip)
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if admin.TOTPEnabled {
		secret, err := s.secretBox.Decrypt(admin.TOTPSecret)
		if err != nil {
			s.internal(w, "decrypt TOTP", err)
			return
		}
		if !totp.Validate(strings.TrimSpace(req.TOTPCode), secret) {
			used, err := s.consumeRecoveryCode(r.Context(), admin, req.TOTPCode)
			if err != nil {
				s.internal(w, "recovery code", err)
				return
			}
			if !used {
				s.login.Failure(ip)
				writeError(w, http.StatusUnauthorized, "需要有效的两步验证码")
				return
			}
		}
	}
	s.login.Success(ip)

	token, session, err := s.createSession(r, admin.ID)
	if err != nil {
		s.internal(w, "create session", err)
		return
	}
	s.setSessionCookie(w, r, token, session.ExpiresAt)
	s.recordAudit(r, "auth.login", "", "")
	s.logger.Info("admin signed in", "ip", ip)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"username":   admin.Username,
		"session_id": session.ID,
		"expires_at": session.ExpiresAt.Unix(),
	})
}

func (s *Server) createSession(r *http.Request, adminID int64) (string, *model.Session, error) {
	now := s.now()
	sessionID, err := security.RandomID()
	if err != nil {
		return "", nil, err
	}
	token, err := security.GenerateToken()
	if err != nil {
		return "", nil, err
	}
	expires := now.Add(s.cfg.Auth.SessionMax.Duration)
	idle := now.Add(s.cfg.Auth.SessionIdle.Duration)
	if idle.After(expires) {
		idle = expires
	}
	session := &model.Session{
		ID:            sessionID,
		TokenHash:     security.HashToken(token),
		AdminID:       adminID,
		ExpiresAt:     expires,
		IdleExpiresAt: idle,
		CreatedAt:     now,
		LastSeenAt:    &now,
		IP:            ipKey(r.Context()),
		UserAgent:     r.UserAgent(),
	}
	if err := s.sessions.Create(r.Context(), session); err != nil {
		return "", nil, err
	}
	return token, session, nil
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secureRequest(r),
		SameSite: http.SameSiteStrictMode,
		Expires:  expires,
		MaxAge:   int(s.cfg.Auth.SessionMax.Duration.Seconds()),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secureRequest(r),
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Unix(1, 0),
		MaxAge:   -1,
	})
}

func (s *Server) logoutHandler(w http.ResponseWriter, r *http.Request) {
	session := sessionFrom(r.Context())
	if session != nil {
		if err := s.sessions.Revoke(r.Context(), session.ID); err != nil {
			s.logger.Warn("revoke session", "error", err)
		}
	}
	s.recordAudit(r, "auth.logout", "", "")
	s.clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) meHandler(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	session := sessionFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"username":        admin.Username,
		"session_id":      session.ID,
		"expires_at":      session.ExpiresAt.Unix(),
		"idle_expires_at": session.IdleExpiresAt.Unix(),
	})
}
