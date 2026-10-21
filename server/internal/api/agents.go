package api

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"probe/common/wire"
	"probe/server/internal/model"
	"probe/server/internal/repository"
	"probe/server/internal/security"
)

const (
	registerCodeTTL = 15 * time.Minute
	maxUnusedCodes  = 5
	configSyncSecs  = 3600
)

func (s *Server) registerAgent(w http.ResponseWriter, r *http.Request) {
	if !s.register.Allow(ipKey(r.Context())) {
		writeError(w, http.StatusTooManyRequests, "注册尝试过于频繁")
		return
	}
	code := bearerToken(r)
	if code == "" {
		writeError(w, http.StatusUnauthorized, "缺少注册码")
		return
	}
	var req wire.RegisterRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	req.Hostname = strings.TrimSpace(req.Hostname)
	if err := validateRegistration(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	token, err := security.GenerateToken()
	if err != nil {
		s.internal(w, "generate agent token", err)
		return
	}
	agent := &model.Agent{
		TokenHash:       security.HashToken(token),
		Hostname:        req.Hostname,
		OS:              req.OS,
		Arch:            req.Arch,
		AgentVersion:    req.AgentVersion,
		HostFingerprint: strings.ToLower(req.HostFingerprint),
		TagIDs:          "[]",
	}
	now := s.now()
	if _, err := s.agents.Register(r.Context(), security.HashRegistrationCode(code), agent, now); err != nil {
		switch {
		case errors.Is(err, repository.ErrRegisterCodeInvalid):
			writeError(w, http.StatusUnauthorized, "注册码无效、已使用或已过期")
		case errors.Is(err, repository.ErrFingerprintConflict):
			writeError(w, http.StatusConflict, "主机指纹已被注册")
		default:
			s.internal(w, "register agent", err)
		}
		return
	}
	if err := s.auditRepo.Write(r.Context(), s.now(), "agent", "agent.register",
		strconv.FormatInt(agent.ID, 10), agent.Hostname); err != nil {
		s.logger.Warn("audit agent register", "error", err)
	}
	s.logger.Info("agent registered", "id", agent.ID, "hostname", agent.Hostname)
	writeJSON(w, http.StatusCreated, wire.RegisterResponse{AgentID: agent.ID, Token: token})
}

func validateRegistration(req wire.RegisterRequest) error {
	if req.Hostname == "" || len(req.Hostname) > 128 || !utf8.ValidString(req.Hostname) ||
		strings.IndexFunc(req.Hostname, unicode.IsControl) >= 0 {
		return errors.New("主机名无效")
	}
	for name, value := range map[string]string{
		"os": req.OS, "arch": req.Arch, "agent_version": req.AgentVersion,
	} {
		if len(value) > 64 || !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return errors.New(name + " 无效")
		}
	}
	if len(req.InstallSalt) > 128 || strings.IndexFunc(req.InstallSalt, unicode.IsControl) >= 0 {
		return errors.New("install_salt 无效")
	}
	raw, err := hex.DecodeString(req.HostFingerprint)
	if err != nil || len(raw) != 32 {
		return errors.New("主机指纹必须是 64 位 SHA-256 hex")
	}
	return nil
}

