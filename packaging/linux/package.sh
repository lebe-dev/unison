#!/usr/bin/env bash
# Builds the Linux package on macOS.
#
# The app and the Debian package need WebKitGTK and the Linux toolchain, so they
# are built in a linux/amd64 Docker container. The Go binaries are cross built
# on the host. The result is packaging/linux/build/Unison_<version>_amd64.deb.
#
# Usage: package.sh [--rebuild-image] [--no-cache]

set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
version="$(tr -d '[:space:]' < "$root/VERSION")"
build="$root/packaging/linux/build"
image_dir="$root/packaging/linux/build-image"
sidecars="$root/desktop/src-tauri/binaries"
platform="linux/amd64"
target="x86_64-unknown-linux-gnu"
prefix="unison-linux"
uid_gid="$(id -u):$(id -g)"
rebuild=0
no_cache=0
started=$SECONDS

usage() {
    cat <<'EOF'
usage: packaging/linux/package.sh [--rebuild-image] [--no-cache]

  --rebuild-image  build the docker image even when its tag exists
  --no-cache       build the docker image without the layer cache

The package lands in packaging/linux/build as Unison_<version>_amd64.deb.
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --rebuild-image)
            rebuild=1
            shift
            ;;
        --no-cache)
            no_cache=1
            rebuild=1
            shift
            ;;
        -h | --help)
            usage
            exit 0
            ;;
        *)
            echo "package-linux: unknown option $1" >&2
            usage >&2
            exit 2
            ;;
    esac
done

fail() {
    echo "package-linux: $*" >&2
    exit 1
}

if ! command -v docker >/dev/null 2>&1; then
    fail "docker is missing, install Docker Desktop"
fi

if ! docker info >/dev/null 2>&1; then
    fail "the docker daemon is not reachable, start Docker Desktop"
fi

# The package is built for amd64, which Docker Desktop emulates on Apple
# Silicon. The emulation needs Rosetta in the settings of Docker Desktop.
machine="$(docker run --rm --platform "$platform" ubuntu:22.04 uname -m 2>/dev/null || true)"
if [ "$machine" != "x86_64" ]; then
    echo "package-linux: the amd64 emulation is not available (got '$machine')" >&2
    echo "package-linux: in Docker Desktop open Settings > General and enable" >&2
    echo "package-linux: \"Use Rosetta for x86_64/amd64 emulation\"" >&2
    exit 1
fi

# The image is tagged with the hash of its Dockerfile, so a rebuild happens
# only when the toolchain or the system packages change.
image_hash="$(shasum -a 256 "$image_dir/Dockerfile" | awk '{print $1}' | cut -c1-12)"
image="$prefix-build:$image_hash"
if [ "$rebuild" -eq 1 ] || ! docker image inspect "$image" >/dev/null 2>&1; then
    build_args=(--platform "$platform" -t "$image")
    if [ "$no_cache" -eq 1 ]; then
        build_args+=(--no-cache)
    fi
    echo "package-linux: building the image $image"
    docker build "${build_args[@]}" "$image_dir"
else
    echo "package-linux: reusing the image $image"
fi

# The DSN comes from the environment or from the .env file of the repository.
sentry_dsn="${SENTRY_DSN:-}"
if [ -z "$sentry_dsn" ] && [ -f "$root/.env" ]; then
    sentry_dsn="$(sed -n 's/^SENTRY_DSN=//p' "$root/.env" | tail -n 1)"
    sentry_dsn="${sentry_dsn%\"}"
    sentry_dsn="${sentry_dsn#\"}"
fi

# The daemon and the client are pure Go, so they are cross built on the host.
# The daemon is the sidecar of the bundle and the client is installed next to
# it by the package.
ldflags="-s -w -X main.version=$version"
ldflags="$ldflags -X github.com/lebe-dev/unison/internal/observability.buildDSN=$sentry_dsn"
ldflags="$ldflags -X github.com/lebe-dev/unison/internal/observability.buildEnvironment=release"

echo "package-linux: building unisond and unison for linux/amd64"
mkdir -p "$sidecars"
(
    cd "$root"
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
        go build -trimpath -ldflags "$ldflags" -o "$sidecars/unisond-$target" ./cmd/unisond
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
        go build -trimpath -ldflags "$ldflags" -o "$sidecars/unison-$target" ./cmd/unison
)
chmod 0755 "$sidecars/unisond-$target" "$sidecars/unison-$target"

# The caches survive between the runs: the cargo registry and its git checkout,
# the target directory of the app and the yarn cache. The frontend modules live
# in their own volume, so the native modules of the host stay untouched. A
# volume of an earlier user belongs to root and is taken over once.
volume_node="$prefix-node-modules"
volume_cargo="$prefix-cargo"
volume_target="$prefix-target"
volume_yarn="$prefix-yarn"

ensure_volume() {
    local volume="$1"
    docker volume create "$volume" >/dev/null
    if docker run --rm --platform "$platform" --user "$uid_gid" \
        -v "$volume:/probe" "$image" \
        sh -c 'touch /probe/.writable && rm /probe/.writable' >/dev/null 2>&1; then
        return 0
    fi
    echo "package-linux: taking over the volume $volume"
    docker run --rm --platform "$platform" --user 0:0 \
        -v "$volume:/probe" "$image" chown -R "$uid_gid" /probe >/dev/null
}

for volume in "$volume_node" "$volume_cargo" "$volume_target" "$volume_yarn"; do
    ensure_volume "$volume"
done

echo "package-linux: building the app and the package"
docker run --rm --platform "$platform" --user "$uid_gid" \
    -e UNISON_VERSION="$version" \
    -e UNISON_SENTRY_DSN="$sentry_dsn" \
    -e CARGO_HOME=/cargo \
    -e CARGO_TARGET_DIR=/build/target \
    -e YARN_CACHE_FOLDER=/caches/yarn \
    -v "$root:/work" \
    -v "$volume_node:/work/desktop/node_modules" \
    -v "$volume_cargo:/cargo" \
    -v "$volume_target:/build/target" \
    -v "$volume_yarn:/caches/yarn" \
    "$image" bash /work/packaging/linux/build-image/build.sh

artifact="$build/Unison_${version}_amd64.deb"
if [ ! -f "$artifact" ]; then
    fail "$artifact was not built"
fi

echo "package-linux: $artifact"
echo "package-linux: $(du -h "$artifact" | awk '{print $1}')"
echo "package-linux: sha256 $(shasum -a 256 "$artifact" | awk '{print $1}')"
echo "package-linux: took $((SECONDS - started))s"
