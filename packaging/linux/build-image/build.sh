#!/usr/bin/env bash
# Builds the deb package inside the build image. The host mounts the repository
# at /work, the caches at /cargo and /caches and the target directory at /build.
set -euo pipefail

version="${UNISON_VERSION:?UNISON_VERSION is not set}"
target="x86_64-unknown-linux-gnu"
work=/work
out="$work/packaging/linux/build"
bundle="/build/target/$target/release/bundle/deb"

# The container runs with the user id of the host, which has no home directory
# in the image. Cargo, yarn and the app look for caches and configuration there.
export HOME=/tmp/unison-build-home
mkdir -p "$HOME"

if [ -n "${UNISON_SENTRY_DSN:-}" ]; then
    export SENTRY_DSN="$UNISON_SENTRY_DSN"
fi

cd "$work/desktop"
echo "linux-package: installing the frontend dependencies"
yarn install --frozen-lockfile --non-interactive

echo "linux-package: building the app and the package"
yarn tauri build --target "$target" --bundles deb --config "{\"version\":\"$version\"}"

deb="$(find "$bundle" -maxdepth 1 -name '*.deb' -type f -print 2>/dev/null | sort | tail -n 1)"
if [ -z "$deb" ]; then
    echo "linux-package: no package in $bundle" >&2
    exit 1
fi

mkdir -p "$out"
install -m 0644 "$deb" "$out/Unison_${version}_amd64.deb"
echo "linux-package: depends $(dpkg-deb -f "$out/Unison_${version}_amd64.deb" Depends)"
