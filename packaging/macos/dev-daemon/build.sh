#!/bin/bash
# Builds the development daemon as bin/unisond.app and links bin/unisond to it.
#
# macOS gives the local network permission to the app that is responsible for a
# process. A bare binary started from a terminal belongs to the terminal, so the
# daemon lives in a bundle of its own. The bundle keeps one identity across
# rebuilds: a fixed Mach-O UUID, a signature with a stable identifier for the
# Keychain, and a registration with LaunchServices.
#
# Environment:
#   UNISON_DEV_SIGN_IDENTITY   codesigning identity, the first Apple Development
#                              identity of the keychain when unset
#
# Usage: build.sh <ldflags>

set -euo pipefail

root=$(cd "$(dirname "$0")/../../.." && pwd)
ldflags="${1:-}"
uuid="4dc5a49dbd7e4abb837b9237af32859f"
app="$root/bin/unisond.app"
lsregister=/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister

mkdir -p "$app/Contents/MacOS"
tmp=$(mktemp "$app/Contents/MacOS/.unisond.XXXXXX")
trap 'rm -f "$tmp"' EXIT

cd "$root"
CGO_ENABLED=1 go build -trimpath -ldflags "$ldflags -B 0x$uuid" -o "$tmp" ./cmd/unisond
# The rename keeps the file of a running daemon intact.
mv -f "$tmp" "$app/Contents/MacOS/unisond"
cp "$root/packaging/macos/dev-daemon/Info.plist" "$app/Contents/Info.plist"

identity="${UNISON_DEV_SIGN_IDENTITY:-$(security find-identity -v -p codesigning | awk '/"Apple Development: / { print $2; exit }')}"
if [[ -z "$identity" ]]; then
    echo "no Apple Development identity found, so the daemon is signed ad-hoc and macOS asks for the Keychain again after each build; set UNISON_DEV_SIGN_IDENTITY to sign it" >&2
    identity="-"
fi
codesign --force --sign "$identity" "$app"
"$lsregister" -f "$app"

# Tools that start bin/unisond, such as the desktop app of a debug build, keep
# working.
ln -sfn unisond.app/Contents/MacOS/unisond "$root/bin/unisond"
