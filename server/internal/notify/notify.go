// Package notify sends alert messages to Telegram, an SSRF-filtered webhook,
// or SMTP. Every configurable destination is treated as untrusted.
package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/netip"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"probe/server/internal/model"
	"probe/server/internal/security"
)

type Sender struct {
	client  *http.Client
	secrets *security.SecretBox
}

func New(secrets *security.SecretBox) *Sender {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ip, err := resolvePublic(ctx, host)
			if err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip, port))
		},
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        4,
		IdleConnTimeout:     60 * time.Second,
	}
	return &Sender{secrets: secrets, client: &http.Client{
		Timeout:   12 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

func (s *Sender) Send(ctx context.Context, channel model.NotifyChannel, title, message string) error {
	raw := channel.Config
	if strings.HasPrefix(raw, "enc:v1:") {
		if s.secrets == nil {
			return fmt.Errorf("channel %s is encrypted but no key is loaded", channel.Name)
		}
		plain, err := s.secrets.Decrypt(strings.TrimPrefix(raw, "enc:v1:"))
		if err != nil {
			return fmt.Errorf("decrypt channel %s: %w", channel.Name, err)
		}
		raw = plain
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return fmt.Errorf("channel %s config is invalid JSON", channel.Name)
	}
	switch strings.ToLower(channel.Type) {
	case "telegram":
		return s.sendTelegram(ctx, cfg, message)
	case "webhook":
		return s.sendWebhook(ctx, cfg, title, message)
	case "email":
		return sendEmail(ctx, cfg, title, message)
	default:
		return fmt.Errorf("unsupported channel type %q", channel.Type)
	}
}

func (s *Sender) sendTelegram(ctx context.Context, cfg map[string]any, message string) error {
	token := stringValue(cfg["bot_token"])
	chat := stringValue(cfg["chat_id"])
	if token == "" || chat == "" {
		return fmt.Errorf("telegram bot_token and chat_id are required")
	}
	body, _ := json.Marshal(map[string]any{"chat_id": chat, "text": message, "disable_web_page_preview": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.telegram.org/bot"+token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return s.do(req)
}

func (s *Sender) sendWebhook(ctx context.Context, cfg map[string]any, title, message string) error {
	raw := strings.TrimSpace(stringValue(cfg["url"]))
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("webhook url must be http(s)")
	}
	body, _ := json.Marshal(map[string]string{"title": title, "text": message})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	headers, err := webhookHeaders(cfg["headers"])
	if err != nil {
		return err
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	req.Header.Set("Content-Type", "application/json")
	return s.do(req)
}

func (s *Sender) do(req *http.Request) error {
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("notification endpoint returned %s", resp.Status)
	}
	return nil
}

func sendEmail(ctx context.Context, cfg map[string]any, title, message string) error {
	host := stringValue(cfg["host"])
	port := stringValue(cfg["port"])
	if port == "" {
		port = "25"
	}
	from := stringValue(cfg["from"])
	to := stringValue(cfg["to"])
	if host == "" || from == "" || to == "" {
		return fmt.Errorf("email host, from and to are required")
	}
	fromAddr, err := mail.ParseAddress(from)
	if err != nil {
		return fmt.Errorf("invalid from address")
	}
	toAddr, err := mail.ParseAddress(to)
	if err != nil {
		return fmt.Errorf("invalid to address")
	}
	from, to = fromAddr.Address, toAddr.Address
	var auth smtp.Auth
	if user := stringValue(cfg["username"]); user != "" {
		auth = smtp.PlainAuth("", user, stringValue(cfg["password"]), host)
	}
	headers := []string{
		"From: " + from, "To: " + to, "Subject: " + sanitizeHeader(title),
		"MIME-Version: 1.0", "Content-Type: text/plain; charset=UTF-8",
	}
	msg := []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + message)
	return sendSMTP(ctx, host, port, auth, from, to, msg)
}

// sendSMTP dials the IP that resolvePublic approved, then speaks SMTP over
// that exact connection. Calling smtp.SendMail would resolve the hostname a
// second time and reopen the DNS-rebinding gap.
func sendSMTP(ctx context.Context, host, port string, auth smtp.Auth, from, to string, msg []byte) error {
	ip, err := resolvePublic(ctx, host)
	if err != nil {
		return fmt.Errorf("smtp host refused: %w", err)
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip, port))
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	if port == "465" {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return err
		}
		conn = tlsConn
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer client.Close()
	if port != "465" {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
				return err
			}
		} else if auth != nil {
			return fmt.Errorf("SMTP server does not offer STARTTLS; refusing to send credentials in the clear")
		}
	}
	if auth != nil {
		if ok, _ := client.Extension("AUTH"); !ok {
			return fmt.Errorf("SMTP server does not offer AUTH")
		}
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func ValidateChannel(channelType string, cfg map[string]any) error {
	switch strings.ToLower(channelType) {
	case "telegram":
		if stringValue(cfg["bot_token"]) == "" || stringValue(cfg["chat_id"]) == "" {
			return fmt.Errorf("telegram 需要 bot_token 和 chat_id")
		}
	case "webhook":
		raw := strings.TrimSpace(stringValue(cfg["url"]))
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("webhook URL 必须是 http(s) 绝对地址")
		}
		if _, err := webhookHeaders(cfg["headers"]); err != nil {
			return err
		}
	case "email":
		from, to := stringValue(cfg["from"]), stringValue(cfg["to"])
		if stringValue(cfg["host"]) == "" || from == "" || to == "" {
			return fmt.Errorf("邮件需要 host、from、to")
		}
		if _, err := mail.ParseAddress(from); err != nil {
			return fmt.Errorf("发件人地址无效")
		}
		if _, err := mail.ParseAddress(to); err != nil {
			return fmt.Errorf("收件人地址无效")
		}
		if p := stringValue(cfg["port"]); p != "" {
			port, err := strconv.Atoi(p)
			if err != nil || port < 1 || port > 65535 {
				return fmt.Errorf("邮件端口必须是 1-65535")
			}
		}
	default:
		return fmt.Errorf("不支持的通知类型")
	}
	return nil
}

