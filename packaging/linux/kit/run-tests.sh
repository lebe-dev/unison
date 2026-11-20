#!/usr/bin/env bash
# Runs the Go test binaries of the kit and writes the report archive.
set -euo pipefail

kit="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tests_dir="$kit/tests"
report_dir="$kit/report"
version="$(tr -d '[:space:]' < "$kit/VERSION")"
host="$(hostname 2>/dev/null | cut -d. -f1)"
[ -n "$host" ] || host="unknown"

# The clipboard and the keyring belong to the session of the user, so their
# tests only run after the user agrees.
clipboard_package="platform/linux/clipboard"
keyring_package="platform/linux/keystore"

timeout_per_package="10m"
package_filter=""
skip_clipboard=0
skip_keyring=0

usage() {
    cat <<'EOF'
usage: scripts/run-tests.sh [--package <text>] [--timeout <duration>]
                            [--skip-clipboard] [--skip-keyring]

  --package <text>   run the packages whose path contains the text
  --timeout <dur>    timeout of one package, 10m by default
  --skip-clipboard   skip the clipboard tests without asking
  --skip-keyring     skip the keyring tests without asking

The report lands in report/, the archive next to the kit as
Unison-tests-<machine>-<yyyyMMdd-HHmm>.tar.gz.
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --package)
            [ $# -ge 2 ] || { echo "run-tests: --package needs a value" >&2; exit 2; }
            package_filter="$2"
            shift 2
            ;;
        --package=*)
            package_filter="${1#--package=}"
            shift
            ;;
        --timeout)
            [ $# -ge 2 ] || { echo "run-tests: --timeout needs a value" >&2; exit 2; }
            timeout_per_package="$2"
            shift 2
            ;;
        --timeout=*)
            timeout_per_package="${1#--timeout=}"
            shift
            ;;
        --skip-clipboard)
            skip_clipboard=1
            shift
            ;;
        --skip-keyring)
            skip_keyring=1
            shift
            ;;
        -h | --help)
            usage
            exit 0
            ;;
        *)
            echo "run-tests: unknown option $1" >&2
            usage >&2
            exit 2
            ;;
    esac
done

# duration_seconds prints the seconds of a Go duration such as 10m or 1m30s.
duration_seconds() {
    local value="$1" total=0 count unit
    if [ -z "$value" ]; then
        return 1
    fi
    if ! [[ "$value" =~ ^([0-9]+[hms])+$ ]]; then
        return 1
    fi
    while [[ "$value" =~ ^([0-9]+)([hms])(.*)$ ]]; do
        count="${BASH_REMATCH[1]}"
        unit="${BASH_REMATCH[2]}"
        value="${BASH_REMATCH[3]}"
        case "$unit" in
            h) total=$((total + count * 3600)) ;;
            m) total=$((total + count * 60)) ;;
            s) total=$((total + count)) ;;
        esac
    done
    if [ "$total" -le 0 ]; then
        return 1
    fi
    printf '%s\n' "$total"
}

timeout_seconds="$(duration_seconds "$timeout_per_package")" || {
    echo "run-tests: the timeout $timeout_per_package is not a Go duration like 10m" >&2
    exit 2
}

if ! command -v timeout >/dev/null 2>&1; then
    echo "run-tests: timeout is missing, install coreutils" >&2
    exit 1
fi
if [ ! -d "$tests_dir" ]; then
    echo "run-tests: $tests_dir is missing" >&2
    exit 1
fi

# confirm asks the user and treats a missing terminal as "no".
confirm() {
    local question="$1" answer
    if [ ! -t 0 ]; then
        echo "run-tests: $question [y/N]"
        echo "run-tests: no terminal to ask, skipping"
        return 1
    fi
    read -r -p "$question [y/N] " answer
    case "$answer" in
        y | Y | yes | YES) return 0 ;;
        *) return 1 ;;
    esac
}

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

# package_name returns the path of a test binary below tests/.
package_name() {
    local binary="$1" dir
    dir="$(dirname "$binary")"
    printf '%s\n' "${dir#"$tests_dir"/}"
}

binaries="$(find "$tests_dir" -type f -name '*.test' | sort)"
if [ -z "$binaries" ]; then
    echo "run-tests: no test binaries in $tests_dir" >&2
    exit 1
fi

selected=()
has_clipboard=0
has_keyring=0
while IFS= read -r binary; do
    [ -n "$binary" ] || continue
    name="$(package_name "$binary")"
    case "$name" in
        *"$package_filter"*) ;;
        *) continue ;;
    esac
    selected+=("$binary")
    if [ "$name" = "$clipboard_package" ]; then
        has_clipboard=1
    fi
    if [ "$name" = "$keyring_package" ]; then
        has_keyring=1
    fi
done <<< "$binaries"

if [ "${#selected[@]}" -eq 0 ]; then
    echo "run-tests: no test binary matches $package_filter" >&2
    exit 1
fi

run_clipboard=1
if [ "$has_clipboard" -eq 1 ] && [ "$skip_clipboard" -eq 1 ]; then
    run_clipboard=0
