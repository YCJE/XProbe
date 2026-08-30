// XProbe Server 入口。
// M1 范围: 初始化 SQLite(WAL)并执行迁移, 验证数据层就绪;
// M2 接入 HTTP/WebSocket(API 层)、Agent 注册与认证。
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/YCJE/XProbe/internal/version"
	"github.com/YCJE/XProbe/server/internal/repository"
)

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)

	dataDir := flag.String("data-dir", "/var/lib/probe-server", "数据目录(存 SQLite)")
	showVersion := flag.Bool("version", false, "打印版本号后退出")
	flag.Parse()

	if *showVersion {
		log.Printf("xprobe-server %s", version.Version)
		return
	}

	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}
	db, err := repository.Open(filepath.Join(*dataDir, "xprobe.db"))
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	if err := repository.Migrate(context.Background(), db); err != nil {
		log.Fatalf("migrate database: %v", err)
	}
	log.Printf("xprobe-server %s: database ready at %s", version.Version, *dataDir)

	// M2: 启动 HTTP/WebSocket 服务。当前等待信号退出。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	log.Printf("shutting down")
}
