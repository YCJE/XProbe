package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/coder/websocket"
	"probe/common/wire"
	"probe/server/internal/security"
)

const minReportGap = 500 * time.Millisecond

func (s *Server) agentWS(w http.ResponseWriter, r *http.Request) {
	agent, err := s.authenticateAgent(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Agent 未认证")
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
	if err != nil {
		s.logger.Warn("agent websocket accept", "error", err)
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(int64(wire.MaxFrameSizeBytes + 1024))

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	sessionID, err := security.RandomID()
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, "session id")
		return
	}
	if previous := s.hub.Attach(agent.ID, sessionID, cancel); previous != nil {
		previous()
	}
	defer func() {
		if s.hub.Detach(agent.ID, sessionID) {
			_ = s.agents.SetOnline(context.Background(), agent.ID, false, s.now())
		}
	}()
	if err := s.agents.SetOnline(ctx, agent.ID, true, s.now()); err != nil {
		s.logger.Warn("mark agent online", "error", err)
	}
	authDone := make(chan struct{})
	defer close(authDone)
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-authDone:
				return
			case <-ticker.C:
				if _, err := s.agents.GetByTokenHash(ctx, agent.TokenHash); err != nil {
					// Token was reset or the node was deleted. Close through
					// the context so the read loop performs normal cleanup.
					cancel()
					return
				}
			}
		}
	}()
	s.logger.Info("agent connected", "id", agent.ID, "hostname", agent.Hostname)
	defer s.logger.Info("agent disconnected", "id", agent.ID, "hostname", agent.Hostname)

	var (
		lastReport time.Time
		window     = time.Now()
		frames     int
		pingLimit  pingBudget
		pingWarned bool
	)
	for {
		readCtx, readCancel := context.WithTimeout(ctx, time.Duration(s.cfg.Monitor.HeartbeatTimeout)*time.Second)
		typ, data, err := conn.Read(readCtx)
		readCancel()
		if err != nil {
			return
		}
		if typ != websocket.MessageText || len(data) > wire.MaxFrameSizeBytes {
			_ = conn.Close(websocket.StatusUnsupportedData, "text frames up to 10 KiB only")
			return
		}
		now := time.Now()
		if now.Sub(window) >= time.Second {
			window, frames = now, 0
		}
		frames++
		if frames > 20 {
			_ = conn.Close(websocket.StatusPolicyViolation, "frame rate too high")
			return
		}

		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			_ = conn.Close(websocket.StatusPolicyViolation, "invalid JSON")
			return
		}
		switch envelope.Type {
		case wire.TypeReport:
			var report wire.Report
			if err := json.Unmarshal(data, &report); err != nil {
				_ = conn.Close(websocket.StatusPolicyViolation, "invalid report")
				return
			}
			if err := report.Validate(); err != nil {
				s.logger.Warn("dropped agent report", "id", agent.ID, "error", err)
				continue
			}
			if delta := now.Unix() - report.Timestamp; delta > 600 || delta < -600 {
				s.logger.Warn("dropped agent report with skewed timestamp", "id", agent.ID, "delta", delta)
				continue
			}
			if !lastReport.IsZero() && now.Sub(lastReport) < minReportGap {
				_ = conn.Close(websocket.StatusPolicyViolation, "reports too fast")
				return
			}
			lastReport = now
			if len(report.AgentVersion) > 64 {
				report.AgentVersion = report.AgentVersion[:64]
			}
			s.hub.Store(agent.ID, report)
			if err := s.recorder.Record(ctx, agent.ID, report, s.now()); err != nil {
				s.logger.Warn("record metric", "error", err)
			}
			if err := s.agents.UpdateIPs(ctx, agent.ID, report.Data.IPInfo.IPv4, report.Data.IPInfo.IPv6); err != nil {
				s.logger.Warn("update agent IPs", "error", err)
			}
			version := report.AgentVersion
			if version == "" {
				version = agent.AgentVersion
			}
			if len(version) > 64 {
				version = version[:64]
			}
			if err := s.agents.Touch(ctx, agent.ID, version, s.now()); err != nil {
				s.logger.Warn("touch agent", "error", err)
			}
		case wire.TypeHeartbeat:
			var heartbeat wire.Heartbeat
			if err := json.Unmarshal(data, &heartbeat); err != nil || heartbeat.Type != wire.TypeHeartbeat {
				_ = conn.Close(websocket.StatusPolicyViolation, "invalid heartbeat")
				return
			}
			if err := s.agents.Touch(ctx, agent.ID, agent.AgentVersion, s.now()); err != nil {
				s.logger.Warn("touch agent", "error", err)
			}
			if err := s.sendAck(ctx, conn); err != nil {
				return
			}
		case wire.TypePingResult:
			var result wire.PingResult
			if err := json.Unmarshal(data, &result); err != nil {
				_ = conn.Close(websocket.StatusPolicyViolation, "invalid ping result")
				return
			}
			if err := result.Validate(); err != nil {
				s.logger.Warn("dropped ping result", "id", agent.ID, "error", err)
				continue
			}
			at := s.now()
			if result.Timestamp > 0 {
				if delta := at.Unix() - result.Timestamp; delta <= 600 && delta >= -600 {
					at = time.Unix(result.Timestamp, 0)
				}
			}
			stats := make([]wire.PingStat, 0, len(result.Data))
			for _, stat := range result.Data {
				stat.Target = strings.TrimSpace(stat.Target)
				if stat.Target == "" {
					continue
				}
				if !utf8.ValidString(stat.Target) || !utf8.ValidString(stat.Name) ||
					!utf8.ValidString(stat.Method) ||
					strings.IndexFunc(stat.Target, unicode.IsControl) >= 0 ||
					strings.IndexFunc(stat.Name, unicode.IsControl) >= 0 ||
					strings.IndexFunc(stat.Method, unicode.IsControl) >= 0 {
					continue
				}
				if len(stat.Target) > 255 {
					stat.Target = stat.Target[:255]
				}
				if len(stat.Name) > 64 {
					stat.Name = stat.Name[:64]
				}
				if len(stat.Method) > 16 {
					stat.Method = stat.Method[:16]
				}
				stats = append(stats, stat)
			}
			if !pingLimit.Allow(len(stats), time.Now()) {
				if !pingWarned {
					s.logger.Warn("dropped ping results over per-agent budget", "id", agent.ID)
					pingWarned = true
				}
				continue
			}
			if err := s.history.InsertPings(ctx, agent.ID, stats, at); err != nil {
				s.logger.Warn("store ping result", "error", err)
			}
		default:
			_ = conn.Close(websocket.StatusPolicyViolation, "unknown frame")
			return
		}
	}
}

func (s *Server) sendAck(ctx context.Context, conn *websocket.Conn) error {
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, []byte(`{"type":"heartbeat_ack"}`))
}