func (s *Server) agentConfig(w http.ResponseWriter, r *http.Request) {
	if _, err := s.authenticateAgent(r); err != nil {
		writeError(w, http.StatusUnauthorized, "Agent 未认证")
		return
	}
	targets, err := s.targets.ListEnabled(r.Context())
	if err != nil {
		s.internal(w, "list ping targets", err)
		return
	}
	if len(targets) > wire.MaxPingEntries {
		targets = targets[:wire.MaxPingEntries]
	}
	out := wire.AgentConfig{
		ReportInterval:     s.cfg.Monitor.ReportInterval,
		ConfigSyncInterval: configSyncSecs,
		PingInterval:       60,
		PingTargets:        make([]wire.PingTarget, 0, len(targets)),
	}
	for _, t := range targets {
		out.PingTargets = append(out.PingTargets, wire.PingTarget{
			ID: t.ID, Target: t.Target, Name: t.Name, Region: t.Region,
			ISP: t.ISP, IPVersion: t.IPVersion, Protocol: t.Protocol, Enabled: t.Enabled,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) authenticateAgent(r *http.Request) (*model.Agent, error) {
	token := bearerToken(r)
	if token == "" {
		return nil, errors.New("missing token")
	}
	return s.agents.GetByTokenHash(r.Context(), security.HashToken(token))
}

func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	raw := r.Header.Get("Authorization")
	if !strings.HasPrefix(raw, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(raw, prefix))
}

func (s *Server) createRegisterCode(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	unused, err := s.codes.CountUnused(r.Context(), now)
	if err != nil {
		s.internal(w, "count register codes", err)
		return
	}
	if unused >= maxUnusedCodes {
		writeError(w, http.StatusTooManyRequests, "未使用的注册码已达上限")
		return
	}
	code, err := security.GenerateRegistrationCode()
	if err != nil {
		s.internal(w, "generate register code", err)
		return
	}
	expires := now.Add(registerCodeTTL)
	id, err := s.codes.Create(r.Context(), security.HashRegistrationCode(code), now, expires)
	if err != nil {
		s.internal(w, "create register code", err)
		return
	}
	s.recordAudit(r, "register_code.create", strconv.FormatInt(id, 10), "")
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         id,
		"code":       code,
		"expires_at": expires.Unix(),
	})
}

func (s *Server) listRegisterCodes(w http.ResponseWriter, r *http.Request) {
	codes, err := s.codes.List(r.Context())
	if err != nil {
		s.internal(w, "list register codes", err)
		return
	}
	out := make([]map[string]any, 0, len(codes))
	for _, c := range codes {
		item := map[string]any{
			"id":         c.ID,
			"created_at": c.CreatedAt.Unix(),
			"expires_at": c.ExpiresAt.Unix(),
			"used":       c.Used,
		}
		if c.UsedByAgentID != nil {
			item["used_by_agent_id"] = *c.UsedByAgentID
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"codes": out})
}

