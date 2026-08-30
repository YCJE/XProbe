// Package tlsconf 构造 Agent 侧强制 TLS + 证书 Pinning 配置(设计文档 4.2/7.5, S2)。
package tlsconf

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ValidateServerURL 校验 Server 地址: 必须是 https(S2, 拒绝 http/ws 明文)。
func ValidateServerURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("server url: %w", err)
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("server url scheme must be https (S2), got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("server url missing host")
	}
	return u, nil
}

// WSURL 将 https 基址转换为 wss WebSocket 地址。
func WSURL(u *url.URL) string {
	w := *u
	w.Scheme = "wss"
	return w.String()
}

// New 构造证书 Pinning TLS 配置。
// 校验策略: 系统证书链验证通过 OR 叶子证书 SHA256 指纹命中白名单, 其余一律拒绝。
// 白名单支持 [旧, 新] 双指纹平滑轮换(设计文档 4.3)。
// InsecureSkipVerify=true 并非跳过校验——校验在 VerifyPeerCertificate 中手工完成
// (若走系统校验, 自签+指纹场景会在回调前即失败)。
func New(allowedFingerprints []string, serverName string) *tls.Config {
	allowed := make(map[string]bool, len(allowedFingerprints))
	for _, f := range allowedFingerprints {
		f = strings.ToLower(strings.TrimSpace(f))
		if f != "" {
			allowed[f] = true
		}
	}
	return &tls.Config{
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			return verify(rawCerts, allowed, serverName)
		},
	}
}

func verify(rawCerts [][]byte, allowed map[string]bool, serverName string) error {
	if len(rawCerts) == 0 {
		return errors.New("tls: no peer certificates")
	}
	leaf, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return fmt.Errorf("tls: parse leaf: %w", err)
	}

	// 1) 系统证书链(Let's Encrypt 等公有 CA 场景)
	if roots, rerr := x509.SystemCertPool(); rerr == nil && len(roots.Subjects()) >= 0 {
		inters := x509.NewCertPool()
		for _, raw := range rawCerts[1:] {
			if c, perr := x509.ParseCertificate(raw); perr == nil {
				inters.AddCert(c)
			}
		}
		if _, verr := leaf.Verify(x509.VerifyOptions{
			DNSName:       serverName,
			Roots:         roots,
			Intermediates: inters,
			KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}); verr == nil {
			return nil
		}
	}

	// 2) 指纹 Pinning(自签/私有 CA 场景)
	h := sha256.Sum256(rawCerts[0])
	if allowed[hex.EncodeToString(h[:])] {
		return nil
	}
	return errors.New("tls: certificate not trusted by system and fingerprint not pinned (S2)")
}
