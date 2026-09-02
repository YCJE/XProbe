package service

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/YCJE/XProbe/internal/model"
	"github.com/YCJE/XProbe/server/internal/pkg"
	"github.com/YCJE/XProbe/server/internal/repository"
)

// crlf SMTP 行分隔(用字节常量避免源码反斜杠转义)。
var crlf = string([]byte{0x0d, 0x0a})

// Notifier 通知发送(SSRF 防护, 设计文档 5.5/7.4)。
type Notifier struct {
	channels *repository.NotifyChannelRepo
	client   *pkg.SafeClient
}

func NewNotifier(channels *repository.NotifyChannelRepo) *Notifier {
	return &Notifier{channels: channels, client: pkg.NewSafeClient(10 * time.Second)}
}

// Send 通过指定渠道发送通知; 发送失败仅记录, 不阻塞告警引擎。
func (n *Notifier) Send(ctx context.Context, channelID int64, title, body string) error {
	if channelID <= 0 {
		return nil
	}
	ch, err := n.channels.Get(ctx, channelID)
	if err != nil {
		return fmt.Errorf("channel %d: %w", channelID, err)
	}
	switch ch.Type {
	case "webhook":
		return n.sendWebhook(ctx, ch, title, body)
	case "telegram":
		return n.sendTelegram(ctx, ch, title, body)
	case "smtp":
		return n.sendSMTP(ctx, ch, title, body)
	default:
		return fmt.Errorf("unknown channel type %q", ch.Type)
	}
}

func (n *Notifier) sendWebhook(ctx context.Context, ch *model.NotifyChannel, title, body string) error {
	url, _ := ch.Config["url"].(string)
	payload, _ := json.Marshal(map[string]string{"title": title, "body": body, "source": "xprobe"})
	return n.client.PostJSON(ctx, url, payload)
}

func (n *Notifier) sendTelegram(ctx context.Context, ch *model.NotifyChannel, title, body string) error {
	botToken, _ := ch.Config["bot_token"].(string)
	chatID, _ := ch.Config["chat_id"].(string)
	if botToken == "" || chatID == "" {
		return fmt.Errorf("telegram: bot_token/chat_id required")
	}
	// 固定 api.telegram.org, 不接受自定义域名(设计文档 5.5)
	url := "https://api.telegram.org/bot" + botToken + "/sendMessage"
	payload, _ := json.Marshal(map[string]string{
		"chat_id": chatID, "text": title + "\n" + body,
	})
	return n.client.PostJSON(ctx, url, payload)
}

func (n *Notifier) sendSMTP(ctx context.Context, ch *model.NotifyChannel, title, body string) error {
	host, _ := ch.Config["host"].(string)
	portF, _ := ch.Config["port"].(float64)
	user, _ := ch.Config["username"].(string)
	password, _ := ch.Config["password"].(string)
	from, _ := ch.Config["from"].(string)
	toList, _ := ch.Config["to"].([]any)
	if host == "" || from == "" || len(toList) == 0 {
		return fmt.Errorf("smtp: host/from/to required")
	}
	port := int(portF)
	if port == 0 {
		port = 25
	}
	to := make([]string, 0, len(toList))
	for _, t := range toList {
		if s, ok := t.(string); ok {
			to = append(to, s)
		}
	}
	msg := strings.Join([]string{
		"From: " + from,
		"To: " + strings.Join(to, ", "),
		"Subject: " + title,
		"Content-Type: text/plain; charset=UTF-8",
		"", body,
	}, crlf)

	// 复用 SafeDialContext: 解析→内网校验→直连同一 IP, 消除预检/实连两次解析的 DNS 重绑定 TOCTOU(设计文档 7.4)。
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	done := make(chan error, 1)
	go func() {
		done <- smtpSend(addr, host, user, password, from, to, []byte(msg))
	}()
	select {
	case err := <-done:
		if err != nil {
			log.Printf("[notify] smtp send failed: %v", err)
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// smtpSend 用 SSRF 防护拨号器建立连接, 再走 smtp 协议(取代 smtp.SendMail 的默认拨号, 防 DNS 重绑定)。
func smtpSend(addr, host, user, password, from string, to []string, msg []byte) error {
	conn, err := pkg.SafeDialContext(context.Background(), "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	// I/O deadline: 防半开 SMTP 服务器泄漏 goroutine(审查 MEDIUM #7)
	_ = conn.SetDeadline(time.Now().Add(60 * time.Second))
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Quit()
	// 服务器支持 STARTTLS 时升级(587 端口认证场景)
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return fmt.Errorf("smtp: starttls: %w", err)
		}
	}
	if user != "" {
		if err := c.Auth(smtp.PlainAuth("", user, password, host)); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	return w.Close()
}