fi
if [ "$has_clipboard" -eq 1 ] && [ "$skip_clipboard" -eq 0 ]; then
    if ! confirm "The clipboard tests replace the content of the clipboard. Run them?"; then
        run_clipboard=0
    fi
fi

run_keyring=1
if [ "$has_keyring" -eq 1 ] && [ "$skip_keyring" -eq 1 ]; then
    run_keyring=0
fi
if [ "$has_keyring" -eq 1 ] && [ "$skip_keyring" -eq 0 ]; then
    if ! confirm "The keyring tests write items into the login keyring. Run them?"; then
        run_keyring=0
    fi
fi

rm -rf "$report_dir"
mkdir -p "$report_dir"

# system_text describes the machine, so a failure can be read with the session
# in mind.
system_text() {
    echo "Kit version: $version"
    echo "Machine: $host"
    echo "User: $(id -un)"
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
    if command -v gnome-shell >/dev/null 2>&1; then
        echo "gnome-shell: $(gnome-shell --version 2>&1)"
    else
        echo "gnome-shell: not installed"
    fi
    echo
    echo "== Secret service =="
    if command -v busctl >/dev/null 2>&1; then
        if busctl --user list 2>/dev/null | awk '{print $1}' | grep -qx 'org.freedesktop.secrets'; then
            echo "org.freedesktop.secrets: on the session bus"
        else
            echo "org.freedesktop.secrets: not on the session bus"
        fi
    elif command -v gdbus >/dev/null 2>&1; then
        if gdbus introspect --session --dest org.freedesktop.secrets \
            --object-path /org/freedesktop/secrets >/dev/null 2>&1; then
            echo "org.freedesktop.secrets: on the session bus"
        else
            echo "org.freedesktop.secrets: not on the session bus"
        fi
    else
        echo "busctl and gdbus are missing, the session bus is not checked"
    fi
    echo
    echo "== Locale =="
    locale 2>/dev/null || echo "locale: not available"
    echo
    echo "== Data directory =="
    if [ -n "${XDG_DATA_HOME:-}" ] && [ "${XDG_DATA_HOME#/}" != "$XDG_DATA_HOME" ]; then
        echo "$XDG_DATA_HOME/unison"
    else
        echo "$HOME/.local/share/unison"
    fi
}

system_text > "$report_dir/system.txt"

summary=()
passed=0
failed=0
timed_out=0
skipped=0

for binary in "${selected[@]}"; do
    name="$(package_name "$binary")"
    binary_name="$(basename "$binary")"
    package_dir="$(dirname "$binary")"

    if [ "$name" = "$clipboard_package" ] && [ "$run_clipboard" -eq 0 ]; then
        echo "skipped $name"
        summary+=("$(printf '%-8s %s' 'skipped' "$name")")
        skipped=$((skipped + 1))
        continue
    fi
    if [ "$name" = "$keyring_package" ] && [ "$run_keyring" -eq 0 ]; then
        echo "skipped $name"
        summary+=("$(printf '%-8s %s' 'skipped' "$name")")
        skipped=$((skipped + 1))
        continue
    fi

    report_file="$report_dir/$name.txt"
    mkdir -p "$(dirname "$report_file")"

    echo "running $name"
    started="$(date +%s%N)"
    set +e
    (
        cd "$package_dir"
        timeout --kill-after=15s "$((timeout_seconds + 60))s" \
            "./$binary_name" -test.v -test.count=1 -test.timeout "$timeout_per_package"
    ) >"$report_file" 2>&1
    code=$?
    set -e
    finished="$(date +%s%N)"
    elapsed_ms=$(( (finished - started) / 1000000 ))
    seconds="$(printf '%d.%01d' $((elapsed_ms / 1000)) $(((elapsed_ms % 1000) / 100)))"

    status="ok"
    if [ "$code" -eq 124 ] || [ "$code" -eq 137 ]; then
        status="timeout"
        timed_out=$((timed_out + 1))
    elif grep -q 'panic: test timed out' "$report_file"; then
        status="timeout"
        timed_out=$((timed_out + 1))
    elif [ "$code" -ne 0 ]; then
        status="FAIL"
        failed=$((failed + 1))
    else
        passed=$((passed + 1))
    fi

    echo "  $status after $seconds s"
    summary+=("$(printf '%-8s %s (%s s, exit code %s)' "$status" "$name" "$seconds" "$code")")
done

total="${#selected[@]}"
summary+=("")
summary+=("total $total, ok $passed, FAIL $failed, timeout $timed_out, skipped $skipped")
printf '%s\n' "${summary[@]}" > "$report_dir/summary.txt"

archive="$(unique_archive "$(dirname "$kit")/Unison-tests-$host-$(date +%Y%m%d-%H%M).tar.gz")"
tar -czf "$archive" -C "$report_dir" .

echo
echo "run-tests: ok $passed, FAIL $failed, timeout $timed_out, skipped $skipped of $total"
echo "$archive"
if [ "$failed" -gt 0 ] || [ "$timed_out" -gt 0 ]; then
    exit 1
fi
