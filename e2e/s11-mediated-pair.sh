#!/usr/bin/env bash
# S11: a trusted device introduces two other devices to each other.
#
# The nodes run in process on the virtual network: A and B are paired, B and C
# are paired, and B introduces A and C. The checks cover the Personal and the
# Friends role, the refusal of a role escalation, and the command line client
# that drives the exchange.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
e2e_init s11

cd "$E2E_REPO"
go test -count=1 -timeout 5m -run '^(TestMediatedPairPersonal|TestMediatedPairFriends|TestMediatedPairRefused)$' ./core/node/nodetest/
go test -count=1 -timeout 5m -run '^(TestIntroducePersonal|TestIntroduceFriends|TestIntroduceDecline|TestIntroduceList)$' ./cmd/unison/

e2e_pass "mediated pairing of two devices through a trusted one"
