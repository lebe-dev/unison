#!/usr/bin/env bash
# Builds the archive that runs the Linux tests and checks on an Ubuntu machine.
#
# The archive holds the daemon and the client for linux/amd64, a test binary for
# every Go package with tests on Linux and the scripts that write the report.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
version="$(tr -d '[:space:]' < "$root/VERSION")"
build="$root/packaging/linux/build"
stage="$build/linux-kit"
stamp="$(date +%Y%m%d-%H%M)"
archive="$build/Unison-$version-linux-kit-$stamp.tar.gz"
sentry_dsn="${SENTRY_DSN:-}"
with_deb=0

# Test tags of the platform integration tests, one package and tag pair per
# line. These tests use the session of the user, so they are not part of the
# default run of go test and the kit enables them for their own binary only.
platform_tags=(
    "platform/linux/clipboard:x11clipboardtest"
    "platform/linux/keystore:secretservicetest"
)

usage() {
    cat <<'EOF'
usage: packaging/linux/kit.sh [--with-deb]

  --with-deb  copy the newest .deb of packaging/linux/build into deb/

The archive lands in packaging/linux/build as
Unison-<VERSION>-linux-kit-<yyyyMMdd-HHmm>.tar.gz.
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --with-deb)
            with_deb=1
            shift
            ;;
        -h | --help)
            usage
            exit 0
            ;;
        *)
            echo "linux-kit: unknown option $1" >&2
            usage >&2
            exit 2
            ;;
    esac
done

# unique_archive returns a name that no earlier run used, so a second build in
# the same minute keeps the previous archive.
unique_archive() {
    local base="$1" candidate="$1" counter=1
    while [ -e "$candidate" ]; do
        counter=$((counter + 1))
        candidate="${base%.tar.gz}-$counter.tar.gz"
    done
    printf '%s\n' "$candidate"
}

debug_ldflags="-s -w -X main.version=$version"
debug_ldflags="$debug_ldflags -X github.com/lebe-dev/unison/internal/observability.buildDSN=$sentry_dsn"
debug_ldflags="$debug_ldflags -X github.com/lebe-dev/unison/internal/observability.buildEnvironment=debug"

module="$(cd "$root" && go list -m)"

deb=""
if [ "$with_deb" -eq 1 ]; then
    deb="$(find "$build" -maxdepth 1 -type f -name '*.deb' -print 2>/dev/null | sort | tail -n 1)"
    if [ -z "$deb" ]; then
        echo "linux-kit: no .deb in $build, build the package first" >&2
        exit 1
    fi
fi

rm -rf "$stage"
mkdir -p "$stage/bin" "$stage/scripts" "$stage/tests"

sed "s/@VERSION@/$version/g" "$root/packaging/linux/kit/README.txt" > "$stage/README.txt"
cp "$root/VERSION" "$stage/VERSION"

for script in run-tests.sh verify.sh collect.sh net-check.sh; do
    install -m 0755 "$root/packaging/linux/kit/$script" "$stage/scripts/$script"
done

install -d "$stage/systemd"
install -m 0644 "$root/packaging/linux/systemd/unisond.service" "$stage/systemd/unisond.service"

echo "linux-kit: building the command line binaries for linux/amd64"
(
    cd "$root"
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
        go build -trimpath -ldflags "$debug_ldflags" -o "$stage/bin/unisond" ./cmd/unisond
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
        go build -trimpath -ldflags "$debug_ldflags" -o "$stage/bin/unison" ./cmd/unison
    if [ -f "$root/cmd/unison-probe/main.go" ]; then
        GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
            go build -trimpath -ldflags "$debug_ldflags" -o "$stage/bin/unison-probe" ./cmd/unison-probe
    fi
)

# tags_for returns the test tags of a package path, one word.
tags_for() {
    local rel="$1" entry
    for entry in "${platform_tags[@]}"; do
        if [ "$rel" = "${entry%%:*}" ]; then
            printf '%s' "${entry#*:}"
            return 0
        fi
    done
}

echo "linux-kit: building the test binaries"
# The list also carries packages whose tests only exist under a platform tag, so
# an integration test without unit tests still gets a binary.
platform_tag_names=""
for entry in "${platform_tags[@]}"; do
    tag="${entry#*:}"
    if [ -z "$platform_tag_names" ]; then
        platform_tag_names="$tag"
    else
        platform_tag_names="$platform_tag_names,$tag"
    fi
done
packages="$(
    cd "$root"
    {
        GOOS=linux GOARCH=amd64 go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}|{{.Dir}}{{end}}' ./...
        GOOS=linux GOARCH=amd64 go list -tags "$platform_tag_names" -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}|{{.Dir}}{{end}}' ./...
    } | sort -u
)"

while IFS='|' read -r pkg dir; do
    [ -n "$pkg" ] || continue
    rel="${pkg#"$module"/}"
    if [ "$rel" = "$pkg" ]; then
        echo "linux-kit: $pkg is not below the module root" >&2
        exit 1
    fi
    out="$stage/tests/$rel"
    mkdir -p "$out"

    tags="$(tags_for "$rel")"
    tag_args=()
    if [ -n "$tags" ]; then
        tag_args=(-tags "$tags")
    fi

    (
        cd "$root"
        GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
            go test -c -ldflags "-s -w" ${tag_args[@]+"${tag_args[@]}"} \
            -o "$out/$(basename "$rel").test" "$pkg"
    )
    if [ -d "$dir/testdata" ]; then
        cp -R "$dir/testdata" "$out/testdata"
    fi
done <<< "$packages"

if [ -n "$deb" ]; then
    mkdir -p "$stage/deb"
    cp "$deb" "$stage/deb/"
    echo "linux-kit: deb $(basename "$deb")"
fi

archive="$(unique_archive "$archive")"
(
    cd "$stage"
    # The extended attributes of macOS would add AppleDouble files to the tar.
    COPYFILE_DISABLE=1 tar -czf "$archive" .
)

echo "linux-kit: $archive"
echo "linux-kit: sha256 $(shasum -a 256 "$archive" | awk '{print $1}')"
