# Build the embedded web UI first, then the Go binary.
FROM node:24-alpine AS web
WORKDIR /src/server/web
COPY server/web/package.json server/web/package-lock.json ./
RUN npm ci
COPY server/web ./
RUN npm run build

FROM golang:1.26.9-alpine AS build
RUN apk add --no-cache ca-certificates
WORKDIR /src
COPY go.work go.work.sum ./
COPY common ./common
COPY agent ./agent
COPY server ./server
COPY --from=web /src/server/internal/webui/dist ./server/internal/webui/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/probe-server ./server/cmd/server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 10001 -S probe \
    && adduser -u 10001 -S -G probe -h /var/lib/probe probe \
    && mkdir -p /var/lib/probe /etc/probe \
    && chown -R 10001:10001 /var/lib/probe /etc/probe
COPY --from=build /out/probe-server /usr/local/bin/probe-server
USER 10001:10001
WORKDIR /var/lib/probe
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/probe-server", "-config", "/etc/probe/config.yaml"]
