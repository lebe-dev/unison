#!/bin/bash
# Builds the macOS app with the bundled daemon, signs it and notarizes it.
#
# Environment:
#   UNISON_MACOS_SIGN_IDENTITY   codesigning identity, ad-hoc when unset
#   UNISON_MACOS_APPLE_ID        Apple ID of the notarization account
#   UNISON_MACOS_APPLE_PASSWORD  app-specific password of that account
#   UNISON_MACOS_TEAM_ID         team ID of the account
#   UNISON_MACOS_NOTARY_PROFILE  keychain profile of `xcrun notarytool store-credentials`
#   UNISON_MACOS_SKIP_NOTARIZE   set to 1 to skip notarization
#
# Usage: package.sh [--universal] [--skip-notarize] [--no-dmg]

set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
version=$(tr -d '[:space:]' <"$root/VERSION")
build="$root/packaging/macos/build"

# The DSN comes from the environment or from the .env file of the repository.
if [[ -z "${SENTRY_DSN:-}" && -f "$root/.env" ]]; then
    SENTRY_DSN=$(sed -n 's/^SENTRY_DSN=//p' "$root/.env" | tail -n 1)
    SENTRY_DSN=${SENTRY_DSN#\"}
    SENTRY_DSN=${SENTRY_DSN%\"}
fi
entitlements="$root/desktop/src-tauri/entitlements.plist"
share_entitlements="$root/desktop/share-extension/entitlements.plist"
share_appex="Contents/PlugIns/UnisonShare.appex"
agent_plist="Library/LaunchAgents/app.unison.unisond.plist"

universal=0
notarize=1
create_dmg=1

usage() {
    sed -n '2,14p' "$0" | sed 's/^# \{0,1\}//'
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --universal) universal=1 ;;
        --skip-notarize) notarize=0 ;;
        --no-dmg) create_dmg=0 ;;
        --help | -h)
            usage
            exit 0
            ;;
        *)
            echo "package-macos: unknown argument $1" >&2
            exit 2
            ;;
    esac
    shift
done

if [[ "${UNISON_MACOS_SKIP_NOTARIZE:-0}" == "1" ]]; then
    notarize=0
fi

if [[ "$(uname -s)" != "Darwin" ]]; then
    echo "package-macos: the macOS bundle can only be built on macOS" >&2
    exit 1
fi

for tool in go yarn lipo codesign plutil hdiutil xcrun; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        echo "package-macos: $tool is missing" >&2
        exit 1
    fi
done

# ---------------------------------------------------------------------------
# Daemon
# ---------------------------------------------------------------------------

# The daemon needs cgo for the Keychain and the pasteboard, so every slice is
# built on macOS and merged into one universal binary.
daemon_ldflags="-s -w -X main.version=$version -X github.com/lebe-dev/unison/internal/observability.buildDSN=${SENTRY_DSN:-} -X github.com/lebe-dev/unison/internal/observability.buildEnvironment=release"

build_daemon() {
    local arch=$1 target=$2
    local cc=clang
    if [[ "$arch" == "amd64" ]]; then
        cc="clang -arch x86_64"
    fi
    echo "package-macos: building unisond for darwin/$arch"
    GOOS=darwin GOARCH="$arch" CGO_ENABLED=1 CC="$cc" go build \
        -trimpath -ldflags "$daemon_ldflags" \
        -o "$target" "$root/cmd/unisond"
}

daemon="$build/unisond"
mkdir -p "$build"
rm -f "$daemon" "$build/unisond-arm64" "$build/unisond-amd64"
build_daemon arm64 "$build/unisond-arm64"
build_daemon amd64 "$build/unisond-amd64"
lipo -create -output "$daemon" "$build/unisond-arm64" "$build/unisond-amd64"
rm -f "$build/unisond-arm64" "$build/unisond-amd64"
chmod 0755 "$daemon"

echo "package-macos: unisond $(lipo -archs "$daemon")"

# ---------------------------------------------------------------------------
# App bundle
# ---------------------------------------------------------------------------

tauri_args=(build --bundles app --config "{\"version\":\"$version\"}")
bundle_dir="$root/desktop/src-tauri/target/release/bundle/macos"
if [[ $universal -eq 1 ]]; then
    if ! command -v rustup >/dev/null 2>&1 ||
        ! rustup target list --installed 2>/dev/null | grep -x "x86_64-apple-darwin" >/dev/null; then
        echo "package-macos: the universal build needs the x86_64 target:" >&2
        echo "  rustup target add x86_64-apple-darwin" >&2
        exit 1
    fi
    tauri_args+=(--target universal-apple-darwin)
    bundle_dir="$root/desktop/src-tauri/target/universal-apple-darwin/release/bundle/macos"
fi

echo "package-macos: building the app"
(cd "$root/desktop" && yarn tauri "${tauri_args[@]}")

if [[ ! -d "$bundle_dir/Unison.app" ]]; then
    echo "package-macos: $bundle_dir/Unison.app was not built" >&2
    exit 1
fi

app="$build/Unison.app"
rm -rf "$app"
mkdir -p "$build"
cp -R "$bundle_dir/Unison.app" "$app"
chmod -R u+w "$app"

main_executable=$(/usr/libexec/PlistBuddy -c "Print :CFBundleExecutable" "$app/Contents/Info.plist")
if [[ ! -x "$app/Contents/MacOS/$main_executable" ]]; then
    echo "package-macos: the app has no executable $main_executable" >&2
    exit 1
