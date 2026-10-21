package api

import (
	"crypto/subtle"
	"net/http"
)

// publicDashboard returns a whitelisted view of the agents. It never includes
// addresses, tokens, agent versions or configuration.
func (s *Server) publicDashboard(w http.ResponseWriter, r *http.Request) {
	shareID := r.PathValue("shareId")
	if shareID == "" {
		writeError(w, http.StatusNotFound, "公开页不存在")
		return
	}
	enabled, err := s.settings.Get(r.Context(), settingPublicEnabled)
	if err != nil || enabled != "true" {
		writeError(w, http.StatusNotFound, "公开页不存在")
		return
	}
	configured, err := s.settings.Get(r.Context(), settingPublicShareID)
	if err != nil || configured == "" ||
		subtle.ConstantTimeCompare([]byte(configured), []byte(shareID)) != 1 {
		writeError(w, http.StatusNotFound, "公开页不存在")
		return
	}
	title, _ := s.settings.Get(r.Context(), settingPublicTitle)
	if title == "" {
		title = "服务器状态"
	}
	servers, err := s.serverSummaries(r.Context())
	if err != nil {
		s.internal(w, "public dashboard", err)
		return
	}
	allowed := []string{
		"id", "hostname", "display_name", "os", "arch", "online", "last_seen",
		"cpu_usage", "mem_usage", "disk_usage", "rx_speed", "tx_speed", "uptime", "region",
	}
	out := make([]map[string]any, 0, len(servers))
	for _, server := range servers {
		item := map[string]any{}
		for _, key := range allowed {
			if value, ok := server[key]; ok {
				item[key] = value
			}
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"title": title, "servers": out})
}
