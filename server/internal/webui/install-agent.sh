#!/bin/sh
# XProbe agent installer. Run as root:
#   curl -fsSL https://monitor.example.com/install-agent.sh | sudo sh -s -- \
#     --server https://monitor.example.com --code XXXX-XXXX-XXXX-XXXX-XXXX-XXXX-XX
set -eu

SERVER=""
CODE=""
CERT_FP=""
INTERVAL="3"

while [ $# -gt 0 ]; do
    case "$1" in
        --server) SERVER="${2:-}"; shift 2 ;;
        --code) CODE="${2:-}"; shift 2 ;;
        --cert-fingerprint) CERT_FP="${2:-}"; shift 2 ;;
        --interval) INTERVAL="${2:-3}"; shift 2 ;;
        *) echo "unknown argument: $1" >&2; exit 2 ;;
    esac
done

[ "$(id -u)" = 0 ] || { echo "please run as root" >&2; exit 1; }
[ -n "$SERVER" ] || { echo "--server is required" >&2; exit 1; }
[ -n "$CODE" ] || { echo "--code is required (create one in the panel)" >&2; exit 1; }
case "$INTERVAL" in
    ''|*[!0-9]*) echo "--interval must be a number" >&2; exit 2 ;;
esac
[ "$INTERVAL" -ge 1 ] && [ "$INTERVAL" -le 60 ] \
    || { echo "--interval must be between 1 and 60 seconds" >&2; exit 2; }
case "$CODE" in
    *[!A-Za-z0-9-]*) echo "--code contains invalid characters" >&2; exit 2 ;;
esac
case "$SERVER" in
    *[[:space:]]*|*\"*|*\'*|*\\*|*@*)
        echo "--server contains unsafe characters" >&2; exit 2 ;;
esac
SERVER="${SERVER%/}"

# Match the host, not just a prefix: http://localhost.evil.example must not be
# treated as a loopback development URL. Ports must be decimal digits.
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
case "$SERVER" in
    https://?*) ;;
    http://*)
        authority="${SERVER#http://}"
        authority="${authority%%/*}"
        is_loopback_authority "$authority"             || { echo "server must use https (plain http is only allowed for a real loopback host)" >&2; exit 1; } ;;
    *) echo "server must use https (plain http is only allowed for a real loopback host)" >&2; exit 1 ;;
esac

if [ -n "$CERT_FP" ]; then
    CERT_FP=$(printf '%s' "$CERT_FP" | tr -d ':' | tr 'A-F' 'a-f')
    case "$CERT_FP" in
        ''|*[!0-9a-f]*) echo "--cert-fingerprint must be a 64-character hex SPKI hash" >&2; exit 2 ;;
    esac
    [ "${#CERT_FP}" -eq 64 ] \
        || { echo "--cert-fingerprint must be a 64-character hex SPKI hash" >&2; exit 2; }
fi

ARCH=$(uname -m)
case "$ARCH" in
    x86_64|amd64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) echo "unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

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
sha256_check() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum -c "$1"
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 -c "$1"
    else
        echo "sha256sum or shasum is required" >&2
        exit 1
    fi
}

TMP=$(mktemp)
SUM="$TMP.sha256"
SIG="$TMP.sig"
trap 'rm -f "$TMP" "$SUM" "$SIG"' EXIT

BASE="${SERVER}/download/agent/${ARCH}"
echo "downloading probe-agent (${ARCH})"
fetch "${BASE}/probe-agent" "$TMP"
fetch "${BASE}/probe-agent.sha256" "$SUM"
# The checksum file is a digest followed by a name; verify our temp path.
DIGEST=$(awk '{print $1}' "$SUM" | head -n 1)
[ -n "$DIGEST" ] || { echo "empty checksum file" >&2; exit 1; }
printf '%s  %s\n' "$DIGEST" "$TMP" > "$SUM.local"
sha256_check "$SUM.local"
rm -f "$SUM.local"

if [ -n "${PROBE_RELEASE_PUBKEY:-}" ]; then
    command -v openssl >/dev/null 2>&1 \
        || { echo "PROBE_RELEASE_PUBKEY is set but openssl is missing" >&2; exit 1; }
    fetch "${BASE}/probe-agent.sig" "$SIG" \
        || { echo "PROBE_RELEASE_PUBKEY is set but the release signature is missing" >&2; exit 1; }
    openssl pkeyutl -verify -pubin -inkey "$PROBE_RELEASE_PUBKEY" \
        -rawin -in "$TMP" -sigfile "$SIG" >/dev/null 2>&1 \
        || { echo "release signature verification failed" >&2; exit 1; }
    echo "release signature verified"
elif fetch "${BASE}/probe-agent.sig" "$SIG" 2>/dev/null; then
    echo "WARN: signature present but PROBE_RELEASE_PUBKEY is not configured; SHA-256 only" >&2
else
    echo "WARN: no release signature available; SHA-256 only" >&2
fi

[ "$(head -c 4 "$TMP" | od -An -tx1 | tr -d ' \n')" = "7f454c46" ] \
    || { echo "downloaded file is not a Linux ELF binary" >&2; exit 1; }

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

install -m 0755 "$TMP" /usr/local/bin/probe-agent
mkdir -p /etc/probe-agent /var/lib/probe-agent

SALT=$(openssl rand -hex 32 2>/dev/null || head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')
cat > /etc/probe-agent/config.yml <<EOF
server: "${SERVER}"
register_code: "${CODE}"
install_salt: "${SALT}"
state_file: "/var/lib/probe-agent/state.json"
report_interval: ${INTERVAL}
config_sync_interval: 3600
ping_method: "auto"
EOF
if [ -n "$CERT_FP" ]; then
    printf 'tls_pin_mode: "spki_sha256"\nserver_cert_fingerprints: ["%s"]\n' "$CERT_FP" >> /etc/probe-agent/config.yml
else
    printf 'tls_pin_mode: "none"\nserver_cert_fingerprints: []\n' >> /etc/probe-agent/config.yml
fi
chmod 600 /etc/probe-agent/config.yml
chown -R probe:probe /etc/probe-agent /var/lib/probe-agent

# Unprivileged ICMP. Failure is non-fatal: the agent falls back to TCP.
sysctl -w net.ipv4.ping_group_range="0 2147483647" >/dev/null 2>&1 || \
    echo "WARN: cannot set net.ipv4.ping_group_range; ICMP will fall back to TCP" >&2

cat > /etc/systemd/system/probe-agent.service <<'EOF'
[Unit]
Description=XProbe Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=probe
Group=probe
ExecStart=/usr/local/bin/probe-agent --config /etc/probe-agent/config.yml
Restart=always
RestartSec=5
NoNewPrivileges=true
CapabilityBoundingSet=
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_NETLINK AF_UNIX
RestrictSUIDSGID=true
MemoryMax=128M
ReadWritePaths=/etc/probe-agent /var/lib/probe-agent

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable probe-agent >/dev/null 2>&1 || true
systemctl restart probe-agent
sleep 1
systemctl --no-pager --lines=5 status probe-agent || true
echo "probe-agent installed. Check: systemctl status probe-agent"
