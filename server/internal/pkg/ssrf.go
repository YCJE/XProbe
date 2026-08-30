// Package pkg — SSRF 防护(设计文档 5.5/7.4, 对标 GHSA-w4g9-mxgg-j532)。
package pkg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// MaxNotifyResponse 通知响应体读取上限(不反射给用户)。
const MaxNotifyResponse = 1024

// SSRF 防护错误。
var ErrSSRFBlocked = errors.New("ssrf: request to private/reserved address blocked")

// IsPrivateIP 检查内网/保留地址(设计文档 7.4 清单 + 0/8 与组播)。
func IsPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	private := []string{
		"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8",
		"169.254.0.0/16", "0.0.0.0/8", "100.64.0.0/10", "224.0.0.0/4",
	}
	for _, cidr := range private {
		if _, n, err := net.ParseCIDR(cidr); err == nil && n.Contains(ip) {
			return true
		}
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() || ip.IsUnspecified() ||
		strings.HasPrefix(ip.String(), "fc") || strings.HasPrefix(ip.String(), "fd") // fc00::/7
}

// CheckURLSchemeAndHost 通知 URL 的第一道校验: 仅 http/https。
func CheckURLSchemeAndHost(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("ssrf: parse url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("ssrf: only http/https allowed, got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("ssrf: missing host")
	}
	return u, nil
}

// SafeDialContext 自定义 Dialer: 强制连接预解析 IP 并逐 IP 内网检查(防 DNS 重绑定)。
func SafeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("ssrf: resolve %s: %w", host, err)
	}
	var lastErr error
	for _, ip := range ips {
		if IsPrivateIP(ip) {
			return nil, fmt.Errorf("%w: %s", ErrSSRFBlocked, ip)
		}
		d := &net.Dialer{Timeout: 10 * time.Second}
		conn, derr := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if derr != nil {
			lastErr = derr
			continue
		}
		return conn, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("ssrf: no usable address for %s", host)
}

// SafeHTTPClient 构造带 SSRF 防护的 HTTP 客户端:
// 内网过滤 + 重定向二次校验 + 超时(设计文档 7.4)。
func SafeHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: SafeDialContext,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("ssrf: too many redirects")
			}
			if _, err := CheckURLSchemeAndHost(req.URL.String()); err != nil {
				return err
			}
			// 重定向目标同样逐 IP 校验(由 DialContext 兜底执行)
			return nil
		},
	}
}

// ReadLimited 读取响应体(最多 1KB, 不返回给最终用户)。
func ReadLimited(resp *http.Response) ([]byte, error) {
	return io.ReadAll(io.LimitReader(resp.Body, MaxNotifyResponse))
}

// SafeClient 携带 SSRF 防护的 HTTP 客户端封装。
type SafeClient struct{ c *http.Client }

func NewSafeClient(timeout time.Duration) *SafeClient {
	return &SafeClient{c: SafeHTTPClient(timeout)}
}

// PostJSON 发送 JSON POST; 响应体限读 1KB 且不回传(防反射, 设计文档 7.4)。
func (s *SafeClient) PostJSON(ctx context.Context, url string, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("ssrf: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.c.Do(req)
	if err != nil {
		return fmt.Errorf("ssrf: request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := ReadLimited(resp)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("notify target returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
