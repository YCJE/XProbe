// Package register exchanges a one-time registration code for the agent's
// long-lived token over HTTPS.
package register

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"

	"probe/agent/internal/config"
	"probe/agent/internal/identity"
	"probe/common/wire"
)

func Register(ctx context.Context, cfg *config.Config, client *http.Client, agentVersion string) (*wire.RegisterResponse, error) {
	if cfg.RegisterCode == "" {
		return nil, fmt.Errorf("register code is empty")
	}
	fingerprint, err := identity.Fingerprint(cfg.InstallSalt)
	if err != nil {
		return nil, fmt.Errorf("host fingerprint: %w", err)
	}
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("hostname: %w", err)
	}
	body, err := json.Marshal(wire.RegisterRequest{
		Hostname:        strings.TrimSpace(hostname),
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		AgentVersion:    agentVersion,
		HostFingerprint: fingerprint,
		InstallSalt:     cfg.InstallSalt,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal registration: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.RegisterURL(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.RegisterCode)
	req.Header.Set("User-Agent", "probe-agent/"+agentVersion)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("register request: %w", err)
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, 64*1024)
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(limited)
		return nil, fmt.Errorf("register refused (%s): %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var out wire.RegisterResponse
	if err := json.NewDecoder(limited).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode registration: %w", err)
	}
	if out.Token == "" || out.AgentID == 0 {
		return nil, fmt.Errorf("hub returned an incomplete registration")
	}
	return &out, nil
}