fi

cp "$daemon" "$app/Contents/MacOS/unisond"
chmod 0755 "$app/Contents/MacOS/unisond"

# The share extension puts Unison into the Share menu of other apps.
share_args=()
if [[ $universal -eq 1 ]]; then
    share_args+=(--universal)
fi
mkdir -p "$app/Contents/PlugIns"
"$root/desktop/share-extension/build.sh" "$app/Contents/PlugIns" "$version" ${share_args[@]+"${share_args[@]}"}

if [[ ! -f "$app/Contents/$agent_plist" ]]; then
    echo "package-macos: the bundle has no $agent_plist" >&2
    echo "package-macos: check macOS.files in desktop/src-tauri/tauri.conf.json" >&2
    exit 1
fi
chmod 0644 "$app/Contents/$agent_plist"

for key in NSLocalNetworkUsageDescription NSBonjourServices; do
    if ! /usr/libexec/PlistBuddy -c "Print :$key" "$app/Contents/Info.plist" >/dev/null 2>&1; then
        echo "package-macos: Info.plist has no $key" >&2
        exit 1
    fi
done

# ---------------------------------------------------------------------------
# Signing
# ---------------------------------------------------------------------------

identity="${UNISON_MACOS_SIGN_IDENTITY:--}"
adhoc=0
if [[ "$identity" == "-" ]]; then
    adhoc=1
    echo "package-macos: UNISON_MACOS_SIGN_IDENTITY is not set, signing ad-hoc"
else
    if ! security find-identity -v -p codesigning | grep -F "$identity" >/dev/null; then
        echo "package-macos: no codesigning identity $identity in the keychain" >&2
        exit 1
    fi
fi

base_sign_args=(--force --options runtime --sign "$identity")
if [[ $adhoc -eq 0 ]]; then
    base_sign_args+=(--timestamp)
fi
sign_args=("${base_sign_args[@]}" --entitlements "$entitlements")

sign() {
    echo "package-macos: signing ${!#}"
    codesign "${sign_args[@]}" "$@"
}

# The extension is signed before the app, which seals it, and with its own
# sandbox entitlements.
echo "package-macos: signing $app/$share_appex"
codesign "${base_sign_args[@]}" --entitlements "$share_entitlements" "$app/$share_appex"
sign --identifier app.unison.unisond "$app/Contents/MacOS/unisond"
sign "$app/Contents/MacOS/$main_executable"
sign "$app"
codesign --verify --deep --strict --verbose=2 "$app"

# ---------------------------------------------------------------------------
# Notarization
# ---------------------------------------------------------------------------

notary_args=()
if [[ -n "${UNISON_MACOS_NOTARY_PROFILE:-}" ]]; then
    notary_args=(--keychain-profile "$UNISON_MACOS_NOTARY_PROFILE")
elif [[ -n "${UNISON_MACOS_APPLE_ID:-}" && -n "${UNISON_MACOS_APPLE_PASSWORD:-}" &&
    -n "${UNISON_MACOS_TEAM_ID:-}" ]]; then
    notary_args=(
        --apple-id "$UNISON_MACOS_APPLE_ID"
        --password "$UNISON_MACOS_APPLE_PASSWORD"
        --team-id "$UNISON_MACOS_TEAM_ID"
    )
fi

notarize() {
    local artifact=$1
    echo "package-macos: submitting $artifact for notarization"
    if ! xcrun notarytool submit "$artifact" --wait --output-format json "${notary_args[@]}"; then
        echo "package-macos: notarization failed" >&2
        exit 1
    fi
}

can_notarize() {
    [[ $notarize -eq 1 && $adhoc -eq 0 && ${#notary_args[@]} -gt 0 ]]
}

if can_notarize; then
    zip="$build/Unison-$version.zip"
    rm -f "$zip"
    ditto -c -k --keepParent "$app" "$zip"
    notarize "$zip"
    xcrun stapler staple "$app"
    rm -f "$zip"
else
    echo "package-macos: skipping notarization"
    if [[ $adhoc -eq 0 && $notarize -eq 1 ]]; then
        echo "package-macos: set UNISON_MACOS_NOTARY_PROFILE or UNISON_MACOS_APPLE_ID," \
            "UNISON_MACOS_APPLE_PASSWORD and UNISON_MACOS_TEAM_ID to notarize" >&2
    fi
fi

# ---------------------------------------------------------------------------
# Disk image
# ---------------------------------------------------------------------------

dmg="$build/Unison-$version.dmg"
if [[ $create_dmg -eq 1 ]]; then
    stage="$build/dmg"
    rm -rf "$stage" "$dmg"
    mkdir -p "$stage"
    cp -R "$app" "$stage/Unison.app"
    ln -s /Applications "$stage/Applications"
    hdiutil create -quiet -volname "Unison" -srcfolder "$stage" -format UDZO -ov "$dmg"
    rm -rf "$stage"
    if [[ $adhoc -eq 0 ]]; then
        codesign --force --sign "$identity" "$dmg"
    fi
    if can_notarize; then
        notarize "$dmg"
        xcrun stapler staple "$dmg"
        xcrun stapler validate "$dmg"
    fi
fi

echo "package-macos: $app"
if [[ $create_dmg -eq 1 ]]; then
    echo "package-macos: $dmg"
fi
