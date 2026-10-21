// Command agent is the probe agent binary.
//
// -once is the M1 acceptance path: collect two samples and print one report
// frame to stdout. Normal mode loads the YAML config, enrolls with a one-time
// code if needed, and maintains the one-way WebSocket reporter.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"probe/agent/internal/collector"
	"probe/agent/internal/config"
	"probe/agent/internal/register"
	"probe/agent/internal/reporter"
	"probe/agent/internal/state"
	"probe/agent/internal/transport"
	"probe/common/wire"
)

const agentVersion = "0.3.0"

func main() {
	var (
		once       = flag.Bool("once", false, "take two samples, print one report frame to stdout, exit")
		configPath = flag.String("config", defaultConfigPath(), "path to the agent config YAML")
		statePath  = flag.String("state", "", "override state file path")
		interval   = flag.Int("interval", 0, "override report interval in seconds (1-60)")
		sampleGap  = flag.Int("sample-gap-ms", 500, "time between the two samples in -once mode")
	)
	flag.Parse()

	if *once {
		path := *statePath
		if path == "" {
			path = defaultStatePath()
		}
		if err := runOnce(path, *sampleGap); err != nil {
			log.Fatalf("probe-agent: %v", err)
		}
		return
	}
	if err := runAgent(*configPath, *statePath, *interval); err != nil {
		log.Fatalf("probe-agent: %v", err)
	}
}

func runOnce(statePath string, gapMs int) error {
	tracker, err := state.Open(statePath)
	if err != nil {
		return fmt.Errorf("open state: %w", err)
	}
	defer func() { _ = tracker.Close() }()
	coll := collector.New(agentVersion, tracker)
	if _, err := coll.Collect(); err != nil {
		return fmt.Errorf("first sample: %w", err)
	}
	time.Sleep(time.Duration(gapMs) * time.Millisecond)
	data, err := coll.Collect()
	if err != nil {
		return fmt.Errorf("second sample: %w", err)
	}
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("hostname: %w", err)
	}
	report := wire.Report{
		Type:         wire.TypeReport,
		Timestamp:    time.Now().Unix(),
		Hostname:     hostname,
		AgentVersion: agentVersion,
		Data:         data,
	}
	if err := report.Validate(); err != nil {
		return fmt.Errorf("self-check: %w", err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func runAgent(configPath, stateOverride string, intervalOverride int) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if stateOverride != "" {
		cfg.StateFile = stateOverride
	}
	if intervalOverride != 0 {
		cfg.ReportInterval = intervalOverride
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	client, err := transport.HTTPClient(cfg)
	if err != nil {
		return fmt.Errorf("build HTTP client: %w", err)
	}
	if cfg.Token == "" {
		if cfg.RegisterCode == "" {
			return fmt.Errorf("config has neither token nor register_code")
		}
		log.Printf("registering %s %s with %s", runtime.GOOS, runtime.GOARCH, cfg.Server)
		out, err := register.Register(ctx, cfg, client, agentVersion)
		if err != nil {
			return fmt.Errorf("register: %w", err)
		}
		cfg.Token = out.Token
		cfg.RegisterCode = ""
		if err := cfg.Save(configPath); err != nil {
			return fmt.Errorf("save registered token: %w", err)
		}
		log.Printf("registered as agent %d; token saved to %s", out.AgentID, configPath)
	}
	// Rebuild after registration so Save/Validate changes cannot leave a stale
	// client; TLS settings are unchanged but this keeps the flow obvious.
	client, err = transport.HTTPClient(cfg)
	if err != nil {
		return fmt.Errorf("build HTTP client: %w", err)
	}

	tracker, err := state.Open(cfg.StateFile)
	if err != nil {
		return fmt.Errorf("open state: %w", err)
	}
	defer func() { _ = tracker.Close() }()
	coll := collector.New(agentVersion, tracker)
	rep := reporter.New(cfg, coll, client, agentVersion, newLogger())
	log.Printf("probe-agent %s (%s/%s): reporting every %ds", agentVersion, runtime.GOOS, runtime.GOARCH, cfg.ReportInterval)
	err = rep.Run(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func defaultConfigPath() string {
	if runtime.GOOS == "windows" {
		return "probe-agent.yml"
	}
	return "/etc/probe-agent/config.yml"
}

func defaultStatePath() string {
	if runtime.GOOS == "windows" {
		return "probe-agent.state.json"
	}
	return "/var/lib/probe-agent/state.json"
}

func newLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}
