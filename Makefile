.PHONY: web server agent build test fmt vet

web:
	cd server/web && npm ci && npm run build

server: web
	mkdir -p bin
	cd server && go build -o ../bin/probe-server ./cmd/server

agent:
	mkdir -p bin
	cd agent && go build -o ../bin/probe-agent ./cmd/agent

build: server agent

test:
	cd common && go test ./...
	cd server && go test ./...
	cd agent && go test ./...

fmt:
	gofmt -w common agent server

vet:
	cd common && go vet ./...
	cd server && go vet ./...
	cd agent && go vet ./...
