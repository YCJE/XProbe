#!/bin/sh
# Cross-platform build entry: build the embedded UI, then both Go binaries.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
cd "$ROOT"

(cd server/web && npm ci && npm run build)
mkdir -p bin
(cd server && go build -o ../bin/probe-server ./cmd/server)
(cd agent && go build -o ../bin/probe-agent ./cmd/agent)
echo "built bin/probe-server and bin/probe-agent"
