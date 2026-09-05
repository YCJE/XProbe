// XProbe Server 入口(M3)。
// 启动流程: 配置 → SQLite 迁移 → 种子 → TLS(S2) → JWT 密钥引导 → HTTP/WS 服务 + 前端 embed。
package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	xprobe "github.com/YCJE/XProbe/server"

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

	// 子命令: reset-password(管理员密码找回, 设计文档 7.3.1)
	if len(os.Args) > 1 && os.Args[1] == "reset-password" {
		runResetPassword(os.Args[2:])
		return
	}

	flag.Parse()

	if *showVersion {
		// 输出到 stdout(升级脚本解析版本号用; log 会混入时间戳前缀到 stderr)
		fmt.Println(version.Version)
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
	records := repository.NewRecordRepo(db)
	registerCodes := repository.NewRegisterCodeRepo(db)
	admins := repository.NewAdminRepo(db)
	sessions := repository.NewSessionRepo(db)
	tags := repository.NewTagRepo(db)
	alerts := repository.NewAlertRepo(db)
	notifyChannels := repository.NewNotifyChannelRepo(db)
	shareRepo := repository.NewSharePageRepo(db)
	services := repository.NewServiceRepo(db)
	if err := pingTargets.EnsureSeedDefaults(context.Background(), time.Now().Unix()); err != nil {
		log.Fatalf("seed ping targets: %v", err)
	}

	// TLS(S2): 配置证书或自签兜底持久化, 指纹经 /api/v1/server-cert 公开
	certFingerprint, err := bootstrapTLS(cfg)
	if err != nil {
		log.Fatalf("tls bootstrap: %v", err)
	}
	log.Printf("tls fingerprint (sha256): %s", certFingerprint)
	// 证书热加载: TLS 握手经 reloader 取证书(按 mtime 缓存), certbot 续期后自动生效无需重启
	reloader := pkg.NewCertReloader(certPath(cfg), keyPath(cfg))

	// JWT 密钥: env > 配置文件 > 首次生成持久化(设计文档 8.2)
	secret, err := jwtSecret(cfg)
	if err != nil {
		log.Fatalf("jwt secret: %v", err)
	}
	jwtMgr := pkg.NewJWTManager(secret,
		time.Duration(cfg.Auth.JWTExpiryMin)*time.Minute, *cfg.Auth.CookieSecure)

	// 业务层
	registry := service.NewRegistry(agents, registerCodes)
	hub := service.NewHub(agents, time.Duration(cfg.Monitor.HeartbeatTimeout)*time.Second)
	hub.SetOnOffline(func(agentID int64) {
		log.Printf("[alert] agent=%d went offline", agentID) // M5: 告警状态机接线点
	})
	authSvc := service.NewAuth(admins, sessions, jwtMgr, pkg.NewLimiter(cfg.Security.RegisterRateLimit, time.Minute))
	notifier := service.NewNotifier(notifyChannels)
	serviceChecker := service.NewServiceChecker(services, notifier)

	registerLimiter := pkg.NewLimiter(cfg.Security.RegisterRateLimit, time.Minute)
	loginLimiter := pkg.NewLimiter(cfg.Security.RegisterRateLimit, time.Minute)
	globalLimiter := pkg.NewLimiter(cfg.Security.GlobalRateLimit, time.Minute)
	downloadLimiter := pkg.NewLimiter(10, time.Minute)

	router := api.NewRouter(api.Deps{
		Registry:        registry,
		Hub:             hub,
		Agents:          agents,
		PingTargets:     pingTargets,
		Records:         records,
		Auth:            authSvc,
		JWT:             jwtMgr,
		Sessions:        sessions,
		Tags:            tags,
		Alerts:          alerts,
		NotifyChannels:  notifyChannels,
		Notifier:        notifier,
		Share:           shareRepo,
		Services:        services,
		Checker:         serviceChecker,
		RegisterLimiter: registerLimiter,
		LoginLimiter:    loginLimiter,
		DownloadLimiter: downloadLimiter,
		GlobalLimiter:   globalLimiter,
		CertFingerprint: certFingerprint,
		CertReloader:    reloader,
		WSCompression:   *cfg.Monitor.WSCompression,
	}, mustWebFS())

	// 后台任务: 心跳兜底巡检 + 实时数据聚合落盘(5min/日, 设计文档 5.3) + 告警引擎 + 会话/注册码清理
	sweepCtx, stopSweep := context.WithCancel(context.Background())
	defer stopSweep()
	go hub.RunSweeper(sweepCtx, 15*time.Second)
	for _, l := range []*pkg.Limiter{registerLimiter, loginLimiter, globalLimiter, downloadLimiter} {
		go l.StartGC(sweepCtx, time.Minute)
	}
	aggregator := service.NewAggregator(hub, records, agents)
	go aggregator.Run(sweepCtx, 5*time.Minute)
	go serviceChecker.Run(sweepCtx)
	alertEngine := service.NewAlertEngine(alerts, agents, hub, notifier)
	if err := alertEngine.Restore(sweepCtx); err != nil {
		log.Printf("[alert] restore: %v", err)
	}
	go alertEngine.Run(sweepCtx, 30*time.Second)
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-sweepCtx.Done():
				return
			case <-t.C:
				_, _ = sessions.DeleteExpired(sweepCtx, time.Now().Unix())
				_, _ = registerCodes.DeleteExpired(sweepCtx, time.Now().Unix())
			}
		}
	}()

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion:     tls.VersionTLS12, // S2: 拒绝老旧协议
			GetCertificate: reloader.GetCertificate,
		},
	}
	errCh := make(chan error, 1)
	go func() {
		log.Printf("xprobe-server %s listening on %s (https/wss)", version.Version, cfg.Listen)
		errCh <- srv.ListenAndServeTLS("", "") // 证书由 GetCertificate 提供
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

func mustWebFS() (f fs.FS) {
	f, err := xprobe.Web()
	if err != nil {
		log.Printf("frontend assets not embedded (%v); API-only mode", err)
		return nil
	}
	return f
}

// jwtSecret 决定 JWT 签名密钥: PROBE_JWT_SECRET env > 配置文件 > 数据目录持久化随机值。
func jwtSecret(cfg *config.Config) (string, error) {
	if v := os.Getenv("PROBE_JWT_SECRET"); v != "" {
		return v, nil
	}
	if cfg.Auth.JWTSecret != "" {
		return cfg.Auth.JWTSecret, nil
	}
	path := filepath.Join(cfg.DataDir, "jwt_secret")
	if b, err := os.ReadFile(path); err == nil {
		if s := strings.TrimSpace(string(b)); len(s) >= 32 {
			return s, nil
		}
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(b)
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		return "", err
	}
	log.Printf("generated new jwt secret at %s", path)
	return secret, nil
}

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
