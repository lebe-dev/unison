#!/usr/bin/env bash
# Builds the Windows installer on a macOS host.
#
# The daemon is a Go cross build, the app is a Rust cross build through
# cargo-xwin and the NSIS installer comes from makensis, which Homebrew ships.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
version="$(tr -d '[:space:]' < "$root/VERSION")"
target="x86_64-pc-windows-msvc"
sidecars="$root/desktop/src-tauri/binaries"
build="$root/packaging/windows/build"
sentry_dsn="${SENTRY_DSN:-}"

llvm_prefix="$(brew --prefix llvm 2>/dev/null || true)"
llvm_bin=""
if [ -n "$llvm_prefix" ] && [ -d "$llvm_prefix/bin" ]; then
    llvm_bin="$llvm_prefix/bin"
fi

missing=()
if ! command -v makensis >/dev/null 2>&1; then
    missing+=("makensis - brew install nsis")
fi
if ! { [ -n "$llvm_bin" ] && [ -x "$llvm_bin/llvm-rc" ]; } && ! command -v llvm-rc >/dev/null 2>&1; then
    missing+=("llvm-rc - brew install llvm")
fi
if ! { [ -n "$llvm_bin" ] && [ -x "$llvm_bin/lld-link" ]; } && ! command -v lld-link >/dev/null 2>&1; then
    missing+=("lld-link - brew install llvm")
fi
if ! command -v cargo-xwin >/dev/null 2>&1; then
    missing+=("cargo-xwin - cargo install --locked cargo-xwin")
fi
if ! rustup target list --installed | grep -qx "$target"; then
    missing+=("rustup target $target - rustup target add $target")
fi
if [ ${#missing[@]} -gt 0 ]; then
    echo "package-windows-cross: the cross build needs these tools:" >&2
    for item in "${missing[@]}"; do
        echo "  $item" >&2
    done
    exit 1
fi

if [ -n "$llvm_bin" ]; then
    export PATH="$llvm_bin:$PATH"
fi

# The sidecar of the bundle carries the target triple of the build.
ldflags="-s -w -H windowsgui -X main.version=$version"
ldflags="$ldflags -X github.com/lebe-dev/unison/internal/observability.buildDSN=$sentry_dsn"
ldflags="$ldflags -X github.com/lebe-dev/unison/internal/observability.buildEnvironment=release"

echo "package-windows-cross: building unisond for windows/amd64"
mkdir -p "$sidecars"
(
    cd "$root"
    GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
        go build -trimpath -ldflags "$ldflags" -o "$sidecars/unisond-$target.exe" ./cmd/unisond
)

echo "package-windows-cross: building the app and the installer"
echo "package-windows-cross: the first run downloads the MSVC CRT and the Windows SDK into ~/.cache/cargo-xwin, and the build takes a while"
config="{\"version\":\"$version\"}"
(
    cd "$root/desktop"
    yarn tauri build --runner cargo-xwin --target "$target" --bundles nsis --config "$config"
)

bundles="$root/desktop/src-tauri/target/$target/release/bundle/nsis"
installer="$(ls -t "$bundles"/*-setup.exe 2>/dev/null | head -n 1 || true)"
if [ -z "$installer" ]; then
    echo "package-windows-cross: no installer in $bundles" >&2
    exit 1
fi

mkdir -p "$build"
artifact="$build/Unison-$version-setup.exe"
cp "$installer" "$artifact"

echo "package-windows-cross: $artifact"
echo "package-windows-cross: sha256 $(shasum -a 256 "$artifact" | awk '{print $1}')"
echo "package-windows-cross: the artifacts are unsigned, Windows shows an unknown publisher"