func (s *Server) deleteRegisterCode(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "无效的注册码 ID")
		return
	}
	if err := s.codes.Delete(r.Context(), id); err != nil {
		s.internal(w, "delete register code", err)
		return
	}
	s.recordAudit(r, "register_code.delete", strconv.FormatInt(id, 10), "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listServers(w http.ResponseWriter, r *http.Request) {
	out, err := s.serverSummaries(r.Context())
	if err != nil {
		s.internal(w, "list agents", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": out})
}

func (s *Server) serverSummaries(ctx context.Context) ([]map[string]any, error) {
	agents, err := s.agents.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(agents))
	for _, a := range agents {
		item := map[string]any{
			"id":                  a.ID,
			"hostname":            a.Hostname,
			"display_name":        a.DisplayName,
			"os":                  a.OS,
			"arch":                a.Arch,
			"agent_version":       a.AgentVersion,
			"online":              s.hub.Online(a.ID),
			"ipv4":                a.IPv4,
			"ipv6":                a.IPv6,
			"region":              a.Region,
			"country_code":        a.CountryCode,
			"isp":                 a.ISP,
			"traffic_quota_bytes": a.TrafficQuota,
		}
		if a.LastSeen != nil {
			item["last_seen"] = a.LastSeen.Unix()
		}
		if a.ExpiresAt != nil {
			item["expires_at"] = a.ExpiresAt.Unix()
		}
		if a.PriceAmount != 0 {
			item["price_amount"] = a.PriceAmount
			item["price_currency"] = a.PriceCurrency
			item["price_cycle"] = a.PriceCycle
		}
		if report, ok := s.hub.Latest(a.ID); ok {
			item["cpu_usage"] = report.Data.CPU.Usage
			item["mem_usage"] = percent(report.Data.Memory.Used, report.Data.Memory.Total)
			item["disk_usage"] = maxDiskPercent(report.Data.Disk)
			item["rx_speed"] = report.Data.Network.RxSpeed
			item["tx_speed"] = report.Data.Network.TxSpeed
			item["uptime"] = report.Data.Uptime
			item["load_1"] = report.Data.CPU.Load1
			item["load_5"] = report.Data.CPU.Load5
			item["load_15"] = report.Data.CPU.Load15
			item["traffic_monthly"] = report.Data.TrafficMonth
			if report.Data.IPInfo.IPv4 != "" {
				item["ipv4"] = report.Data.IPInfo.IPv4
			}
			if report.Data.IPInfo.IPv6 != "" {
				item["ipv6"] = report.Data.IPInfo.IPv6
			}
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Server) serverDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "无效的节点 ID")
		return
	}
	agent, err := s.agents.GetByID(r.Context(), id)
	if errors.Is(err, repository.ErrAgentNotFound) {
		writeError(w, http.StatusNotFound, "节点不存在")
		return
	}
	if err != nil {
		s.internal(w, "get agent", err)
		return
	}
	out := map[string]any{
		"id": agent.ID, "hostname": agent.Hostname, "display_name": agent.DisplayName,
		"os": agent.OS, "arch": agent.Arch, "agent_version": agent.AgentVersion,
		"online": s.hub.Online(agent.ID), "ipv4": agent.IPv4, "ipv6": agent.IPv6,
		"region": agent.Region, "country_code": agent.CountryCode, "isp": agent.ISP,
		"traffic_quota_bytes": agent.TrafficQuota, "created_at": agent.CreatedAt.Unix(),
	}
	if agent.LastSeen != nil {
		out["last_seen"] = agent.LastSeen.Unix()
	}
	if agent.ExpiresAt != nil {
		out["expires_at"] = agent.ExpiresAt.Unix()
	}
	if agent.PriceAmount != 0 {
		out["price_amount"] = agent.PriceAmount
		out["price_currency"] = agent.PriceCurrency
		out["price_cycle"] = agent.PriceCycle
	}
	if report, ok := s.hub.Latest(agent.ID); ok {
		out["cpu_usage"] = report.Data.CPU.Usage
		out["load_1"] = report.Data.CPU.Load1
		out["load_5"] = report.Data.CPU.Load5
		out["load_15"] = report.Data.CPU.Load15
		out["mem_usage"] = percent(report.Data.Memory.Used, report.Data.Memory.Total)
		out["memory_total"] = report.Data.Memory.Total
		out["memory_used"] = report.Data.Memory.Used
		out["swap_total"] = report.Data.Memory.SwapTotal
		out["swap_used"] = report.Data.Memory.SwapUsed
		out["disk_usage"] = maxDiskPercent(report.Data.Disk)
		out["disks"] = diskList(report.Data.Disk)
		out["rx_speed"] = report.Data.Network.RxSpeed
		out["tx_speed"] = report.Data.Network.TxSpeed
		out["tcp_connections"] = report.Data.Network.TCPConnections
		out["udp_connections"] = report.Data.Network.UDPConnections
		out["process_count"] = report.Data.ProcessCount
		out["uptime"] = report.Data.Uptime
		out["traffic_monthly"] = report.Data.TrafficMonth
		if report.AgentVersion != "" {
			out["agent_version"] = report.AgentVersion
		}
		if report.Data.IPInfo.IPv4 != "" {
			out["ipv4"] = report.Data.IPInfo.IPv4
		}
		if report.Data.IPInfo.IPv6 != "" {
			out["ipv6"] = report.Data.IPInfo.IPv6
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) updateServerMeta(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "无效的节点 ID")
		return
	}
	agent, err := s.agents.GetByID(r.Context(), id)
	if errors.Is(err, repository.ErrAgentNotFound) {
		writeError(w, http.StatusNotFound, "节点不存在")
		return
	}
	if err != nil {
		s.internal(w, "get agent", err)
		return
	}
	var body struct {
		DisplayName       string  `json:"display_name"`
		Region            string  `json:"region"`
		CountryCode       string  `json:"country_code"`
		ISP               string  `json:"isp"`
		ExpiresAt         int64   `json:"expires_at"`
		PriceAmount       float64 `json:"price_amount"`
		PriceCurrency     string  `json:"price_currency"`
		PriceCycle        string  `json:"price_cycle"`
		TrafficQuotaBytes int64   `json:"traffic_quota_bytes"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	body.DisplayName = strings.TrimSpace(body.DisplayName)
	body.Region = strings.TrimSpace(body.Region)
	body.CountryCode = strings.ToUpper(strings.TrimSpace(body.CountryCode))
	body.ISP = strings.TrimSpace(body.ISP)
	body.PriceCurrency = strings.ToUpper(strings.TrimSpace(body.PriceCurrency))
	body.PriceCycle = strings.TrimSpace(body.PriceCycle)
	if len(body.DisplayName) > 128 || len(body.Region) > 64 || len(body.CountryCode) > 8 ||
		len(body.ISP) > 80 || len(body.PriceCurrency) > 8 {
		writeError(w, http.StatusBadRequest, "字段过长")
		return
	}
	if body.PriceAmount < 0 || body.TrafficQuotaBytes < 0 {
		writeError(w, http.StatusBadRequest, "金额和流量配额不能为负")
		return
	}
	switch body.PriceCycle {
	case "", "monthly", "yearly", "one-time":
	default:
		writeError(w, http.StatusBadRequest, "计费周期无效")
		return
	}
	agent.DisplayName = body.DisplayName
	agent.Region = body.Region
	agent.CountryCode = body.CountryCode
	agent.ISP = body.ISP
	agent.PriceAmount = body.PriceAmount
	agent.PriceCurrency = body.PriceCurrency
	agent.PriceCycle = body.PriceCycle
	agent.TrafficQuota = body.TrafficQuotaBytes
	agent.ExpiresAt = nil
	if body.ExpiresAt > 0 {
		t := time.Unix(body.ExpiresAt, 0)
		agent.ExpiresAt = &t
	}
	if err := s.agents.UpdateMeta(r.Context(), agent); err != nil {
		s.internal(w, "update agent meta", err)
		return
	}
	s.recordAudit(r, "server.meta", strconv.FormatInt(id, 10), "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) serverHistory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "无效的节点 ID")
		return
	}
	rangeKey := r.URL.Query().Get("range")
	if rangeKey == "" {
		rangeKey = "1h"
	}
	durations := map[string]time.Duration{
		"1h": time.Hour, "6h": 6 * time.Hour, "12h": 12 * time.Hour,
		"1d": 24 * time.Hour, "2d": 48 * time.Hour,
	}
	window, ok := durations[rangeKey]
	if !ok {
		writeError(w, http.StatusBadRequest, "不支持的时间范围")
		return
	}
	if _, err := s.agents.GetByID(r.Context(), id); err != nil {
		if errors.Is(err, repository.ErrAgentNotFound) {
			writeError(w, http.StatusNotFound, "节点不存在")
		} else {
			s.internal(w, "get agent", err)
		}
		return
	}
	since := s.now().Add(-window)
	metrics, err := s.history.ListMetrics(r.Context(), id, since)
	if err != nil {
		s.internal(w, "list metrics", err)
		return
	}
	pings, err := s.history.ListPings(r.Context(), id, since, 240)
	if err != nil {
		s.internal(w, "list pings", err)
		return
	}
	metricOut := make([]map[string]any, 0, len(metrics))
	for _, m := range metrics {
		metricOut = append(metricOut, map[string]any{
			"timestamp": m.Timestamp, "cpu": m.CPU, "mem": m.Mem,
			"disk": m.Disk, "net_rx": m.NetRx, "net_tx": m.NetTx,
		})
	}
	pingOut := make([]map[string]any, 0, len(pings))
	for _, p := range pings {
		points := make([]map[string]any, 0, len(p.Points))
		for _, point := range p.Points {
			points = append(points, map[string]any{
				"timestamp": point.Timestamp, "avg": point.Avg, "min": point.Min,
				"max": point.Max, "jitter": point.Jitter, "loss": point.Loss,
			})
		}
		pingOut = append(pingOut, map[string]any{
			"target": p.Target, "name": p.Name, "method": p.Method,
			"ip_version": p.IPVersion, "points": points,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"range": rangeKey, "metrics": metricOut, "pings": pingOut,
	})
}

func maxDiskPercent(disks []wire.DiskMetrics) float64 {
	max := 0.0
	for _, d := range disks {
		if p := percent(d.Used, d.Total); p > max {
			max = p
		}
	}
	return max
}

func diskList(disks []wire.DiskMetrics) []map[string]any {
	out := make([]map[string]any, 0, len(disks))
	for _, d := range disks {
		out = append(out, map[string]any{
			"device": d.Device, "total": d.Total, "used": d.Used,
			"percent": percent(d.Used, d.Total),
		})
	}
	return out
}

func percent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) * 100 / float64(total)
}
