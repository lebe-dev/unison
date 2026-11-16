#!/usr/bin/env bash
# Shared helpers of the end-to-end scenarios. Every scenario sources this file,
# starts its daemons with temporary data directories and cleans up on exit.
#
# The scripts never touch the data directory, the downloads directory or the
# Keychain items of a real installation: daemons get a private temporary root,
# run with --clipboard=none and use the predictable dev Keychain service of
# their directory name, which is deleted on exit.

set -o pipefail

E2E_REPO=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
E2E_BIN=${E2E_BIN:-$E2E_REPO/bin}
E2E_PIDS=()
E2E_EXTRA_PIDS=()
E2E_SERVICES=()
E2E_PASSED=0

# e2e_init prepares the temporary root of one scenario and the cleanup trap.
e2e_init() {
    E2E_NAME=$1
    local base=${E2E_TMPDIR:-${TMPDIR:-/tmp}}
    base=${base%/}
    E2E_ROOT=$(mktemp -d "$base/e2e-$E2E_NAME.XXXXXX")
    trap e2e_cleanup EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM
    for tool in unisond unison; do
        if [ ! -x "$E2E_BIN/$tool" ]; then
            echo "missing $E2E_BIN/$tool, run: just e2e-build" >&2
            exit 1
        fi
    done
    echo "SCENARIO $E2E_NAME (root $E2E_ROOT)"
}

# e2e_daemon starts a daemon of the scenario under the given name.
e2e_daemon() {
    local name=$1
    shift
    local dir="$E2E_ROOT/$name"
    mkdir -p "$dir"
    "$E2E_BIN/unisond" \
        --data-dir "$dir" \
        --endpoint "$dir/api.sock" \
        --clipboard=none \
        --name "$name" \
        "$@" >"$E2E_ROOT/$name.console" 2>&1 &
    local pid=$!
    E2E_PIDS+=("$pid")
    E2E_SERVICES+=("$name")
    echo "$pid" >"$E2E_ROOT/$name.pid"
}

# e2e_sandboxed starts a daemon under a sandbox profile, which cuts the network
# paths a relay scenario has to rule out.
e2e_sandboxed() {
    local name=$1 profile=$2
    shift 2
    local dir="$E2E_ROOT/$name"
    mkdir -p "$dir"
    sandbox-exec -f "$profile" "$E2E_BIN/unisond" \
        --data-dir "$dir" \
        --endpoint "$dir/api.sock" \
        --clipboard=none \
        --name "$name" \
        "$@" >"$E2E_ROOT/$name.console" 2>&1 &
    local pid=$!
    E2E_PIDS+=("$pid")
    E2E_SERVICES+=("$name")
    echo "$pid" >"$E2E_ROOT/$name.pid"
}

# e2e_track makes the cleanup stop a process that is not a daemon of the
# scenario, such as the relay.
e2e_track() {
    E2E_EXTRA_PIDS+=("$1")
}

# e2e_copy_text copies a text on one device and waits until the other one has
# it. E2E_LAST_MS holds the time, which also covers two command line starts.
e2e_copy_text() {
    local from=$1 to=$2 text=$3 timeout=${4:-15}
    local start end got="" deadline
    start=$(e2e_ms)
    printf '%s' "$text" | e2e_cli "$from" clip set >/dev/null
    deadline=$((SECONDS + timeout))
    while [ "$SECONDS" -lt "$deadline" ]; do
        got=$(e2e_cli "$to" clip get 2>/dev/null || true)
        if [ "$got" = "$text" ]; then
            break
        fi
        sleep 0.02
    done
    end=$(e2e_ms)
    e2e_expect_eq "$text" "$got" "clipboard text on $to"
    E2E_LAST_MS=$((end - start))
}

# e2e_stop stops one daemon of the scenario.
e2e_stop() {
    local name=$1 pid
    pid=$(cat "$E2E_ROOT/$name.pid" 2>/dev/null) || return 0
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    rm -f "$E2E_ROOT/$name.pid"
}

e2e_endpoint() {
    echo "$E2E_ROOT/$1/api.sock"
}

# e2e_cli runs the command line client against one daemon of the scenario.
e2e_cli() {
    local name=$1
    shift
    "$E2E_BIN/unison" --endpoint "$(e2e_endpoint "$name")" "$@"
}

# e2e_api calls one unary method of the local API with a JSON body.
e2e_api() {
    local name=$1 method=$2 body=${3:-"{}"}
    printf '%s' "$body" >"$E2E_ROOT/request.json"
    curl -sS --unix-socket "$(e2e_endpoint "$name")" --http2-prior-knowledge \
        -H 'content-type: application/json' \
        --data-binary @"$E2E_ROOT/request.json" \
        -o "$E2E_ROOT/response.json" \
        -w '%{http_code}' \
        "http://unison/unison.local.v1.UnisonService/$method"
}

# e2e_wait_api waits until the daemon answers on its endpoint.
e2e_wait_api() {
    local name=$1 timeout=${2:-90}
    local deadline=$((SECONDS + timeout))
    while [ "$SECONDS" -lt "$deadline" ]; do
        if e2e_cli "$name" status >/dev/null 2>&1; then
            return 0
        fi
        sleep 0.2
    done
    echo "daemon $name did not answer within ${timeout}s" >&2
    return 1
}

