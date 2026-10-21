package reporter

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"probe/agent/internal/config"
	"probe/common/wire"
)

type fakeCollector struct{}

func (fakeCollector) Collect() (wire.ReportData, error) {
	return wire.ReportData{
		CPU:     wire.CPUMetrics{Usage: 1, Cores: 1},
		Memory:  wire.MemoryMetrics{Total: 100, Used: 10},
		Network: wire.NetworkMetrics{},
	}, nil
}

func TestReporterSendsReport(t *testing.T) {
	reports := make(chan wire.Report, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/agent/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"report_interval":1,"config_sync_interval":60,"ping_targets":[]}`)
	})
	mux.HandleFunc("/api/v1/agent/report", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, data, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			var envelope struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(data, &envelope)
			switch envelope.Type {
			case wire.TypeHeartbeat:
				_ = conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"heartbeat_ack"}`))
			case wire.TypeReport:
				var report wire.Report
				if json.Unmarshal(data, &report) == nil {
					select {
					case reports <- report:
					default:
					}
				}
			}
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	cfg := &config.Config{
		Server:             server.URL,
		Token:              "token",
		ReportInterval:     1,
		ConfigSyncInterval: 60,
		StateFile:          "unused",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	rep := New(cfg, fakeCollector{}, &http.Client{Timeout: 5 * time.Second}, "test", logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { _ = rep.Run(ctx) }()

	select {
	case report := <-reports:
		if report.Type != wire.TypeReport || report.Hostname == "" {
			t.Fatalf("unexpected report: %+v", report)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for a report")
	}
}
