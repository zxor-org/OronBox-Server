#!/usr/bin/env sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
mkdir -p "$root/bin"
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o "$root/bin/oronbox-server" ./cmd/server
echo "built $root/bin/oronbox-server"
