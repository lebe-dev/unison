#!/usr/bin/env bash
# S9: a device of the same network that was never paired is refused on the TLS
# handshake and appears nowhere.
#
# The refusal itself needs a device that dials with its own certificate, which
# the command line cannot ask for; the harness does it in two packages. The live
# part of the script keeps an unpaired third daemon next to a paired pair and
# checks that nothing of it shows up.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
e2e_init s9

A=e2e-a
B=e2e-b
STRANGER=e2e-s

cd "$E2E_REPO"
go test -count=1 -timeout 5m -run '^TestUnknownDeviceRefused$' ./core/node/ ./core/node/nodetest/
cd - >/dev/null

e2e_daemon "$A"
e2e_daemon "$B"
e2e_daemon "$STRANGER"
e2e_wait_api "$A"
e2e_wait_api "$B"
e2e_wait_api "$STRANGER"
e2e_pair "$A" "$B"
e2e_wait_online "$A"
e2e_wait_online "$B"

stranger_id=$(e2e_cli "$STRANGER" --json status | python3 -c 'import json, sys; print(json.load(sys.stdin)["deviceId"])')
sleep 3

devices=$(e2e_cli "$A" devices)
count=$(printf '%s\n' "$devices" | awk 'NR > 1 && NF > 0' | wc -l | tr -d ' ')
e2e_expect_eq "1" "$count" "number of devices on $A"

if grep -q "$stranger_id" "$E2E_ROOT/$A/logs/unisond.log"; then
    echo "the log of $A mentions the unpaired device" >&2
    exit 1
fi
if grep -q "peer online" "$E2E_ROOT/$STRANGER/logs/unisond.log"; then
    echo "the unpaired device reports a peer" >&2
    exit 1
fi

e2e_pass "TLS refusal of an unknown device, unpaired device invisible in the LAN"
