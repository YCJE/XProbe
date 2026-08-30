package pkg

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// SessionCookie 会话 Cookie 名。
const SessionCookie = "xprobe_session"

// JWT 错误。
var ErrInvalidToken = errors.New("auth: invalid token")

// Claims JWT 载荷。
type Claims struct {
	AdminID  int64  `json:"aid"`
	Username string `json:"usr"`
	jwt.RegisteredClaims
}

// JWTManager 签发/校验管理员 JWT(HS256, 2h 有效期 + 静默续期, 设计文档 7.3)。
type JWTManager struct {
	secret       []byte
	expiry       time.Duration
	renewWindow  time.Duration
	cookieSecure bool
}

func NewJWTManager(secret string, expiry time.Duration, cookieSecure bool) *JWTManager {
	return &JWTManager{
		secret:       []byte(secret),
		expiry:       expiry,
		renewWindow:  30 * time.Minute, // 剩余不足 30 分钟时静默续期
		cookieSecure: cookieSecure,
	}
}

// Issue 签发 Token, 返回 (token, 过期时间)。
func (m *JWTManager) Issue(adminID int64, username string) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(m.expiry)
	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		return "", time.Time{}, err
	}
	claims := &Claims{
		AdminID:  adminID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        hex.EncodeToString(jti),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
			Issuer:    "xprobe",
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, exp, nil
}

// Parse 校验并解析 Token。
func (m *JWTManager) Parse(tokenStr string) (*Claims, error) {
	tok, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(*jwt.Token) (any, error) {
		return m.secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !tok.Valid {
		return nil, ErrInvalidToken
	}
	claims, ok := tok.Claims.(*Claims)
	if !ok {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// NeedsRenewal 剩余有效期不足续期窗口时返回 true。
func (m *JWTManager) NeedsRenewal(claims *Claims) bool {
	if claims.ExpiresAt == nil {
		return false
	}
	return time.Until(claims.ExpiresAt.Time) < m.renewWindow
}

// SetSessionCookie 下发 HttpOnly + Secure + SameSite=Strict 会话 Cookie(设计文档 7.3)。
func (m *JWTManager) SetSessionCookie(w http.ResponseWriter, token string, exp time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.cookieSecure,
		SameSite: http.SameSiteStrictMode,
		Expires:  exp,
	})
}

// ClearSessionCookie 清除会话 Cookie。
func (m *JWTManager) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   m.cookieSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

// ValidatePasswordPolicy 密码策略: ≥12 位且含大小写与数字(设计文档 7.3)。
func ValidatePasswordPolicy(pw string) error {
	var hasUpper, hasLower, hasDigit bool
	for _, c := range pw {
		switch {
		case 'A' <= c && c <= 'Z':
			hasUpper = true
		case 'a' <= c && c <= 'z':
			hasLower = true
		case '0' <= c && c <= '9':
			hasDigit = true
		}
	}
	if len(pw) < 12 || !hasUpper || !hasLower || !hasDigit {
		return fmt.Errorf("password must be >=12 chars with upper, lower and digit")
	}
	return nil
}
