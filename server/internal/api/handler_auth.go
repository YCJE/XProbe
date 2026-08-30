package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/YCJE/XProbe/internal/model"
	"github.com/YCJE/XProbe/server/internal/pkg"
	"github.com/YCJE/XProbe/server/internal/service"
)

// JWTAuth 管理员认证中间件: Cookie 优先, 兼容 Authorization Bearer(测试/CLI)。
// 校验 JWT 签名 + sessions 表未吊销; 剩余 <30min 静默续期(新会话签发并吊销旧会话)。
func (d Deps) JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := tokenFromRequest(c)
		if token == "" {
			abort401(c)
			return
		}
		claims, err := d.Auth.Validate(c.Request.Context(), token)
		if err != nil {
			abort401(c)
			return
		}
		c.Set("claims", claims)
		c.Set("token", token)

		if d.JWT.NeedsRenewal(claims) {
			if newToken, exp, rerr := d.Auth.RenewSession(c.Request.Context(), token,
				claims.Username, c.Request.UserAgent()); rerr == nil {
				d.JWT.SetSessionCookie(c.Writer, newToken, exp)
				c.Set("token", newToken)
			}
		}
		c.Next()
	}
}

func tokenFromRequest(c *gin.Context) string {
	if ck, err := c.Cookie(pkg.SessionCookie); err == nil && ck != "" {
		return ck
	}
	if h := c.GetHeader("Authorization"); len(h) > 7 && h[:7] == "Bearer " {
		return h[7:]
	}
	return ""
}

// HandleSetup POST /api/v1/auth/setup: 首次初始化管理员(仅一次)。
func (d Deps) HandleSetup(c *gin.Context) {
	var req model.SetupRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and password required"})
		return
	}
	err := d.Auth.Setup(c.Request.Context(), req.Username, req.Password, c.Request.UserAgent())
	switch {
	case err == nil:
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	case errors.Is(err, service.ErrSetupDone):
		c.JSON(http.StatusConflict, gin.H{"error": "setup already done"})
	case errors.Is(err, service.ErrWeakPass):
		c.JSON(http.StatusBadRequest, gin.H{"error": "password must be >=12 chars with upper, lower and digit"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}

// HandleSetupState GET /api/v1/auth/setup-state: 前端判断显示初始化页。
func (d Deps) HandleSetupState(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"setup_done": d.Auth.SetupDone(c.Request.Context())})
}

// HandleLogin POST /api/v1/auth/login(中间件已限速 5 次/分钟/IP)。
func (d Deps) HandleLogin(c *gin.Context) {
	var req model.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	token, exp, err := d.Auth.Login(c.Request.Context(), req.Username, req.Password,
		c.ClientIP(), c.Request.UserAgent())
	switch {
	case err == nil:
		d.JWT.SetSessionCookie(c.Writer, token, exp)
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	case errors.Is(err, service.ErrTooManyAuth):
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many login attempts"})
	case errors.Is(err, service.ErrLocked):
		c.JSON(http.StatusLocked, gin.H{"error": "account locked for 15 minutes"})
	case errors.Is(err, service.ErrBadCreds):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "bad credentials"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}

// HandleLogout POST /api/v1/auth/logout: 吊销当前会话并清 Cookie。
func (d Deps) HandleLogout(c *gin.Context) {
	if token := tokenFromRequest(c); token != "" {
		_ = d.Auth.Logout(c.Request.Context(), token)
	}
	d.JWT.ClearSessionCookie(c.Writer)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// HandleSessions GET /api/v1/auth/sessions。
func (d Deps) HandleSessions(c *gin.Context) {
	sessions, err := d.Sessions.ListActive(c.Request.Context(), time.Now().Unix())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	// 标记当前会话
	if cur, _ := d.Sessions.FindByHash(c.Request.Context(), pkg.SHA256Hex(tokenFromRequest(c))); cur != nil {
		for i := range sessions {
			sessions[i].Current = sessions[i].ID == cur.ID
		}
	}
	c.JSON(http.StatusOK, gin.H{"sessions": sessions})
}

// HandleRevokeSession DELETE /api/v1/auth/sessions/:id。
func (d Deps) HandleRevokeSession(c *gin.Context) {
	var uri struct {
		ID int64 `uri:"id" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := d.Sessions.Revoke(c.Request.Context(), uri.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// HandleRevokeAllSessions DELETE /api/v1/auth/sessions: 登出所有设备。
func (d Deps) HandleRevokeAllSessions(c *gin.Context) {
	if err := d.Sessions.RevokeAll(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	d.JWT.ClearSessionCookie(c.Writer)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
