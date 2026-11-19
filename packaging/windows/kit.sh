#!/usr/bin/env bash
# Builds the archive that runs the Windows tests and checks on the target host.
#
# The archive holds the installer, the binaries for manual runs, a test binary
# per Go package and the PowerShell scripts that write the report.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
version="$(tr -d '[:space:]' < "$root/VERSION")"
target="x86_64-pc-windows-msvc"
build="$root/packaging/windows/build"
setup="$build/Unison-$version-setup.exe"
archive="$build/Unison-$version-windows-kit.zip"
stage="$build/windows-kit"
sentry_dsn="${SENTRY_DSN:-}"

if [ ! -f "$setup" ]; then
    echo "windows-kit: $setup is missing, build the installer first" >&2
    exit 1
fi

debug_ldflags="-s -w -X main.version=$version"
debug_ldflags="$debug_ldflags -X github.com/lebe-dev/unison/internal/observability.buildDSN=$sentry_dsn"
debug_ldflags="$debug_ldflags -X github.com/lebe-dev/unison/internal/observability.buildEnvironment=debug"

module="$(cd "$root" && go list -m)"

rm -rf "$stage"
mkdir -p "$stage/bin" "$stage/setup" "$stage/scripts" "$stage/tests"

sed "s/@VERSION@/$version/g" "$root/packaging/windows/kit/README.txt" > "$stage/README.txt"
cp "$root/VERSION" "$stage/VERSION"
cp "$setup" "$stage/setup/"

cp "$root/packaging/windows/kit/run-tests.ps1" "$stage/scripts/"
cp "$root/packaging/windows/kit/collect.ps1" "$stage/scripts/"
cp "$root/packaging/windows/kit/check-pipe.ps1" "$stage/scripts/"
cp "$root/packaging/windows/verify.ps1" "$stage/scripts/"

echo "windows-kit: building the command line binaries for windows/amd64"
(
    cd "$root"
    GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
        go build -trimpath -ldflags "$debug_ldflags" -o "$stage/bin/unisond.exe" ./cmd/unisond
    GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
        go build -trimpath -ldflags "$debug_ldflags" -o "$stage/bin/unison.exe" ./cmd/unison
)

echo "windows-kit: building the test binaries"
packages="$(
    cd "$root"
    GOOS=windows GOARCH=amd64 go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}|{{.Dir}}{{end}}' ./...
)"

while IFS='|' read -r pkg dir; do
    [ -n "$pkg" ] || continue
    rel="${pkg#"$module"/}"
    if [ "$rel" = "$pkg" ]; then
        echo "windows-kit: $pkg is not below the module root" >&2
        exit 1
    fi
    out="$stage/tests/$rel"
    mkdir -p "$out"

    # The clipboard tests replace the clipboard, so they are opt-in.
    if [ "$rel" = "platform/windows/clipboard" ]; then
        (
            cd "$root"
            GOOS=windows GOARCH=amd64 go test -c -ldflags "-s -w" -tags clipboardtest -o "$out/$(basename "$rel").test.exe" "$pkg"
        )
    else
        (
            cd "$root"
            GOOS=windows GOARCH=amd64 go test -c -ldflags "-s -w" -o "$out/$(basename "$rel").test.exe" "$pkg"
        )
    fi

    if [ -d "$dir/testdata" ]; then
        cp -R "$dir/testdata" "$out/testdata"
    fi
done <<< "$packages"

(
    cd "$stage"
    rm -f "$archive"
    zip -qrX "$archive" .
)

echo "windows-kit: $archive"
echo "windows-kit: sha256 $(shasum -a 256 "$archive" | awk '{print $1}')"
