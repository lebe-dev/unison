#!/usr/bin/env bash
# S1: text copied on one device is on the clipboard of the other one without
# any action of the user.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
e2e_init s1

A=e2e-a
B=e2e-b

e2e_daemon "$A"
e2e_daemon "$B"
e2e_wait_api "$A"
e2e_wait_api "$B"
e2e_pair "$A" "$B"
e2e_wait_online "$A"
e2e_wait_online "$B"

e2e_copy_text "$A" "$B" "hello from $A"
first=$E2E_LAST_MS
e2e_copy_text "$B" "$A" "hello from $B, 1234567890"
second=$E2E_LAST_MS

# The item is in the history of the receiving device.
history=$(e2e_cli "$B" history ls --limit 5)
e2e_expect_contains "$history" "text" "history of $B"

e2e_pass "text both ways in ${first} ms and ${second} ms"
