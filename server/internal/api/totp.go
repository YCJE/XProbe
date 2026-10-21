package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/pquerna/otp/totp"
	"probe/server/internal/model"
	"probe/server/internal/security"
)

const settingTOTPPending = "totp_pending"

func (s *Server) totpSetup(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	var body struct {
		Password string `json:"password"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	ok, err := s.verifyPassword(r.Context(), body.Password, admin.PasswordHash)
	if err != nil {
		s.internal(w, "verify password", err)
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, "密码错误")
		return
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "XProbe", AccountName: admin.Username})
	if err != nil {
		s.internal(w, "generate TOTP", err)
		return
	}
	encrypted, err := s.secretBox.Encrypt(key.Secret())
	if err != nil {
		s.internal(w, "encrypt TOTP", err)
		return
	}
	if err := s.settings.Set(r.Context(), settingTOTPPending, encrypted, s.now()); err != nil {
		s.internal(w, "store TOTP pending", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"secret": key.Secret(), "url": key.URL()})
}

func (s *Server) totpEnable(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	var body struct {
		Code string `json:"code"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	pending, err := s.settings.Get(r.Context(), settingTOTPPending)
	if err != nil || pending == "" {
		writeError(w, http.StatusBadRequest, "请先开始两步验证设置")
		return
	}
	secret, err := s.secretBox.Decrypt(pending)
	if err != nil {
		s.internal(w, "decrypt TOTP", err)
		return
	}
	if !totp.Validate(strings.TrimSpace(body.Code), secret) {
		writeError(w, http.StatusUnauthorized, "验证码错误")
		return
	}
	encrypted, err := s.secretBox.Encrypt(secret)
	if err != nil {
		s.internal(w, "encrypt TOTP", err)
		return
	}
	recovery, hashes, err := newRecoveryCodes()
	if err != nil {
		s.internal(w, "recovery codes", err)
		return
	}
	if err := s.admins.EnableTOTP(r.Context(), admin.ID, encrypted, hashes); err != nil {
		s.internal(w, "enable TOTP", err)
		return
	}
	_ = s.settings.Set(r.Context(), settingTOTPPending, "", s.now())
	s.recordAudit(r, "totp.enable", "", "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "recovery_codes": recovery})
}

func (s *Server) totpDisable(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	var body struct {
		Password string `json:"password"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	ok, err := s.verifyPassword(r.Context(), body.Password, admin.PasswordHash)
	if err != nil {
		s.internal(w, "verify password", err)
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, "密码错误")
		return
	}
	if err := s.admins.ClearTOTP(r.Context(), admin.ID); err != nil {
		s.internal(w, "clear TOTP", err)
		return
	}
	_ = s.settings.Set(r.Context(), settingTOTPPending, "", s.now())
	s.recordAudit(r, "totp.disable", "", "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func newRecoveryCodes() ([]string, string, error) {
	codes := make([]string, 0, 10)
	hashes := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		raw := make([]byte, 10)
		if _, err := rand.Read(raw); err != nil {
			return nil, "", err
		}
		code := strings.ToUpper(hex.EncodeToString(raw))
		codes = append(codes, code)
		hashes = append(hashes, security.HashToken(code))
	}
	encoded, err := json.Marshal(hashes)
	return codes, string(encoded), err
}

// consumeRecoveryCode removes a matching hashed recovery code from the admin.
func (s *Server) consumeRecoveryCode(ctx context.Context, admin *model.Admin, code string) (bool, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" || admin.TOTPRecoveryCodes == "" {
		return false, nil
	}
	var hashes []string
	if err := json.Unmarshal([]byte(admin.TOTPRecoveryCodes), &hashes); err != nil {
		return false, err
	}
	needle := security.HashToken(code)
	index := -1
	for i, hash := range hashes {
		if len(hash) == len(needle) &&
			subtle.ConstantTimeCompare([]byte(hash), []byte(needle)) == 1 {
			index = i
			break
		}
	}
	if index < 0 {
		return false, nil
	}
	hashes = append(hashes[:index], hashes[index+1:]...)
	encoded, err := json.Marshal(hashes)
	if err != nil {
		return false, err
	}
	return s.admins.ConsumeRecoveryCodes(ctx, admin.ID, admin.TOTPRecoveryCodes, string(encoded))
}
