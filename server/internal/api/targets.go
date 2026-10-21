package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"probe/server/internal/model"
	"probe/server/internal/repository"
)

type targetBody struct {
	Target    string `json:"target"`
	Name      string `json:"name"`
	Region    string `json:"region"`
	ISP       string `json:"isp"`
	IPVersion int    `json:"ip_version"`
	Protocol  string `json:"protocol"`
	Enabled   *bool  `json:"enabled"`
}

func (s *Server) listPingTargets(w http.ResponseWriter, r *http.Request) {
	targets, err := s.targets.ListAll(r.Context())
	if err != nil {
		s.internal(w, "list ping targets", err)
		return
	}
	if targets == nil {
		targets = []model.PingTarget{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"targets": targets})
}

func validateTarget(body targetBody) (*model.PingTarget, error) {
	target := strings.TrimSpace(body.Target)
	name := strings.TrimSpace(body.Name)
	region := strings.TrimSpace(body.Region)
	isp := strings.TrimSpace(body.ISP)
	if target == "" || len(target) > 255 || name == "" || len(name) > 64 {
		return nil, errors.New("目标或名称无效")
	}
	if len(region) > 64 || len(isp) > 80 {
		return nil, errors.New("区域或运营商信息过长")
	}
	for _, value := range []string{target, name, region, isp} {
		if strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return nil, errors.New("目标信息包含控制字符")
		}
	}
	protocol := strings.ToLower(strings.TrimSpace(body.Protocol))
	if protocol == "" {
		protocol = "icmp"
	}
	switch protocol {
	case "icmp", "tcp", "http":
	default:
		return nil, errors.New("协议必须是 icmp/tcp/http")
	}
	if protocol == "http" {
		raw := target
		if !strings.Contains(raw, "://") {
			raw = "https://" + raw
		}
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, errors.New("HTTP 目标必须是 http(s) URL")
		}
	}
	if body.IPVersion != 4 && body.IPVersion != 6 {
		return nil, errors.New("IP 版本必须是 4 或 6")
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	return &model.PingTarget{
		Target: target, Name: name, Region: region,
		ISP: isp, IPVersion: body.IPVersion,
		Protocol: protocol, Enabled: enabled,
	}, nil
}

func (s *Server) createPingTarget(w http.ResponseWriter, r *http.Request) {
	var body targetBody
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	count, err := s.targets.Count(r.Context())
	if err != nil {
		s.internal(w, "count ping targets", err)
		return
	}
	if count >= 64 {
		writeError(w, http.StatusTooManyRequests, "探测目标最多 64 个")
		return
	}
	target, err := validateTarget(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.targets.Create(r.Context(), target, s.now()); err != nil {
		s.internal(w, "create ping target", err)
		return
	}
	s.recordAudit(r, "target.create", strconv.FormatInt(target.ID, 10), target.Target)
	writeJSON(w, http.StatusCreated, target)
}

func (s *Server) updatePingTarget(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "无效的目标 ID")
		return
	}
	existing, err := s.targets.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "目标不存在")
		return
	}
	var body targetBody
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	target, err := validateTarget(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	target.ID = id
	target.IsDefault = existing.IsDefault
	target.CreatedAt = existing.CreatedAt
	if err := s.targets.Update(r.Context(), target); err != nil {
		s.internal(w, "update ping target", err)
		return
	}
	s.recordAudit(r, "target.update", strconv.FormatInt(id, 10), target.Target)
	writeJSON(w, http.StatusOK, target)
}

func (s *Server) deletePingTarget(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "无效的目标 ID")
		return
	}
	if err := s.targets.Delete(r.Context(), id); err != nil {
		if errors.Is(err, repository.ErrPingTargetNotFound) {
			writeError(w, http.StatusConflict, "默认目标不能删除，可停用")
		} else {
			s.internal(w, "delete ping target", err)
		}
		return
	}
	s.recordAudit(r, "target.delete", strconv.FormatInt(id, 10), "")
	w.WriteHeader(http.StatusNoContent)
}