# e2e_pair pairs two daemons: the first one shows a code, the second one joins.
e2e_pair() {
    local show=$1 join=$2 role=${3:-personal}
    local out="$E2E_ROOT/pair-$show.out"
    ( yes y | e2e_cli "$show" pair --role "$role" >"$out" 2>&1 || true ) &
    local job=$!
    local deadline=$((SECONDS + 60)) uri=""
    while [ "$SECONDS" -lt "$deadline" ]; do
        uri=$(sed -n 's/^qr: //p' "$out" 2>/dev/null | head -1)
        if [ -n "$uri" ]; then
            break
        fi
        sleep 0.1
    done
    if [ -z "$uri" ]; then
        echo "no pairing code from $show" >&2
        cat "$out" >&2
        kill "$job" 2>/dev/null || true
        return 1
    fi
    e2e_cli "$join" pair --role "$role" --qr "$uri"
    wait "$job" 2>/dev/null || true
}

# e2e_wait_online waits until a daemon sees the paired devices.
e2e_wait_online() {
    local name=$1 min=${2:-1} state=${3:-online} timeout=${4:-60}
    local deadline=$((SECONDS + timeout)) count
    while [ "$SECONDS" -lt "$deadline" ]; do
        count=$(e2e_cli "$name" devices 2>/dev/null | grep -c "$state" || true)
        if [ "$count" -ge "$min" ]; then
            return 0
        fi
        sleep 0.2
    done
    echo "$name does not see $min device(s) in state $state within ${timeout}s" >&2
    e2e_cli "$name" devices >&2 || true
    return 1
}

# e2e_set_downloads points the downloads directory of a daemon at the scenario.
# The daemon has no flag for it and the files must not land in the real
# downloads directory of the user.
e2e_set_downloads() {
    local name=$1 dir=$2
    mkdir -p "$dir"
    local code
    code=$(e2e_api "$name" UpdateSettings "{\"downloadsDir\":\"$dir\"}")
    if [ "$code" != 200 ]; then
        echo "UpdateSettings answered $code on $name" >&2
        cat "$E2E_ROOT/response.json" >&2 || true
        return 1
    fi
}

# e2e_ms prints the wall clock in milliseconds.
e2e_ms() {
    python3 -c 'import time; print(int(time.time() * 1000))'
}

e2e_sha256() {
    shasum -a 256 "$1" | awk '{print $1}'
}

e2e_free_port() {
    python3 -c 'import socket
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()'
}

# e2e_expect_eq compares an expected and an actual value.
e2e_expect_eq() {
    local want=$1 got=$2 what=$3
    if [ "$want" != "$got" ]; then
        echo "unexpected $what: got \"$got\", want \"$want\"" >&2
        return 1
    fi
}

# e2e_expect_contains fails when a text does not hold a substring.
e2e_expect_contains() {
    local haystack=$1 needle=$2 what=$3
    case "$haystack" in
        *"$needle"*) return 0 ;;
        *)
            echo "unexpected $what: \"$needle\" is missing in:" >&2
            printf '%s\n' "$haystack" >&2
            return 1
            ;;
    esac
}

# e2e_pass reports a scenario that reached its checks and ends it. A run that
# ends before this call is a failure, which keeps the result right even when the
# shell stops on its own.
e2e_pass() {
    E2E_PASSED=1
    echo "PASS $E2E_NAME: $*"
    exit 0
}

# e2e_dump_logs prints the logs of the scenario after a failure.
e2e_dump_logs() {
    local name
    for name in ${E2E_SERVICES[@]+"${E2E_SERVICES[@]}"}; do
        [ -f "$E2E_ROOT/$name.console" ] || continue
        if [ -s "$E2E_ROOT/$name.console" ]; then
            echo "--- $name console ---" >&2
            tail -20 "$E2E_ROOT/$name.console" >&2
        fi
        if [ -f "$E2E_ROOT/$name/logs/unisond.log" ]; then
            echo "--- $name log ---" >&2
            tail -30 "$E2E_ROOT/$name/logs/unisond.log" >&2
        fi
    done
}

# e2e_clean_services removes the Keychain items the daemons created. The names
# are predictable because the service follows the directory name, so the
# scenario only removes its own items.
e2e_clean_services() {
    if [ "${E2E_KEEP_KEYCHAIN:-0}" = 1 ]; then
        echo "kept Keychain items app.unison.dev.e2e-*"
        return 0
    fi
    local name service count
    for name in ${E2E_SERVICES[@]+"${E2E_SERVICES[@]}"}; do
        service="app.unison.dev.$name"
        count=0
        while [ "$count" -lt 8 ] && security delete-generic-password -s "$service" </dev/null >/dev/null 2>&1; do
            count=$((count + 1))
        done
    done
}

e2e_cleanup() {
    local status=$?
    set +e
    local name pid
    for name in ${E2E_SERVICES[@]+"${E2E_SERVICES[@]}"}; do
        pid=$(cat "$E2E_ROOT/$name.pid" 2>/dev/null)
        if [ -n "$pid" ]; then
            kill "$pid" 2>/dev/null
        fi
    done
    for name in ${E2E_SERVICES[@]+"${E2E_SERVICES[@]}"}; do
        pid=$(cat "$E2E_ROOT/$name.pid" 2>/dev/null)
        if [ -n "$pid" ]; then
            wait "$pid" 2>/dev/null
        fi
    done
    local extra
    for extra in ${E2E_EXTRA_PIDS[@]+"${E2E_EXTRA_PIDS[@]}"}; do
        kill "$extra" 2>/dev/null
        wait "$extra" 2>/dev/null
    done
    if [ "$E2E_PASSED" -ne 1 ]; then
        status=1
        echo "FAIL $E2E_NAME" >&2
        e2e_dump_logs
    fi
    e2e_clean_services
    if [ "${E2E_KEEP:-0}" = 1 ]; then
        echo "kept $E2E_ROOT"
    else
        rm -rf "$E2E_ROOT"
    fi
    exit "$status"
}
