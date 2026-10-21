#!/bin/sh
# Install the XProbe server binary and a hardened systemd unit.
# Example:
#   sudo sh install-server.sh --binary ./probe-server --public-url https://monitor.example.com
set -eu

BINARY=""
BINARY_URL=""
PUBLIC_URL="https://monitor.example.com"
LISTEN="127.0.0.1:8080"

while [ $# -gt 0 ]; do
    case "$1" in
        --binary) BINARY="${2:-}"; shift 2 ;;
        --binary-url) BINARY_URL="${2:-}"; shift 2 ;;
        --public-url) PUBLIC_URL="${2:-}"; shift 2 ;;
        --listen) LISTEN="${2:-}"; shift 2 ;;
        *) echo "unknown argument: $1" >&2; exit 2 ;;
    esac
done

[ "$(id -u)" = 0 ] || { echo "please run as root" >&2; exit 1; }
if [ -z "$BINARY" ] && [ -z "$BINARY_URL" ]; then
    echo "--binary or --binary-url is required" >&2
    exit 1
fi
case "$PUBLIC_URL$LISTEN" in
    *[[:space:]]*|*\"*|*\'*|*\\*)
        echo "public URL or listen address contains unsafe characters" >&2; exit 2 ;;
esac

if ! grep -q '^probe:' /etc/group; then
    if command -v groupadd >/dev/null 2>&1; then
        groupadd --system probe
    else
        addgroup -S probe
    fi
fi
if ! id probe >/dev/null 2>&1; then
    if command -v useradd >/dev/null 2>&1; then
        useradd --system --gid probe --no-create-home --shell /usr/sbin/nologin probe
    else
        adduser -S -D -H -G probe -s /sbin/nologin probe
    fi
fi

install -d -o probe -g probe -m 0750 /opt/xprobe /var/lib/xprobe
install -d -o root -g probe -m 0750 /etc/xprobe

fetch() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$1" -o "$2"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$2" "$1"
    else
        echo "curl or wget is required" >&2
        exit 1
    fi
}

TMP=""
SIG=""
if [ -n "$BINARY_URL" ]; then
    case "$BINARY_URL" in
        *@*) echo "--binary-url must not contain userinfo" >&2; exit 1 ;;
    esac
    is_loopback_authority() {
        authority="$1"
        case "$authority" in
            localhost|127.0.0.1|\[::1\]) return 0 ;;
            localhost:*|127.0.0.1:*|\[::1\]:*)
                port="${authority##*:}"
                case "$port" in ''|*[!0-9]*) return 1 ;; *) return 0 ;; esac ;;
            *) return 1 ;;
        esac
    }
    case "$BINARY_URL" in
        https://?*) ;;
        http://*)
            authority="${BINARY_URL#http://}"
            authority="${authority%%/*}"
            is_loopback_authority "$authority"                 || { echo "--binary-url must use https (plain http is only allowed for a loopback host)" >&2; exit 1; } ;;
        *)
            echo "--binary-url must use https (plain http is only allowed for a loopback host)" >&2
            exit 1 ;;
    esac
    TMP=$(mktemp)
    SIG="$TMP.sig"
    trap 'rm -f "$TMP" "$SIG"' EXIT
    fetch "$BINARY_URL" "$TMP"
    if [ -n "${PROBE_RELEASE_PUBKEY:-}" ]; then
        command -v openssl >/dev/null 2>&1 \
            || { echo "PROBE_RELEASE_PUBKEY is set but openssl is missing" >&2; exit 1; }
        fetch "${BINARY_URL}.sig" "$SIG" \
            || { echo "release signature is missing for $BINARY_URL" >&2; exit 1; }
        openssl pkeyutl -verify -pubin -inkey "$PROBE_RELEASE_PUBKEY" \
            -rawin -in "$TMP" -sigfile "$SIG" >/dev/null 2>&1 \
            || { echo "release signature verification failed" >&2; exit 1; }
        echo "release signature verified"
    else
        echo "WARN: PROBE_RELEASE_PUBKEY is not set; --binary-url is not signature-verified" >&2
    fi
    BINARY="$TMP"
fi

install -m 0755 "$BINARY" /opt/xprobe/probe-server

if [ ! -f /etc/xprobe/config.yaml ]; then
    cat > /etc/xprobe/config.yaml <<EOF
listen: "${LISTEN}"
public_url: "${PUBLIC_URL}"
data_dir: "/var/lib/xprobe"
tls:
  mode: "off"
proxy:
  mode: "none"
auth:
  session_idle: "2h"
  session_max: "168h"
  login_rate_limit: 5
monitor:
  report_interval: 3
  heartbeat_timeout: 90
  aggregation_interval: 300
storage:
  data_retention_days: 90
  sqlite_busy_timeout_ms: 5000
log:
  level: "info"
EOF
    chown root:probe /etc/xprobe/config.yaml
    chmod 0640 /etc/xprobe/config.yaml
    echo "created /etc/xprobe/config.yaml; edit it before exposing the server"
fi

cat > /etc/systemd/system/xprobe.service <<'EOF'
[Unit]
Description=XProbe server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=probe
Group=probe
ExecStart=/opt/xprobe/probe-server -config /etc/xprobe/config.yaml
Restart=always
RestartSec=5
NoNewPrivileges=true
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_BIND_SERVICE
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
RestrictSUIDSGID=true
MemoryMax=256M
ReadWritePaths=/var/lib/xprobe

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now xprobe
systemctl --no-pager --lines=8 status xprobe || true
echo "XProbe installed. Edit /etc/xprobe/config.yaml, then: systemctl restart xprobe"
echo "Place signed agent binaries under /var/lib/xprobe/agent-dist/{amd64,arm64}/"
