#!/bin/bash
# Checks the layout, the property lists and the signature of a packaged app.
#
# Usage: verify.sh [path/to/Unison.app]

set -uo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
app="${1:-$root/packaging/macos/build/Unison.app}"

failures=0

pass() { echo "ok    $1"; }
fail() {
    echo "FAIL  $1"
    failures=$((failures + 1))
}
note() { echo "note  $1"; }

if [[ ! -d "$app" ]]; then
    echo "verify-macos: $app is missing, run the package recipe first" >&2
    exit 1
fi

plist="$app/Contents/Info.plist"
agent="$app/Contents/Library/LaunchAgents/app.unison.unisond.plist"

plist_value() { plutil -extract "$2" raw -o - "$1" 2>/dev/null; }

# Bundle layout.
main_executable=$(plist_value "$plist" CFBundleExecutable)
if [[ -n "$main_executable" && -x "$app/Contents/MacOS/$main_executable" ]]; then
    pass "main executable $main_executable"
else
    fail "main executable of the bundle"
fi

if [[ -x "$app/Contents/MacOS/unisond" ]]; then
    pass "daemon Contents/MacOS/unisond"
else
    fail "daemon Contents/MacOS/unisond"
fi

if plutil -lint -s "$agent" >/dev/null 2>&1; then
    pass "login item property list parses"
else
    fail "login item property list"
fi

label=$(plist_value "$agent" Label)
program=$(plist_value "$agent" BundleProgram)
run_at_load=$(plist_value "$agent" RunAtLoad)
[[ "$label" == "app.unison.unisond" ]] && pass "login item label $label" ||
    fail "login item label is $label"
[[ "$program" == "Contents/MacOS/unisond" ]] && pass "login item program $program" ||
    fail "login item program is $program"
[[ "$run_at_load" == "true" ]] && pass "login item runs at load" ||
    fail "login item does not run at load"

# Info.plist keys of the Local Network permission and the version.
identifier=$(plist_value "$plist" CFBundleIdentifier)
[[ "$identifier" == "app.unison" ]] && pass "bundle identifier $identifier" ||
    fail "bundle identifier is $identifier"

minimum=$(plist_value "$plist" LSMinimumSystemVersion)
[[ "$minimum" == 13.* ]] && pass "minimum system version $minimum" ||
    fail "minimum system version is $minimum"

description=$(plist_value "$plist" NSLocalNetworkUsageDescription)
[[ -n "$description" ]] && pass "local network usage description" ||
    fail "local network usage description"

services=$(plutil -extract NSBonjourServices json -o - "$plist" 2>/dev/null)
for service in _unison._udp _unison-pair._udp; do
    grep -q "\"$service\"" <<<"$services" && pass "bonjour service $service" ||
        fail "bonjour service $service"
done

# Signature.
if codesign --verify --deep --strict "$app" >/dev/null 2>&1; then
    pass "signature verifies"
else
    fail "signature does not verify"
fi

if [[ "$(codesign -dv --verbose=2 "$app" 2>&1 | sed -n 's/^Authority=//p' | head -1)" == \
    "Developer ID Application"* ]]; then
    note "signed for distribution"
else
    note "signed ad-hoc or by a development certificate"
fi

entitlements=$(codesign -d --entitlements :- "$app" 2>/dev/null)
for key in com.apple.security.network.client com.apple.security.network.server; do
    grep -q "$key" <<<"$entitlements" && pass "entitlement $key" ||
        fail "entitlement $key"
done

if grep -q "flags=.*runtime" <<<"$(codesign -d --verbose=4 "$app" 2>&1)"; then
    pass "hardened runtime"
else
    fail "hardened runtime"
fi

# Gatekeeper. Only a Developer ID signature passes, an ad-hoc build does not.
if spctl --assess --type execute --verbose "$app" >/dev/null 2>&1; then
    pass "gatekeeper assessment"
else
    note "gatekeeper rejects the build locally, which is expected without notarization"
fi

# Binaries.
archs=$(lipo -archs "$app/Contents/MacOS/unisond" 2>/dev/null || echo unknown)
[[ "$archs" == *"arm64"* && "$archs" == *"x86_64"* ]] && pass "daemon architectures $archs" ||
    note "daemon architectures $archs"

if version=$("$app/Contents/MacOS/unisond" --version 2>&1); then
    pass "daemon answers $version"
else
    fail "daemon does not start"
fi

if [[ -n "$main_executable" ]]; then
    if status=$("$app/Contents/MacOS/$main_executable" --login-item=status 2>&1); then
        if [[ "$status" == "not-found" ]]; then
            note "login item state $status, the system does not know the item of this build yet"
        else
            pass "login item state $status"
        fi
    else
        fail "login item state"
    fi
fi

echo
if [[ $failures -gt 0 ]]; then
    echo "verify-macos: $failures checks failed"
    exit 1
fi

cat <<'EOF'
verify-macos: the bundle is ready to install, check these by hand on the target machine:
  1. open the disk image, drag Unison to Applications, start it
  2. allow the local network request once and check that the paired devices appear
  3. log out and in again: the daemon runs without the app and the paired devices stay online
  4. copy text on one device and paste it on the other one
EOF
