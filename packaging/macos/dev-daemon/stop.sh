#!/bin/bash
# Stops the daemon that holds the lock of a data directory and waits for it to
# exit. Nothing happens when no daemon holds it.
#
# Usage: stop.sh <data dir>

set -euo pipefail

lock="$1/unisond.lock"
if [[ ! -e "$lock" ]] || ! command -v lsof >/dev/null; then
    exit 0
fi
pids="$(lsof -t "$lock" 2>/dev/null || true)"
if [[ -z "$pids" ]]; then
    exit 0
fi
for pid in $pids; do
    echo "stopping the daemon $pid: $(ps -o command= -p "$pid" || true)" >&2
    kill -TERM "$pid" 2>/dev/null || true
done
for _ in $(seq 1 50); do
    alive=""
    for pid in $pids; do
        if kill -0 "$pid" 2>/dev/null; then
            alive="$alive $pid"
        fi
    done
    if [[ -z "$alive" ]]; then
        exit 0
    fi
    sleep 0.2
done
echo "the daemon did not stop:$alive" >&2
exit 1
