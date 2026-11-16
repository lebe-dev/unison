#!/usr/bin/env bash
# S4: an image copied on one device is on the clipboard of the other one.
#
# The image scenarios need a real pasteboard on both ends, so this script drives
# the two nodes of the test harness, which exchange png and tiff payloads through
# the same core: an inline png, a png above the inline limit that travels through
# the blob store, and a tiff that arrives as png.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
e2e_init s4

cd "$E2E_REPO"
go test -count=1 -timeout 5m -run '^(TestNodesClipImage|TestNodesClipBlobImage|TestNodesClipTIFF)$' ./core/node/

e2e_pass "inline png, blob png and tiff to png between two nodes"
