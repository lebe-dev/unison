#!/usr/bin/env bash
# S6: a large file copied on one device shows up on the other one as metadata
# and travels only when it is downloaded.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
e2e_init s6

cd "$E2E_REPO"
go test -count=1 -timeout 5m -run '^(TestNodesClipFileKeepsMetadata|TestNodesClipFileBelowLimit)$' ./core/node/

e2e_pass "file metadata on the peer, payload fetched on request"
