package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/YCJE/XProbe/internal/model"
	"github.com/YCJE/XProbe/server/internal/pkg"
	"github.com/YCJE/XProbe/server/internal/repository"
)

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
	user, _ := ch.Config["username"].(string)
	password, _ := ch.Config["password"].(string)
	from, _ := ch.Config["from"].(string)
	toList, _ := ch.Config["to"].([]any)
	if host == "" || from == "" || len(toList) == 0 {
		return fmt.Errorf("smtp: host/from/to required")
	}
	// SMTP 无重定向概念, 但主机解析与连接同样走 SSRF 内网检查(设计文档 7.4)
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
	}, "\r\n")
	var auth smtp.Auth
	if user != "" {
		auth = smtp.PlainAuth("", user, password, host)
	}
	done := make(chan error, 1)
	go func() {
		// smtp.SendMail 内部解析 host; 发送前预检内网地址
		if err := precheckHost(host); err != nil {
			done <- err
			return
		}
		done <- smtp.SendMail(host, auth, from, to, []byte(msg))
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

// precheckHost SMTP 主机名预检(逐 IP 内网过滤)。
func precheckHost(hostPort string) error {
	host := hostPort
	if i := strings.LastIndex(hostPort, ":"); i > 0 {
		host = hostPort[:i]
	}
	if ip := net.ParseIP(host); ip != nil {
		if pkg.IsPrivateIP(ip) {
			return fmt.Errorf("%w: %s", pkg.ErrSSRFBlocked, ip)
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if pkg.IsPrivateIP(ip) {
			return fmt.Errorf("%w: %s", pkg.ErrSSRFBlocked, ip)
		}
	}
	return nil
}
