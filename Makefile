GO ?= go
VERSION ?= dev
LDFLAGS := -X github.com/YCJE/XProbe/internal/version.Version=$(VERSION)

.PHONY: all build-server build-agent build-linux test vet cover fmt audit-noexec clean

all: build-server build-agent

build-frontend:
	cd frontend && npm ci --no-audit --no-fund && npm run build
	mkdir -p server/web
	cp -r frontend/dist/. server/web/

build-server:
	CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o bin/xprobe-server ./server/cmd/server

build-agent:
	CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o bin/xprobe-agent ./agent/cmd/agent

# v1 发布矩阵(设计文档 8.4): Server amd64/arm64, Agent amd64/arm64/armv7, 全程无 cgo
build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o bin/linux-amd64/xprobe-server ./server/cmd/server
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -ldflags "$(LDFLAGS)" -o bin/linux-arm64/xprobe-server ./server/cmd/server
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o bin/linux-amd64/xprobe-agent ./agent/cmd/agent
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -ldflags "$(LDFLAGS)" -o bin/linux-arm64/xprobe-agent ./agent/cmd/agent
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 $(GO) build -ldflags "$(LDFLAGS)" -o bin/linux-armv7/xprobe-agent ./agent/cmd/agent

test:
	$(GO) test -timeout 300s ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

cover:
	$(GO) test -coverprofile=cover.out ./... && $(GO) tool cover -func=cover.out

# S4 审计门禁(设计文档 7.2/7.8): Agent 代码零命令执行符号
audit-noexec:
	bash scripts/audit_noexec.sh

# 本地出全矩阵发布物(与 CI 同口径): 复制 build-linux 产物 + SHA256
release: build-frontend build-linux
	@for A in amd64 arm64 armv7; do 	  mkdir -p server/assets/agents/linux-$$A; 	  if [ -f bin/linux-$$A/xprobe-agent ]; then cp bin/linux-$$A/xprobe-agent server/assets/agents/linux-$$A/; fi; 	done
	@if [ ! -f server/assets/agents/linux-amd64/xprobe-agent ]; then echo "缺少 Agent 二进制(先跑 build-linux)"; exit 1; fi
	mkdir -p server/web
	cp -r frontend/dist/. server/web/
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/release/xprobe-server-linux-amd64 ./server/cmd/server
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/release/xprobe-server-linux-arm64 ./server/cmd/server
	mkdir -p bin/release
	cp bin/linux-armv7/xprobe-agent bin/release/ 2>/dev/null || true
	cd bin/release && for f in xprobe-*; do sha256sum $$f > $$f.sha256; done
	@echo "release artifacts in bin/release/"

clean:
	rm -rf bin cover.out