// webhookHeaders accepts the JSON object shown in the UI (or an already
// decoded object from API clients). Header names and values are validated so
// a stored config can never inject a second HTTP request via CRLF.
func webhookHeaders(raw any) (map[string]string, error) {
	if raw == nil {
		return nil, nil
	}
	var values map[string]string
	switch v := raw.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, nil
		}
		if err := json.Unmarshal([]byte(v), &values); err != nil {
			return nil, fmt.Errorf("自定义头必须是 JSON 对象")
		}
	case map[string]any:
		values = make(map[string]string, len(v))
		for name, value := range v {
			text, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("自定义头 %q 的值必须是字符串", name)
			}
			values[name] = text
		}
	default:
		return nil, fmt.Errorf("自定义头必须是 JSON 对象")
	}
	if len(values) > 20 {
		return nil, fmt.Errorf("自定义头最多 20 个")
	}
	for name, value := range values {
		if !validHeaderName(name) {
			return nil, fmt.Errorf("自定义头名称无效: %q", name)
		}
		switch strings.ToLower(name) {
		case "host", "content-length", "transfer-encoding", "connection":
			return nil, fmt.Errorf("不允许自定义头 %q", name)
		}
		if len(value) > 1024 || strings.IndexFunc(value, func(r rune) bool {
			return r < 0x20 && r != '\t' || r == 0x7f
		}) >= 0 || strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("自定义头 %q 的值无效", name)
		}
	}
	return values, nil
}

func validHeaderName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c)):
		default:
			return false
		}
	}
	return true
}

func resolvePublic(ctx context.Context, host string) (string, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		ip = ip.Unmap()
		if blocked(ip) {
			return "", fmt.Errorf("private or special address refused")
		}
		return ip.String(), nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(addrs) == 0 {
		return "", fmt.Errorf("cannot resolve %s", host)
	}
	for _, addr := range addrs {
		ip, ok := netip.AddrFromSlice(addr.IP)
		if !ok {
			return "", fmt.Errorf("invalid resolved address")
		}
		ip = ip.Unmap()
		if blocked(ip) {
			return "", fmt.Errorf("host resolves to a private or special address")
		}
	}
	return addrs[0].IP.String(), nil
}

func blocked(ip netip.Addr) bool {
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsLoopback() ||
		ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() {
		return true
	}
	if ip.Is4() {
		b := ip.As4()
		// CGNAT 100.64.0.0/10, IETF protocol assignments 192.0.0.0/24,
		// benchmarking 198.18.0.0/15 and 0.0.0.0/8 are not public destinations.
		if b[0] == 0 {
			return true
		}
		if b[0] == 100 && b[1] >= 64 && b[1] <= 127 {
			return true
		}
		if b[0] == 192 && b[1] == 0 && b[2] == 0 {
			return true
		}
		if b[0] == 198 && (b[1] == 18 || b[1] == 19) {
			return true
		}
	}
	return false
}

func stringValue(v any) string {
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value)
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case json.Number:
		return value.String()
	default:
		return ""
	}
}

func sanitizeHeader(value string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(value)
}
