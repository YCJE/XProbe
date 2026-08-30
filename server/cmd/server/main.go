// XProbe Server 入口(M2)。
// 启动流程: 配置 → SQLite 迁移 → 种子探测目标 → TLS(自签兜底) → HTTP/WS 服务。
// S2 强制 TLS: 仅以 ListenAndServeTLS 提供 wss/https, 不存在明文回退。
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/YCJE/XProbe/internal/version"
	"github.com/YCJE/XProbe/server/internal/api"
	"github.com/YCJE/XProbe/server/internal/config"
	"github.com/YCJE/XProbe/server/internal/pkg"
	"github.com/YCJE/XProbe/server/internal/repository"
	"github.com/YCJE/XProbe/server/internal/service"
)

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)

	configPath := flag.String("config", config.DefaultPath, "配置文件路径")
	dataDir := flag.String("data-dir", "", "数据目录(覆盖配置文件)")
	showVersion := flag.Bool("version", false, "打印版本号后退出")
	flag.Parse()

	if *showVersion {
		log.Printf("xprobe-server %s", version.Version)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Printf("config %s not found, using defaults", *configPath)
			cfg = config.Defaults()
		} else {
			log.Fatalf("load config: %v", err)
		}
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	// 数据层
	db, err := repository.Open(filepath.Join(cfg.DataDir, "xprobe.db"))
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := repository.Migrate(context.Background(), db); err != nil {
		log.Fatalf("migrate database: %v", err)
	}
	agents := repository.NewAgentRepo(db)
	pingTargets := repository.NewPingTargetRepo(db)
	if err := pingTargets.EnsureSeedDefaults(context.Background(), time.Now().Unix()); err != nil {
		log.Fatalf("seed ping targets: %v", err)
	}

	// 业务层
	registry := service.NewRegistry(agents, repository.NewRegisterCodeRepo(db))
	hub := service.NewHub(agents, time.Duration(cfg.Monitor.HeartbeatTimeout)*time.Second)
	hub.SetOnOffline(func(agentID int64) {
		log.Printf("[alert] agent=%d went offline", agentID) // M5: 告警状态机接线点
	})

	// TLS(S2): 配置证书或首次生成自签并持久化, 指纹经 /api/v1/server-cert 公开
	certFingerprint, err := bootstrapTLS(cfg)
	if err != nil {
		log.Fatalf("tls bootstrap: %v", err)
	}
	log.Printf("tls fingerprint (sha256): %s", certFingerprint)

	router := api.NewRouter(api.Deps{
		Registry:        registry,
		Hub:             hub,
		Agents:          agents,
		PingTargets:     pingTargets,
		CertFingerprint: certFingerprint,
		RegisterLimiter: pkg.NewLimiter(cfg.Security.RegisterRateLimit, time.Minute),
		GlobalLimiter:   pkg.NewLimiter(cfg.Security.GlobalRateLimit, time.Minute),
		WSCompression:   *cfg.Monitor.WSCompression,
	})

	// 心跳超时兜底巡检
	sweepCtx, stopSweep := context.WithCancel(context.Background())
	defer stopSweep()
	go hub.RunSweeper(sweepCtx, 15*time.Second)

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12}, // S2: 拒绝老旧协议
	}
	errCh := make(chan error, 1)
	go func() {
		log.Printf("xprobe-server %s listening on %s (https/wss)", version.Version, cfg.Listen)
		errCh <- srv.ListenAndServeTLS(certPath(cfg), keyPath(cfg))
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-errCh:
		log.Fatalf("server exited: %v", err)
	case <-ctx.Done():
		log.Printf("shutting down")
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}
}

func certPath(cfg *config.Config) string { return cfg.TLS.Cert }
func keyPath(cfg *config.Config) string  { return cfg.TLS.Key }

// bootstrapTLS 决定证书来源并返回指纹。
// 未配置证书时: 在数据目录生成自签证书(首次)并复用, 保证重启后指纹不变。
func bootstrapTLS(cfg *config.Config) (string, error) {
	if cfg.TLS.Cert != "" && cfg.TLS.Key != "" {
		certPEM, err := os.ReadFile(cfg.TLS.Cert)
		if err != nil {
			return "", fmt.Errorf("read cert: %w", err)
		}
		return pkg.LoadCertFingerprint(certPEM)
	}

	certFile := filepath.Join(cfg.DataDir, "cert.pem")
	keyFile := filepath.Join(cfg.DataDir, "key.pem")
	if _, err := os.Stat(certFile); errors.Is(err, os.ErrNotExist) {
		log.Printf("no TLS cert configured, generating self-signed certificate (S2)")
		certPEM, keyPEM, _, err := pkg.GenerateSelfSigned([]string{"localhost"})
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
			return "", err
		}
		if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
			return "", err
		}
	}
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return "", err
	}
	cfg.TLS.Cert, cfg.TLS.Key = certFile, keyFile
	return pkg.LoadCertFingerprint(certPEM)
}
