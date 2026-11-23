#!/usr/bin/env bash
# Collects the logs and the state of the machine into an archive.
set -euo pipefail

kit="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="$(tr -d '[:space:]' < "$kit/VERSION")"
host="$(hostname 2>/dev/null | cut -d. -f1)"
[ -n "$host" ] || host="unknown"

data_dir=""
since=30

usage() {
    cat <<'EOF'
usage: scripts/collect.sh [--data-dir <path>] [--since <minutes>]

  --data-dir <path>  data directory of the daemon, $XDG_DATA_HOME/unison by
                     default, or ~/.local/share/unison without that variable
  --since <minutes>  keep the journal and the log files of the last minutes,
                     30 by default; 0 keeps everything

The archive lands next to the kit as
Unison-collect-<machine>-<yyyyMMdd-HHmm>.tar.gz.
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --data-dir)
            [ $# -ge 2 ] || { echo "collect: --data-dir needs a value" >&2; exit 2; }
            data_dir="$2"
            shift 2
            ;;
        --data-dir=*)
            data_dir="${1#--data-dir=}"
            shift
            ;;
        --since)
            [ $# -ge 2 ] || { echo "collect: --since needs a value" >&2; exit 2; }
            since="$2"
            shift 2
            ;;
        --since=*)
            since="${1#--since=}"
            shift
            ;;
        -h | --help)
            usage
            exit 0
            ;;
        *)
            echo "collect: unknown option $1" >&2
            usage >&2
            exit 2
            ;;
    esac
done

if ! [[ "$since" =~ ^[0-9]+$ ]]; then
    echo "collect: --since takes minutes as a number" >&2
    exit 2
fi

if [ -z "$data_dir" ]; then
    if [ -n "${XDG_DATA_HOME:-}" ] && [ "${XDG_DATA_HOME#/}" != "$XDG_DATA_HOME" ]; then
        data_dir="$XDG_DATA_HOME/unison"
    else
        data_dir="$HOME/.local/share/unison"
    fi
fi

# unique_archive returns a name that no earlier run used, so two runs in the
# same minute keep both archives.
unique_archive() {
    local base="$1" candidate="$1" counter=1
    while [ -e "$candidate" ]; do
        counter=$((counter + 1))
        candidate="${base%.tar.gz}-$counter.tar.gz"
    done
    printf '%s\n' "$candidate"
}

# strip_private removes the paths that never belong to a report.
strip_private() {
    local dir="$1" removed=0 path
    while IFS= read -r -d '' path; do
        rm -rf "$path"
        removed=$((removed + 1))
        echo "collect: removed $path"
    done < <(find "$dir" \( -name keys -o -name blobs -o -name 'unison.db*' -o -name '*.sock' \) -print0)
    if [ "$removed" -gt 0 ]; then
        echo "collect: removed $removed private paths"
    fi
}

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

{
    echo "Kit version: $version"
    echo "Machine: $host"
    echo "User: $(id -un)"
    echo "Data directory: $data_dir"
    echo "Report time: $(date '+%Y-%m-%d %H:%M:%S %z')"
    echo
    echo "== Operating system =="
    if [ -f /etc/os-release ]; then
        cat /etc/os-release
    else
        echo "/etc/os-release is missing"
    fi
    echo
    echo "== Kernel =="
    uname -a
    echo
    echo "== Session =="
    echo "XDG_SESSION_TYPE: ${XDG_SESSION_TYPE:-unset}"
    echo "XDG_CURRENT_DESKTOP: ${XDG_CURRENT_DESKTOP:-unset}"
    echo "DESKTOP_SESSION: ${DESKTOP_SESSION:-unset}"
    echo "DISPLAY: ${DISPLAY:-unset}"
    echo "WAYLAND_DISPLAY: ${WAYLAND_DISPLAY:-unset}"
} > "$work/system.txt"

log_count=0
if [ -d "$data_dir/logs" ]; then
    mkdir -p "$work/logs"
    while IFS= read -r -d '' path; do
        cp "$path" "$work/logs/"
        log_count=$((log_count + 1))
    done < <(if [ "$since" -gt 0 ]; then
        find "$data_dir/logs" -maxdepth 1 -type f -mmin "-$since" -print0
    else
        find "$data_dir/logs" -maxdepth 1 -type f -print0
    fi)
fi
if [ "$log_count" -eq 0 ]; then
    echo "no log files in $data_dir/logs" > "$work/logs.txt"
fi

if command -v journalctl >/dev/null 2>&1; then
    if [ "$since" -gt 0 ]; then
        journalctl --user -u unisond --no-pager --since "-${since}min" > "$work/journal.txt" 2>&1 || true
    else
        journalctl --user -u unisond --no-pager > "$work/journal.txt" 2>&1 || true
    fi
else
    echo "journalctl is not installed" > "$work/journal.txt"
fi

set +e
bash "$kit/scripts/verify.sh" > "$work/verify.txt" 2>&1
echo "verify exit code: $?" >> "$work/verify.txt"
bash "$kit/scripts/net-check.sh" > "$work/net-check.txt" 2>&1
echo "net-check exit code: $?" >> "$work/net-check.txt"
if [ -x "$kit/bin/unison" ]; then
    "$kit/bin/unison" status > "$work/client.txt" 2>&1
    echo "unison status exit code: $?" >> "$work/client.txt"
else
    echo "bin/unison is missing" > "$work/client.txt"
fi
set -e

{
    if command -v pgrep >/dev/null 2>&1; then
        pgrep -a unisond 2>/dev/null || echo "no unisond process"
    else
        echo "pgrep is not installed"
    fi
} > "$work/processes.txt"

{
    if [ -e "$data_dir/unisond.sock" ]; then
        ls -l "$data_dir/unisond.sock"
    else
        echo "no socket in $data_dir"
    fi
} > "$work/socket.txt"

strip_private "$work"

archive="$(unique_archive "$(dirname "$kit")/Unison-collect-$host-$(date +%Y%m%d-%H%M).tar.gz")"
tar -czf "$archive" -C "$work" .

if tar -tzf "$archive" | grep -Eq '(^|/)(keys|blobs)(/|$)|(^|/)unison\.db'; then
    rm -f "$archive"
    echo "collect: the archive held private data and was removed, check $data_dir" >&2
    exit 1
fi

echo "collect: $log_count log files from $data_dir/logs"
echo "$archive"
