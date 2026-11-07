#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$ROOT"
case "$(uname -s):$(uname -m)" in
    Linux:x86_64|Linux:amd64) SERVER="$ROOT/kairi-server-linux-amd64" ;;
    Linux:aarch64|Linux:arm64) SERVER="$ROOT/kairi-server-linux-arm64" ;;
    *) echo 'CDN sync supports Linux x64 and ARM64.' >&2; exit 2 ;;
esac
if [ "$#" -gt 1 ]; then echo 'Usage: sh Sync-CDN.sh [--dry-run]' >&2; exit 2; fi
case "${1:-}" in
    '') set -- -sync-cdn cdn-sync.json ;;
    --dry-run) set -- -sync-cdn cdn-sync.json -cdn-sync-dry-run ;;
    *) echo 'Usage: sh Sync-CDN.sh [--dry-run]' >&2; exit 2 ;;
esac
if [ ! -f "$SERVER" ]; then echo 'Extract the complete server package first.' >&2; exit 1; fi
if [ ! -x "$SERVER" ]; then chmod u+x "$SERVER"; fi
exec "$SERVER" "$@" -platform-content-config "$ROOT/deployment.json"
