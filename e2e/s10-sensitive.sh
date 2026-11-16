#!/usr/bin/env bash
# S10: sensitive content never reaches the other device or the history.
#
# The pipeline drops a capture that the platform marked as sensitive, so the
# checks run against the clipboard pipeline with the markers both platforms set.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
e2e_init s10

cd "$E2E_REPO"
go test -count=1 -timeout 5m -run '^(TestSensitiveContentIsDropped|TestSensitiveContentDiscardsPendingChange)$' ./core/clip/

e2e_pass "sensitive content dropped before the announcement and the history"
