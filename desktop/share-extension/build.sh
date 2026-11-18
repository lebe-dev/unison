#!/bin/bash
# Builds the share extension UnisonShare.appex, unsigned.
#
# Usage: build.sh <output-dir> <version> [--universal]

set -euo pipefail

if [[ $# -lt 2 ]]; then
    sed -n '2,4p' "$0" | sed 's/^# \{0,1\}//'
    exit 2
fi

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
out=$1
version=$2
universal=0
if [[ "${3:-}" == "--universal" ]]; then
    universal=1
fi

min_os=13.0
appex="$out/UnisonShare.appex"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

compile() {
    local arch=$1 target=$2
    echo "share-extension: building for $arch"
    xcrun swiftc \
        -O \
        -module-name UnisonShare \
        -target "$arch-apple-macos$min_os" \
        -parse-as-library \
        -application-extension \
        -framework AppKit \
        -Xlinker -e -Xlinker _NSExtensionMain \
        -o "$target" \
        "$here/ShareViewController.swift"
}

host_arch=$(uname -m)
if [[ $universal -eq 1 ]]; then
    compile arm64 "$work/UnisonShare-arm64"
    compile x86_64 "$work/UnisonShare-x86_64"
    lipo -create -output "$work/UnisonShare" "$work/UnisonShare-arm64" "$work/UnisonShare-x86_64"
else
    compile "$host_arch" "$work/UnisonShare"
fi

rm -rf "$appex"
mkdir -p "$appex/Contents/MacOS" "$appex/Contents/Resources"
cp "$work/UnisonShare" "$appex/Contents/MacOS/UnisonShare"
chmod 0755 "$appex/Contents/MacOS/UnisonShare"
cp "$here/Info.plist" "$appex/Contents/Info.plist"
plutil -replace CFBundleShortVersionString -string "$version" "$appex/Contents/Info.plist"
plutil -replace CFBundleVersion -string "$version" "$appex/Contents/Info.plist"
plutil -lint "$appex/Contents/Info.plist" >/dev/null
cp "$root/desktop/src-tauri/icons/icon.icns" "$appex/Contents/Resources/icon.icns"
printf 'XPC!????' >"$appex/Contents/PkgInfo"

echo "share-extension: $appex"
