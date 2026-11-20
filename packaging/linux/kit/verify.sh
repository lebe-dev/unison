#!/usr/bin/env bash
# Checks the binaries of the kit and the installed package.
set -euo pipefail

kit="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="$(tr -d '[:space:]' < "$kit/VERSION")"

# Names the package and the kit share.
package="unison"
installed_daemon="/usr/bin/unisond"
installed_cli="/usr/bin/unison"
unit_file="/usr/lib/systemd/user/unisond.service"

failures=0

pass() { echo "ok      $*"; }
fail() {
    echo "FAIL    $*"
    failures=$((failures + 1))
}
note() { echo "note    $*"; }
skip() { echo "skipped $*"; }

data_dir() {
    if [ -n "${XDG_DATA_HOME:-}" ] && [ "${XDG_DATA_HOME#/}" != "$XDG_DATA_HOME" ]; then
        printf '%s/unison\n' "$XDG_DATA_HOME"
        return 0
    fi
    printf '%s/.local/share/unison\n' "$HOME"
}

if [ "$(uname -s)" = "Linux" ]; then
    pass "Linux $(uname -r) $(uname -m)"
else
    fail "this script checks a Linux machine, $(uname -s) is not supported"
fi

if [ -x "$kit/bin/unisond" ]; then
    got="$("$kit/bin/unisond" --version 2>&1 || true)"
    if [ "$got" = "unisond $version" ]; then
        pass "kit daemon: $got"
    else
        fail "kit daemon prints $got, want unisond $version"
    fi
else
    fail "kit daemon $kit/bin/unisond is missing or not executable"
fi

if [ -x "$kit/bin/unison" ]; then
    got="$("$kit/bin/unison" --version 2>&1 || true)"
    if [ "$got" = "unison $version" ]; then
        pass "kit client: $got"
    else
        fail "kit client prints $got, want unison $version"
    fi
else
    fail "kit client $kit/bin/unison is missing or not executable"
fi

if [ -e "$kit/bin/unison-probe" ]; then
    if [ -x "$kit/bin/unison-probe" ]; then
        pass "kit probe is present"
    else
        fail "kit probe is not executable"
    fi
else
    note "the kit has no unison-probe"
fi

installed=0
if command -v dpkg >/dev/null 2>&1 && dpkg -s "$package" >/dev/null 2>&1; then
    installed=1
fi

if [ "$installed" -eq 0 ]; then
    skip "installed package: skipped (no package installed)"
    skip "installed files: skipped (no package installed)"
    skip "user unit: skipped (no package installed)"
    skip "daemon process: skipped (no package installed)"
    skip "local socket: skipped (no package installed)"
    skip "installed version: skipped (no package installed)"
else
    pass "package $package $(dpkg-query -W -f='${Version}' "$package" 2>/dev/null || echo unknown)"

    for path in "$installed_daemon" "$installed_cli" "$unit_file"; do
        if [ -e "$path" ]; then
            pass "installed $path"
        else
            fail "installed $path"
        fi
    done

    if systemctl --user cat unisond >/dev/null 2>&1; then
        pass "user unit unisond is known to systemd"
    else
        fail "user unit unisond is not known, check $unit_file"
    fi

    pid="$(pgrep -u "$(id -u)" -x unisond 2>/dev/null | head -n 1 || true)"
    if [ -n "$pid" ]; then
        pass "unisond runs for $(id -un) with pid $pid"
    else
        note "unisond does not run for $(id -un)"
    fi

    socket="$(data_dir)/unisond.sock"
    if [ -S "$socket" ]; then
        mode="$(stat -c '%a' "$socket" 2>/dev/null || echo unknown)"
        if [ "$mode" = "600" ]; then
            pass "local socket $socket is 0600"
        else
            fail "local socket $socket has mode $mode, want 600"
        fi
    else
        note "local socket $socket is missing"
    fi

    if [ -x "$installed_daemon" ]; then
        got="$("$installed_daemon" --version 2>&1 || true)"
        if [ "$got" = "unisond $version" ]; then
            pass "installed daemon: $got"
        else
            fail "installed daemon prints $got, want unisond $version"
        fi
    fi
fi

echo
if [ "$failures" -gt 0 ]; then
    echo "verify: $failures checks failed"
    exit 1
fi

cat <<'EOF'
verify: check these by hand on the virtual machine:
  1. run scripts/run-tests.sh in the desktop session, not over ssh
  2. answer the clipboard question and paste the content in another app
  3. answer the keyring question and look for the items in seahorse
  4. switch the machine to bridged networking and run net-check.sh again
  5. log out and in again and check that the daemon of the package starts
EOF
