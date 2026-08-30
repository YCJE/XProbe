// Package register 实现 Agent 首次注册客户端(HTTPS REST, 设计文档 4.2)。
package register

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/YCJE/XProbe/internal/model"
)

type Client struct {
	HTTP      *http.Client // 须带 TLS Pinning 配置
	ServerURL string       // https 基址
}

// Register 用注册码换取持久 Token。
// 错误按 HTTP 语义返回: 400/401/409/429, 消息可直接展示给安装者。
func (c *Client) Register(ctx context.Context, req model.RegisterRequest) (*model.RegisterResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.ServerURL+"/api/v1/agent/register", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("register request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var out model.RegisterResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return nil, fmt.Errorf("register response: %w", err)
		}
		if out.Token == "" {
			return nil, fmt.Errorf("register response missing token")
		}
		return &out, nil
	default:
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return nil, fmt.Errorf("register: HTTP %d: %s", resp.StatusCode, e.Error)
	}
}
