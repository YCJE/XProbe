// Package reporter maintains the agent's one-way WebSocket connection and
// periodically sends reports. It never accepts commands from the server; the
// only inbound frame defined by the protocol is heartbeat_ack.
package reporter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"probe/agent/internal/config"
	"probe/agent/internal/ping"
	"probe/common/wire"
)

const (
	heartbeatInterval = 30 * time.Second
	pingInterval      = 60 * time.Second
)

type Collector interface {
	Collect() (wire.ReportData, error)
}

type Reporter struct {
	cfg      *config.Config
	coll     Collector
	client   *http.Client
	wsClient *http.Client
	version  string
	logger   *slog.Logger
	hostname string
	targets  atomic.Value
}

func New(cfg *config.Config, coll Collector, client *http.Client, version string, logger *slog.Logger) *Reporter {
	wsClient := *client
	wsClient.Timeout = 0 // the upgraded connection outlives a REST timeout
	hostname, _ := os.Hostname()
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		hostname = "unknown"
	}
	rep := &Reporter{
		cfg: cfg, coll: coll, client: client, wsClient: &wsClient,
		version: version, logger: logger, hostname: hostname,
	}
	rep.targets.Store([]wire.PingTarget{})
	return rep
}

func (r *Reporter) Run(ctx context.Context) error {
	// The first Collect only establishes a baseline; CPU and network deltas
	// are meaningful from the next sample onward.
	if _, err := r.coll.Collect(); err != nil {
		return fmt.Errorf("initial collection: %w", err)
	}
	select {
	case <-time.After(500 * time.Millisecond):
	case <-ctx.Done():
		return ctx.Err()
	}

	delay := time.Second
	for {
		started := time.Now()
		err := r.runOnce(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Since(started) > 30*time.Second {
			delay = time.Second
		}
		r.logger.Warn("agent connection ended", "error", err, "retry_in", delay)
		jitter := time.Duration(rand.Int63n(int64(delay/5) + 1))
		select {
		case <-time.After(delay + jitter):
		case <-ctx.Done():
			return ctx.Err()
		}
		delay *= 2
		if delay > 60*time.Second {
			delay = 60 * time.Second
		}
	}
}

func (r *Reporter) runOnce(ctx context.Context) error {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+r.cfg.Token)
	header.Set("User-Agent", "probe-agent/"+r.version)
	conn, _, err := websocket.Dial(ctx, r.cfg.WebSocketURL(), &websocket.DialOptions{
		HTTPHeader: header,
		HTTPClient: r.wsClient,
	})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.CloseNow()
	// The hub only sends heartbeat_ack. Keep inbound frames tiny so a broken
	// or malicious peer cannot make the agent allocate large buffers.
	conn.SetReadLimit(1024)
	r.logger.Info("connected", "url", r.cfg.WebSocketURL())
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var writeMu sync.Mutex
	var pingInFlight atomic.Bool
	triggerPings := func() {
		if !pingInFlight.CompareAndSwap(false, true) {
			return
		}
		go func() {
			defer pingInFlight.Store(false)
			if err := r.sendPings(connCtx, conn, &writeMu); err != nil && connCtx.Err() == nil {
				r.logger.Warn("ping round failed", "error", err)
			}
		}()
	}

	readErr := make(chan error, 1)
	go func() {
		for {
			if _, _, err := conn.Read(connCtx); err != nil {
				readErr <- err
				return
			}
		}
	}()

	reportTicker := time.NewTicker(time.Duration(r.cfg.ReportInterval) * time.Second)
	defer reportTicker.Stop()
	heartbeatTicker := time.NewTicker(heartbeatInterval)
	defer heartbeatTicker.Stop()
	configTicker := time.NewTicker(time.Duration(r.cfg.ConfigSyncInterval) * time.Second)
	defer configTicker.Stop()
	pingTicker := time.NewTicker(pingInterval)
	defer pingTicker.Stop()

	r.pullConfig(connCtx)
	triggerPings()
	for {
		select {
		case <-connCtx.Done():
			return connCtx.Err()
		case err := <-readErr:
			return fmt.Errorf("read: %w", err)
		case <-reportTicker.C:
			if err := r.sendReport(connCtx, conn, &writeMu); err != nil {
				return err
			}
		case <-heartbeatTicker.C:
			if err := r.sendHeartbeat(connCtx, conn, &writeMu); err != nil {
				return err
			}
		case <-configTicker.C:
			r.pullConfig(connCtx)
		case <-pingTicker.C:
			triggerPings()
		}
	}
}

