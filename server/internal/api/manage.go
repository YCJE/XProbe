package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"probe/server/internal/model"
	"probe/server/internal/notify"
	"probe/server/internal/repository"
	"probe/server/internal/security"
)

const (
	settingPublicEnabled = "public_enabled"
	settingPublicShareID = "public_share_id"
	settingPublicTitle   = "public_title"
)

func (s *Server) recordAudit(r *http.Request, action, target, detail string) {
	actor := "admin"
	if a := adminFrom(r.Context()); a != nil {
		actor = a.Username
	}
	if err := s.auditRepo.Write(r.Context(), s.now(), actor, action, target, detail); err != nil {
		s.logger.Warn("audit write", "error", err)
	}
}

func (s *Server) listAlerts(w http.ResponseWriter, r *http.Request) {
	rules, err := s.alerts.ListRules(r.Context())
	if err != nil {
		s.internal(w, "list alerts", err)
		return
	}
	if rules == nil {
		rules = []model.AlertRule{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

func (s *Server) listAlertHistory(w http.ResponseWriter, r *http.Request) {
	history, err := s.alerts.ListHistory(r.Context(), 100)
	if err != nil {
		s.internal(w, "list alert history", err)
		return
	}
	if history == nil {
		history = []model.AlertHistory{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": history})
}

type alertBody struct {
	Name             string            `json:"name"`
	Metric           string            `json:"metric"`
	Operator         string            `json:"operator"`
	Threshold        float64           `json:"threshold"`
	Duration         int               `json:"duration"`
	Enabled          *bool             `json:"enabled"`
	Scope            *model.AlertScope `json:"scope"`
	NotifyChannelIDs []int64           `json:"notify_channel_ids"`
}

func (s *Server) ruleFromBody(body alertBody) (*model.AlertRule, error) {
	name := strings.TrimSpace(body.Name)
	if name == "" || len(name) > 120 {
		return nil, errors.New("规则名称无效")
	}
	switch body.Metric {
	case "cpu", "memory", "disk", "offline":
	default:
		return nil, errors.New("指标必须是 cpu/memory/disk/offline")
	}
	if body.Metric == "offline" {
		body.Operator = "="
		body.Threshold = 0
	} else {
		switch body.Operator {
		case ">", ">=", "<", "<=", "=":
		default:
			return nil, errors.New("运算符无效")
		}
		if body.Threshold < 0 || body.Threshold > 100 {
			return nil, errors.New("阈值必须在 0-100 之间")
		}
	}
	if body.Duration < 0 || body.Duration > 86400 {
		return nil, errors.New("持续时间必须在 0-86400 秒")
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	scope := model.AlertScope{Type: "all"}
	if body.Scope != nil && body.Scope.Type != "" {
		switch body.Scope.Type {
		case "all", "agent_ids", "tag_ids":
			scope = *body.Scope
		default:
			return nil, errors.New("作用域无效")
		}
	}
	if body.NotifyChannelIDs == nil {
		body.NotifyChannelIDs = []int64{}
	}
	return &model.AlertRule{
		Name: name, Metric: body.Metric, Operator: body.Operator,
		Threshold: body.Threshold, DurationSec: body.Duration, Enabled: enabled,
		Scope: scope, NotifyChannelIDs: body.NotifyChannelIDs, CreatedAt: s.now(),
	}, nil
}

func (s *Server) createAlert(w http.ResponseWriter, r *http.Request) {
	var body alertBody
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	rule, err := s.ruleFromBody(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.alerts.CreateRule(r.Context(), rule); err != nil {
		s.internal(w, "create alert", err)
		return
	}
	s.recordAudit(r, "alert.create", strconv.FormatInt(rule.ID, 10), rule.Name)
	writeJSON(w, http.StatusCreated, rule)
}

func (s *Server) updateAlert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "无效的规则 ID")
		return
	}
	var body alertBody
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	rule, err := s.ruleFromBody(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rule.ID = id
	if err := s.alerts.UpdateRule(r.Context(), rule); err != nil {
		if errors.Is(err, repository.ErrAlertNotFound) {
			writeError(w, http.StatusNotFound, "规则不存在")
		} else {
			s.internal(w, "update alert", err)
		}
		return
	}
	s.recordAudit(r, "alert.update", strconv.FormatInt(id, 10), rule.Name)
	writeJSON(w, http.StatusOK, rule)
}

func (s *Server) deleteAlert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "无效的规则 ID")
		return
	}
	if err := s.alerts.DeleteRule(r.Context(), id); err != nil {
		s.internal(w, "delete alert", err)
		return
	}
	s.recordAudit(r, "alert.delete", strconv.FormatInt(id, 10), "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := s.notifyRepo.List(r.Context())
	if err != nil {
		s.internal(w, "list channels", err)
		return
	}
	out := make([]map[string]any, 0, len(channels))
	for _, c := range channels {
		out = append(out, map[string]any{
			"id": c.ID, "name": c.Name, "type": c.Type, "created_at": c.CreatedAt.Unix(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": out})
}

func (s *Server) createChannel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string         `json:"name"`
		Type   string         `json:"type"`
		Config map[string]any `json:"config"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Type = strings.ToLower(strings.TrimSpace(body.Type))
	if body.Name == "" || len(body.Name) > 80 {
		writeError(w, http.StatusBadRequest, "渠道名称无效")
		return
	}
	if err := notify.ValidateChannel(body.Type, body.Config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	raw, err := json.Marshal(body.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, "配置格式错误")
		return
	}
	encrypted, err := s.secretBox.Encrypt(string(raw))
	if err != nil {
		s.internal(w, "encrypt channel config", err)
		return
	}
	channel := &model.NotifyChannel{Name: body.Name, Type: body.Type, Config: "enc:v1:" + encrypted}
	if _, err := s.notifyRepo.Create(r.Context(), channel, s.now()); err != nil {
		s.internal(w, "create channel", err)
		return
	}
	s.recordAudit(r, "channel.create", strconv.FormatInt(channel.ID, 10), body.Name)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": channel.ID, "name": channel.Name, "type": channel.Type,
	})
}

func (s *Server) deleteChannel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "无效的渠道 ID")
		return
	}
	if err := s.notifyRepo.Delete(r.Context(), id); err != nil {
		if errors.Is(err, repository.ErrChannelNotFound) {
			writeError(w, http.StatusNotFound, "渠道不存在")
		} else {
			s.internal(w, "delete channel", err)
		}
		return
	}
	s.recordAudit(r, "channel.delete", strconv.FormatInt(id, 10), "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) testChannel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "无效的渠道 ID")
		return
	}
	channel, err := s.notifyRepo.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "渠道不存在")
		return
	}
	if err := s.notifier.Send(r.Context(), *channel, "XProbe 测试通知", "这是一条来自 XProbe 的测试通知。"); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	enabled := s.boolSetting(r, settingPublicEnabled)
	shareID, _ := s.settings.Get(r.Context(), settingPublicShareID)
	title, _ := s.settings.Get(r.Context(), settingPublicTitle)
	if title == "" {
		title = "服务器状态"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"public_enabled": enabled, "public_share_id": shareID, "public_title": title,
		"public_url": s.cfg.PublicURL, "totp_enabled": admin.TOTPEnabled, "version": Version,
	})
}

func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PublicEnabled *bool  `json:"public_enabled"`
		PublicTitle   string `json:"public_title"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	now := s.now()
	if body.PublicEnabled != nil {
		if *body.PublicEnabled {
			shareID, _ := s.settings.Get(r.Context(), settingPublicShareID)
			if shareID == "" {
				generated, err := security.RandomID()
				if err != nil {
					s.internal(w, "create share id", err)
					return
				}
				shareID = generated
				if err := s.settings.Set(r.Context(), settingPublicShareID, shareID, now); err != nil {
					s.internal(w, "create share id", err)
					return
				}
			}
		}
		if err := s.settings.Set(r.Context(), settingPublicEnabled, strconv.FormatBool(*body.PublicEnabled), now); err != nil {
			s.internal(w, "save public setting", err)
			return
		}
	}
	if body.PublicTitle != "" {
		title := strings.TrimSpace(body.PublicTitle)
		if len(title) > 80 {
			writeError(w, http.StatusBadRequest, "标题过长")
			return
		}
		if err := s.settings.Set(r.Context(), settingPublicTitle, title, now); err != nil {
			s.internal(w, "save public title", err)
			return
		}
	}
	s.recordAudit(r, "settings.update", "", "")
	s.getSettings(w, r)
}

func (s *Server) boolSetting(r *http.Request, key string) bool {
	value, err := s.settings.Get(r.Context(), key)
	return err == nil && value == "true"
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.sessions.List(r.Context())
	if err != nil {
		s.internal(w, "list sessions", err)
		return
	}
	current := sessionFrom(r.Context())
	out := make([]map[string]any, 0, len(sessions))
	for _, session := range sessions {
		item := map[string]any{
			"id": session.ID, "ip": session.IP, "user_agent": session.UserAgent,
			"created_at": session.CreatedAt.Unix(), "expires_at": session.ExpiresAt.Unix(),
			"current": current != nil && current.ID == session.ID,
		}
		if session.LastSeenAt != nil {
			item["last_seen_at"] = session.LastSeenAt.Unix()
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "无效的会话 ID")
		return
	}
	current := sessionFrom(r.Context())
	if err := s.sessions.Revoke(r.Context(), id); err != nil {
		s.internal(w, "revoke session", err)
		return
	}
	if current != nil && current.ID == id {
		s.clearSessionCookie(w, r)
	}
	s.recordAudit(r, "session.revoke", id, "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	entries, err := s.auditRepo.List(r.Context(), 100)
	if err != nil {
		s.internal(w, "list audit", err)
		return
	}
	if entries == nil {
		entries = []model.AuditEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": entries})
}
