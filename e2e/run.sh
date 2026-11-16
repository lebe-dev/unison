#!/usr/bin/env bash
# Runs the end-to-end scenarios of the release matrix on this machine.
#
# Without arguments every scenario runs, otherwise the names given on the
# command line, for example: run.sh s1 s5.
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
scenarios=("$@")
if [ "${#scenarios[@]}" -eq 0 ]; then
    scenarios=(s1 s4 s5 s6 s7 s8 s9 s10)
fi

failed=0
summary=()
for scenario in "${scenarios[@]}"; do
    script=$(ls "$here/$scenario"-*.sh 2>/dev/null | head -1)
    if [ -z "$script" ]; then
        echo "unknown scenario $scenario" >&2
        summary+=("$scenario unknown")
        failed=1
        continue
    fi
    echo
    echo "=== $scenario"
    if bash "$script"; then
        summary+=("$scenario pass")
    else
        summary+=("$scenario FAIL")
        failed=1
    fi
done

echo
echo "=== summary"
printf '%s\n' "${summary[@]}"
exit "$failed"
