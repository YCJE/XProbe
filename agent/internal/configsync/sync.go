// Package configsync 实现 Agent 探测目标配置的定时 HTTPS 拉取(设计文档 4.7)。
// 只读数据同步, 非控制指令; 拉取失败使用本地缓存。
package configsync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/YCJE/XProbe/internal/model"
)

// Pull 拉取探测目标配置(Authorization 头携带 Token, 禁止入 URL)。
func Pull(ctx context.Context, hc *http.Client, serverURL, token string) (*model.AgentConfigPayload, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL+"/api/v1/agent/config", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("config pull: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("config pull: HTTP %d", resp.StatusCode)
	}
	var out model.AgentConfigPayload
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("config pull response: %w", err)
	}
	return &out, nil
}

// SaveCache 缓存配置到本地(原子写, 权限 600, S8)。
func SaveCache(path string, p *model.AgentConfigPayload) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return atomicWrite(path, b)
}

// LoadCache 读取本地缓存; 不存在或损坏返回错误, 调用方按"无缓存"处理。
func LoadCache(path string) (*model.AgentConfigPayload, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p model.AgentConfigPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("config cache corrupt: %w", err)
	}
	return &p, nil
}

func atomicWrite(path string, b []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
