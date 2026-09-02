GO ?= go
VERSION ?= dev
LDFLAGS := -X github.com/YCJE/XProbe/internal/version.Version=$(VERSION)

.PHONY: all build-server build-agent build-linux test vet cover fmt audit-noexec clean

all: build-server build-agent

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
release: build-linux
	mkdir -p bin/release
	cp bin/linux-*/xprobe-* bin/release/
	cd bin/release && for f in xprobe-*; do sha256sum $$f > $$f.sha256; done
	@echo "release artifacts in bin/release/"

clean:
	rm -rf bin cover.out
