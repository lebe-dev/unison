#!/usr/bin/env bash
# S5: a file shared on one device arrives in the downloads directory of the
# other one, with progress and a matching checksum.
#
# E2E_FILE_SIZE sets the payload size in bytes (32 MiB by default; the release
# scenario uses 524288000).
set -euo pipefail
source "$(dirname "$0")/lib.sh"
e2e_init s5

A=e2e-a
B=e2e-b

e2e_daemon "$A"
e2e_daemon "$B"
e2e_wait_api "$A"
e2e_wait_api "$B"
e2e_pair "$A" "$B"
e2e_wait_online "$A"
e2e_wait_online "$B"

# Received files go to the downloads directory of the daemon, which is a
# temporary directory of the scenario.
e2e_set_downloads "$B" "$E2E_ROOT/downloads"

size=${E2E_FILE_SIZE:-33554432}
payload="$E2E_ROOT/payload.bin"
head -c "$size" /dev/urandom >"$payload"
want=$(e2e_sha256 "$payload")

start=$(e2e_ms)
e2e_cli "$A" send "$payload" --to "$B"
deadline=$((SECONDS + 300)) row=""
while [ "$SECONDS" -lt "$deadline" ]; do
    row=$(e2e_cli "$B" transfers 2>/dev/null | tail -1)
    case "$row" in
        *done*) break ;;
    esac
    sleep 0.1
done
elapsed=$(( $(e2e_ms) - start ))
e2e_expect_contains "$row" "done" "state of the transfer on $B"
sender=$(e2e_cli "$A" transfers)
e2e_expect_contains "$sender" "done" "state of the transfer on $A"
e2e_expect_contains "$sender" "out" "direction of the transfer on $A"

received="$E2E_ROOT/downloads/payload.bin"
if [ ! -f "$received" ]; then
    echo "the file did not arrive in the downloads directory" >&2
    ls -l "$E2E_ROOT/downloads" >&2 || true
    exit 1
fi
got=$(e2e_sha256 "$received")
e2e_expect_eq "$want" "$got" "checksum of the received file"
received_size=$(wc -c <"$received" | tr -d ' ')
e2e_expect_eq "$size" "$received_size" "size of the received file"

echo "  $size bytes in ${elapsed} ms"
e2e_pass "file of $size bytes arrived in ${elapsed} ms with the same checksum"
