#!/bin/bash
# Runs the development daemon of bin/unisond.app with its log in the terminal.
#
# The daemon is started by LaunchServices, so the system treats it as an app of
# its own and asks for its local network permission. Ctrl-C stops it.
#
# Usage: run.sh <data dir> [daemon flags...]

set -euo pipefail

root=$(cd "$(dirname "$0")/../../.." && pwd)
dir="$1"
shift
app="$root/bin/unisond.app"

mkdir -p "$dir/logs"
log="$dir/logs/unisond-dev.log"
: >"$log"

tail -n +1 -f "$log" &
tail_pid=$!

cleanup() {
    "$root/packaging/macos/dev-daemon/stop.sh" "$dir" || true
    kill "$tail_pid" 2>/dev/null || true
}
trap cleanup EXIT
trap 'exit 130' INT TERM

# Bash runs a trap only between commands, and wait returns on a signal, so the
# launcher waits in the background.
open -n -W -a "$app" --stdout "$log" --stderr "$log" --args --log-stderr "$@" &
wait $!
