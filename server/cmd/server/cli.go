package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/YCJE/XProbe/server/internal/config"
	"github.com/YCJE/XProbe/server/internal/pkg"
	"github.com/YCJE/XProbe/server/internal/repository"
	"github.com/YCJE/XProbe/server/internal/service"
)

// reset-password 管理员密码找回(设计文档 7.3.1): 重置密码并吊销全部会话。
// 用法: xprobe-server reset-password --username admin [--config /etc/probe-server/config.yml]
func runResetPassword(args []string) {
	log.SetFlags(log.LstdFlags | log.LUTC)
	fs := flag.NewFlagSet("reset-password", flag.ExitOnError)
	username := fs.String("username", "admin", "管理员用户名")
	configPath := fs.String("config", config.DefaultPath, "配置文件路径")
	dataDir := fs.String("data-dir", "", "数据目录(覆盖配置文件)")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Fatalf("load config: %v", err)
		}
		cfg = config.Defaults()
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}

	db, err := repository.Open(cfg.DataDir + "/xprobe.db")
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := repository.Migrate(context.Background(), db); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	fmt.Print("新密码(≥12 位, 含大小写与数字): ")
	pw1, err := readPassword()
	if err != nil {
		log.Fatalf("read password: %v", err)
	}
	fmt.Print("确认新密码: ")
	pw2, err := readPassword()
	if err != nil {
		log.Fatalf("read password: %v", err)
	}
	if pw1 != pw2 {
		log.Fatal("两次输入不一致")
	}

	authSvc := service.NewAuth(
		repository.NewAdminRepo(db),
		repository.NewSessionRepo(db),
		pkg.NewJWTManager("unused-secret-reset", time.Hour, false),
		nil,
	)
	if err := authSvc.ResetPassword(context.Background(), *username, pw1); err != nil {
		log.Fatalf("reset failed: %v", err)
	}
	log.Printf("密码已重置, 该用户全部会话已吊销")
}

func readPassword() (string, error) {
	// 优先使用终端静默读取; 非终端(管道)回退行读取
	if term.IsTerminal(int(syscall.Stdin)) {
		b, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
