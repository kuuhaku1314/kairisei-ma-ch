#!/bin/sh
# Shared Linux entry point. Runtime needs only /bin/sh and coreutils.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$ROOT"
ARCH=${1:-}
case "$ARCH" in amd64|arm64) shift ;; *) echo 'Unsupported server architecture.' >&2; exit 2 ;; esac

usage() {
    echo "Usage: sh Start-Server-linux-$ARCH.sh <server IPv4 or DNS host> [port] [--dry-run]"
    echo 'Example: sh Start-Server-linux-amd64.sh 192.168.2.149 26040'
    echo 'Admin stays on 127.0.0.1 at port + 2. Stop with Ctrl+C or SIGTERM.'
}
if [ "$#" -eq 0 ]; then usage; exit 2; fi
if [ "$1" = '--help' ]; then usage; exit 0; fi
ADVERTISE_HOST=$1
shift
PORT=26040
if [ "$#" -gt 0 ] && [ "$1" != '--dry-run' ]; then PORT=$1; shift; fi
DRY_RUN=false
if [ "$#" -gt 0 ] && [ "$1" = '--dry-run' ]; then DRY_RUN=true; shift; fi
if [ "$#" -ne 0 ]; then usage >&2; exit 2; fi

case "$ADVERTISE_HOST" in
    ''|*[!a-zA-Z0-9.-]*|.*|*.|*..*) echo 'Server address must be an IPv4 address or DNS host, without URL syntax.' >&2; exit 2 ;;
esac
if [ "${#ADVERTISE_HOST}" -gt 253 ]; then echo 'Server host is too long.' >&2; exit 2; fi
OLD_IFS=$IFS
IFS=.
set -- $ADVERTISE_HOST
IFS=$OLD_IFS
case "$ADVERTISE_HOST" in
    *[!0-9.]*)
        for label do
            if [ "${#label}" -gt 63 ]; then echo 'DNS label is too long.' >&2; exit 2; fi
            case "$label" in -*|*-) echo 'DNS labels cannot begin or end with a hyphen.' >&2; exit 2 ;; esac
        done ;;
    *)
        if [ "$#" -ne 4 ]; then echo 'Server address must have four IPv4 octets.' >&2; exit 2; fi
        FIRST_OCTET=$1
        for octet do
            if [ "${#octet}" -gt 3 ] || [ "$octet" -gt 255 ]; then echo 'Invalid IPv4 octet.' >&2; exit 2; fi
            case "$octet" in 0?*) echo 'IPv4 octets must not have leading zeros.' >&2; exit 2 ;; esac
        done
        if [ "$FIRST_OCTET" -eq 0 ] || [ "$FIRST_OCTET" -ge 224 ]; then
            echo 'Server address must be a unicast IPv4 address.' >&2; exit 2
        fi ;;
esac
case "$PORT" in ''|*[!0-9]*|0?*) echo 'Port must be an integer from 1 to 65533.' >&2; exit 2 ;; esac
if [ "${#PORT}" -gt 5 ] || [ "$PORT" -lt 1 ] || [ "$PORT" -gt 65533 ]; then
    echo 'Port must be from 1 to 65533.' >&2; exit 2
fi
BATTLE_PORT=$((PORT + 1))
ADMIN_PORT=$((PORT + 2))

if [ "$(uname -s)" != Linux ]; then echo 'This entry point requires Linux.' >&2; exit 2; fi
case "$ARCH:$(uname -m)" in
    amd64:x86_64|amd64:amd64|arm64:aarch64|arm64:arm64) ;;
    *) echo "Wrong architecture: requested $ARCH, detected $(uname -m)." >&2; exit 2 ;;
esac
SERVER="$ROOT/kairi-server-linux-$ARCH"
if [ ! -f "$SERVER" ] || [ ! -f deployment.json ] || [ ! -f resource-set/resource-set.json ]; then
    echo 'Incomplete package. Extract all release files first.' >&2; exit 1
fi
set -- \
    -resource-set "$ROOT/resource-set/resource-set.json" \
    -platform-content-config "$ROOT/deployment.json" \
    -request-log "$ROOT/_local/runtime/requests.jsonl" \
    -cn-save-path "$ROOT/_local/data/cn602-save.json" \
    -cdn-config "$ROOT/cdn.json" \
    -cdn-sync-config "$ROOT/cdn-sync.json" \
    -listen-host '0.0.0.0' -advertise-host "$ADVERTISE_HOST" \
    -port "$PORT" -battle-port "$BATTLE_PORT" \
    -admin-listen-host '127.0.0.1' -admin-port "$ADMIN_PORT"
if [ "$DRY_RUN" = true ]; then
    echo 'Dry run: no server started and no account data written.'
    printf '%s\n' "$SERVER" "$@"
    exit 0
fi
mkdir -p "$ROOT/_local/data" "$ROOT/_local/runtime"
# The request recorder appends to an existing file. Preserve previous requests.
: >> "$ROOT/_local/runtime/requests.jsonl"
chmod u+x "$SERVER"
mkdir -p "$ROOT/_local/runtime/server-runs"
run_dir=$(mktemp -d "$ROOT/_local/runtime/server-runs/run.XXXXXXXX")
runtime_log="$run_dir/stdout.log"
echo "Starting Linux $ARCH server at http://$ADVERTISE_HOST:$PORT"
echo "Admin: http://127.0.0.1:$ADMIN_PORT/ ; Ctrl+C/SIGTERM stops gracefully."
echo "Runtime log: $runtime_log"
exec "$SERVER" "$@" -admin-log-file "$runtime_log" > "$runtime_log"
