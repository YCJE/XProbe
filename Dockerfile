# syntax=docker/dockerfile:1
# XProbe 多阶段构建(设计文档 10.8 M6):
# 前端构建 → Agent 三架构构建 → Server(内嵌前端 + Agent 二进制) → 精简运行时。

FROM node:20-alpine AS frontend
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

FROM golang:1.26-alpine AS agent
WORKDIR /src
COPY go.mod go.sum ./
COPY agent/ ./agent/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o out/agents/linux-amd64/xprobe-agent ./agent/cmd/agent \
 && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o out/agents/linux-arm64/xprobe-agent ./agent/cmd/agent \
 && CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -o out/agents/linux-armv7/xprobe-agent ./agent/cmd/agent \
 && cd out/agents && for d in */; do (cd "$d" && sha256sum xprobe-agent > xprobe-agent.sha256); done

FROM golang:1.26-alpine AS server
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
COPY internal/ ./internal/
COPY server/ ./server/
COPY --from=frontend /app/frontend/dist ./server/web
COPY --from=agent /src/out/agents ./server/assets/agents
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "-s -w -X github.com/YCJE/XProbe/internal/version.Version=${VERSION}" \
    -o out/xprobe-server ./server/cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates sqlite \
 && adduser -D -H -u 10000 probe
COPY --from=server /src/out/xprobe-server /usr/local/bin/xprobe-server
USER probe
VOLUME ["/data"]
EXPOSE 443
ENTRYPOINT ["/usr/local/bin/xprobe-server", "--data-dir", "/data"]
