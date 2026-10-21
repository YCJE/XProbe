package api

import (
	"net/http"
	"path/filepath"

	"probe/server/internal/webui"
)

func (s *Server) installAgentScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(webui.InstallScript())
}

// downloadAgent serves files an operator has placed in
// <data_dir>/agent-dist/<arch>/. Only the two architectures and three fixed
// names below are reachable; there is no user-controlled path component.
func (s *Server) downloadAgent(w http.ResponseWriter, r *http.Request) {
	arch := r.PathValue("arch")
	if arch != "amd64" && arch != "arm64" {
		http.NotFound(w, r)
		return
	}
	file := r.PathValue("file")
	if file == "" {
		file = "probe-agent"
	}
	switch file {
	case "probe-agent", "probe-agent.sha256", "probe-agent.sig":
	default:
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.cfg.DataDir, "agent-dist", arch, file)
	switch {
	case file == "probe-agent":
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="probe-agent"`)
	case file == "probe-agent.sha256":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, path)
}