func (r *Reporter) sendReport(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex) error {
	data, err := r.coll.Collect()
	if err != nil {
		return fmt.Errorf("collect: %w", err)
	}
	report := wire.Report{
		Type:         wire.TypeReport,
		Timestamp:    time.Now().Unix(),
		Hostname:     r.hostname,
		AgentVersion: r.version,
		Data:         data,
	}
	if err := report.Validate(); err != nil {
		r.logger.Warn("report self-check failed; dropping sample", "error", err)
		return nil
	}
	payload, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if len(payload) > wire.MaxFrameSizeBytes {
		r.logger.Warn("report exceeds frame limit; dropping sample", "bytes", len(payload))
		return nil
	}
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	writeMu.Lock()
	defer writeMu.Unlock()
	if err := conn.Write(writeCtx, websocket.MessageText, payload); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

func (r *Reporter) sendHeartbeat(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex) error {
	payload, _ := json.Marshal(wire.Heartbeat{Type: wire.TypeHeartbeat, Timestamp: time.Now().Unix()})
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	writeMu.Lock()
	defer writeMu.Unlock()
	if err := conn.Write(writeCtx, websocket.MessageText, payload); err != nil {
		return fmt.Errorf("write heartbeat: %w", err)
	}
	return nil
}

func (r *Reporter) pullConfig(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.cfg.ConfigURL(), nil)
	if err != nil {
		r.logger.Warn("config request", "error", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+r.cfg.Token)
	req.Header.Set("User-Agent", "probe-agent/"+r.version)
	resp, err := r.client.Do(req)
	if err != nil {
		r.logger.Warn("config pull failed", "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		r.logger.Warn("config pull refused", "status", resp.Status)
		return
	}
	var cfg wire.AgentConfig
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&cfg); err != nil {
		r.logger.Warn("config decode failed", "error", err)
		return
	}
	targets := cfg.PingTargets
	if len(targets) > wire.MaxPingEntries {
		targets = targets[:wire.MaxPingEntries]
	}
	r.targets.Store(append([]wire.PingTarget(nil), targets...))
	r.logger.Info("config pulled", "ping_targets", len(targets), "report_interval", cfg.ReportInterval)
}

func (r *Reporter) sendPings(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex) error {
	raw, _ := r.targets.Load().([]wire.PingTarget)
	if len(raw) == 0 {
		return nil
	}
	results := make([]wire.PingStat, 0, len(raw))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, target := range raw {
		target := target
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			stat := ping.Run(ctx, target, r.cfg.PingMethod)
			mu.Lock()
			results = append(results, stat)
			mu.Unlock()
		}()
	}
	wg.Wait()
	if ctx.Err() != nil || len(results) == 0 {
		return ctx.Err()
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Target < results[j].Target })
	const chunk = 20
	for start := 0; start < len(results); start += chunk {
		end := start + chunk
		if end > len(results) {
			end = len(results)
		}
		frame := wire.PingResult{Type: wire.TypePingResult, Timestamp: time.Now().Unix(), Data: results[start:end]}
		if err := frame.Validate(); err != nil {
			r.logger.Warn("ping result self-check failed", "error", err)
			continue
		}
		payload, err := json.Marshal(frame)
		if err != nil {
			return fmt.Errorf("marshal ping result: %w", err)
		}
		if len(payload) > wire.MaxFrameSizeBytes {
			r.logger.Warn("ping frame exceeds limit; dropping chunk", "bytes", len(payload))
			continue
		}
		writeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		writeMu.Lock()
		err = conn.Write(writeCtx, websocket.MessageText, payload)
		writeMu.Unlock()
		cancel()
		if err != nil {
			return fmt.Errorf("write ping result: %w", err)
		}
	}
	r.logger.Info("ping round complete", "targets", len(results))
	return nil
}
