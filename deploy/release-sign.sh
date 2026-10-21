#!/bin/sh
# Sign release binaries with Ed25519 and write .sha256 + .sig sidecars.
# Keep the private key offline; publish only release-public.pem.
#   PROBE_RELEASE_KEY=/secure/probe-release.pem ./release-sign.sh \
#     data/agent-dist/amd64/probe-agent data/agent-dist/arm64/probe-agent
set -eu

KEY="${PROBE_RELEASE_KEY:-release-private.pem}"
PUB="${PROBE_RELEASE_PUB:-$(dirname "$KEY")/release-public.pem}"

if [ ! -f "$KEY" ]; then
    openssl genpkey -algorithm ED25519 -out "$KEY"
    chmod 600 "$KEY"
    echo "generated $KEY (keep it offline)"
fi
openssl pkey -in "$KEY" -pubout -out "$PUB"

for FILE in "$@"; do
    [ -f "$FILE" ] || { echo "missing $FILE" >&2; exit 1; }
    if command -v sha256sum >/dev/null 2>&1; then
        (cd "$(dirname "$FILE")" && sha256sum "$(basename "$FILE")") > "$FILE.sha256"
    else
        (cd "$(dirname "$FILE")" && shasum -a 256 "$(basename "$FILE")") > "$FILE.sha256"
    fi
    openssl pkeyutl -sign -inkey "$KEY" -rawin -in "$FILE" -out "$FILE.sig"
    echo "signed $FILE"
done

echo "public key: $PUB"
echo "On agents: export PROBE_RELEASE_PUBKEY=/etc/probe-agent/release-public.pem"
