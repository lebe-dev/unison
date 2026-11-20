set shell := ["bash", "-eu", "-o", "pipefail", "-c"]
set dotenv-load := true

version := trim(read("VERSION"))
ldflags := "-s -w -X main.version=" + version

# The DSN comes from .env or the environment and is empty in builds without
# reporting.
sentry_dsn := env("SENTRY_DSN", "")
sentry_ldflags := "-X github.com/lebe-dev/unison/internal/observability.buildDSN=" + sentry_dsn
debug_ldflags := ldflags + " " + sentry_ldflags + " -X github.com/lebe-dev/unison/internal/observability.buildEnvironment=debug"
release_ldflags := ldflags + " " + sentry_ldflags + " -X github.com/lebe-dev/unison/internal/observability.buildEnvironment=release"

# Version stamped into the artifacts of release/, and the version of a dev build,
# which adds the short commit hash.
release_version := version
release_dev_version := version + "-" + `git rev-parse --short HEAD`

# Every distribution artifact is written here.
release_dir := "release"

# Binaries are pure Go so every target can be cross-compiled without a C toolchain.
export CGO_ENABLED := "0"

# The daemon reaches the Keychain and the pasteboard through cgo on macOS.
daemon_cgo := if os() == "macos" { "1" } else { "0" }

# Data directory of the daemon.
daemon_dir := if os() == "windows" {
    env_var("LOCALAPPDATA") + "/Unison"
} else {
    home_directory() + "/Library/Application Support/Unison"
}

# Data directory of the second daemon, which pairs with the first one on the
# same machine.
daemon_second_dir := if os() == "windows" {
    env_var("LOCALAPPDATA") + "/Unison-2"
} else {
    home_directory() + "/Library/Application Support/Unison-2"
}

# List available recipes.
default:
    @just --list

# Build the Go binaries for the host platform into bin/.
build: lint-go
    go build -trimpath -ldflags "{{debug_ldflags}}" -o bin/ ./cmd/...

# Build unisond for the given OS and architecture.
build-unisond os arch:
    GOOS={{os}} GOARCH={{arch}} go build -trimpath -ldflags "{{debug_ldflags}}" -o bin/unisond-{{os}}-{{arch}}{{ if os == "windows" { ".exe" } else { "" } }} ./cmd/unisond

# Build unisond for every supported desktop target.
build-unisond-all:
    just build-unisond darwin arm64
    just build-unisond darwin amd64
    just build-unisond windows amd64
    just build-unisond linux amd64

# Build unisond for the host with the Keychain and pasteboard of the platform. On
# macOS it is the bundle bin/unisond.app, so the daemon holds its own permissions.
build-unisond-host:
    {{ if os() == "macos" { "packaging/macos/dev-daemon/build.sh '" + debug_ldflags + "'" } else { "CGO_ENABLED=" + daemon_cgo + " go build -trimpath -ldflags '" + debug_ldflags + "' -o bin/unisond ./cmd/unisond" } }}

# Build and run the daemon of the default data directory, replacing the one that runs.
run-daemon *args: build-unisond-host
    just stop-daemon "{{daemon_dir}}"
    {{ if os() == "macos" { "packaging/macos/dev-daemon/run.sh \"" + daemon_dir + "\"" } else { "exec bin/unisond --log-stderr" } }} {{args}}

# Run a second daemon next to the first one, for pairing on a single machine.
run-daemon-2 *args: build-unisond-host
    just stop-daemon "{{daemon_second_dir}}"
    {{ if os() == "macos" { "packaging/macos/dev-daemon/run.sh \"" + daemon_second_dir + "\"" } else { "exec bin/unisond --log-stderr" } }} --data-dir "{{daemon_second_dir}}" --name unisond-2 --clipboard=none {{args}}

# Stop the daemon that holds the lock of a data directory, and wait for it to exit.
stop-daemon dir=daemon_dir:
    packaging/macos/dev-daemon/stop.sh "{{dir}}"

# Run the unit tests.
test: lint-go
    go test ./...

# Fuzz the parser of the pairing code.
fuzz-qr time="30s":
    go test -run '^$' -fuzz '^FuzzParseQR$' -fuzztime {{time}} ./core/pairing/

# Fuzz one parser for the given time.
fuzz-target package target time:
    go test -run '^$' -fuzz '^{{target}}$' -fuzztime {{time}} {{package}}

# Fuzz every parser, with the given time per target.
fuzz time="10m":
    just fuzz-target ./core/session FuzzReadFrame {{time}}
    just fuzz-target ./core/session FuzzParseHello {{time}}
    just fuzz-target ./core/session FuzzStreamHeader {{time}}
    just fuzz-target ./core/clip FuzzClipAnnounce {{time}}
    just fuzz-target ./core/clip FuzzSanitizeImage {{time}}
    just fuzz-target ./core/history FuzzHistorySync {{time}}
    just fuzz-target ./core/history FuzzHistoryBatch {{time}}
    just fuzz-target ./core/trust FuzzIntroduction {{time}}
    just fuzz-target ./core/trust FuzzRevocation {{time}}
    just fuzz-target ./core/trust FuzzSetPeerPolicy {{time}}
    just fuzz-target ./core/gossip FuzzGossipDigest {{time}}
    just fuzz-target ./core/gossip FuzzGossipRecords {{time}}
    just fuzz-target ./core/share FuzzShareOffer {{time}}
    just fuzz-target ./core/share FuzzShareAnswer {{time}}
    just fuzz-target ./core/blob/transfer FuzzBlobRequest {{time}}
    just fuzz-target ./core/blob/transfer FuzzBlobTree {{time}}
    just fuzz-target ./core/blob/transfer FuzzHaveBlob {{time}}
    just fuzz-target ./core/discovery FuzzParseTXT {{time}}
    just fuzz-target ./core/discovery FuzzMDNSPacket {{time}}
    just fuzz-target ./core/transport FuzzPeerCertificate {{time}}
    just fuzz-target ./core/transport FuzzAuthProof {{time}}
    just fuzz-target ./core/identity FuzzPublicIdentity {{time}}
    just fuzz-target ./core/pairing FuzzPairingFrame {{time}}
    just fuzz-target ./core/pairing FuzzParseQR {{time}}
    just fuzz-target ./core/relay FuzzRelayFrame {{time}}
    just fuzz-target ./internal/relay FuzzServerDatagram {{time}}
    just fuzz-target ./internal/relay FuzzServerRegister {{time}}
    just fuzz-target ./platform/windows/clipboard FuzzCFHTML {{time}}
    just fuzz-target ./platform/windows/clipboard FuzzDIB {{time}}
    just fuzz-target ./platform/windows/clipboard FuzzHDROP {{time}}

# Fuzz every parser for a short while, as a smoke run.
fuzz-short:
    just fuzz 30s

# Run the long integration tests of the node harness.
test-integration:
    go test -tags integration -timeout 20m ./core/node/nodetest/

# Run the Keychain integration tests against the real login Keychain.
test-keychain:
    mkdir -p bin
    CGO_ENABLED=1 go test -c -tags keychaintest -o bin/keystore.test ./platform/darwin/keystore/
    ./bin/keystore.test -test.count=1

# Run the Secret Service integration tests against the keyring of the session (Linux).
test-keyring:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ "$(uname -s)" != Linux ]]; then
        echo "test-keyring runs on Linux only"
        exit 0
    fi
    go test -count=1 -tags secretservicetest ./platform/linux/keystore/

# Delete the Keychain items of the dev, test and per-directory hash services.
keychain-reset:
    #!/usr/bin/env bash
    set -uo pipefail
    removed=0
    while IFS=$'\t' read -r service account; do
        case "$service" in
            app.unison.dev.*|app.unison.test.*) ;;
            app.unison.*)
                suffix=${service#app.unison.}
                [[ $suffix =~ ^[0-9a-f]{8}$ ]] || continue
                ;;
            *) continue ;;
        esac
        if security delete-generic-password -s "$service" -a "$account" >/dev/null 2>&1; then
            removed=$((removed + 1))
        fi
    done < <(security dump-keychain 2>/dev/null | awk '
        /^class:/ { service = ""; account = "" }
        /"acct"<blob>=/ { account = $0; sub(/.*"acct"<blob>=/, "", account); sub(/^"/, "", account); sub(/"$/, "", account) }
        /"svce"<blob>=/ {
            service = $0
            sub(/.*"svce"<blob>=/, "", service)
            sub(/^"/, "", service)
            sub(/"$/, "", service)
            if (account != "") print service "\t" account
        }')
    echo "removed $removed keychain items"

# Run the pasteboard integration tests against the real pasteboard.
test-pasteboard:
    CGO_ENABLED=1 go test -count=1 -tags pasteboardtest ./platform/darwin/clipboard/

# Run the Win32 clipboard integration tests against the real clipboard (Windows).
test-clipboard:
    go test -tags clipboardtest ./platform/windows/clipboard/

# Run the X11 clipboard integration tests against the running display (Linux).
test-clipboard-linux:
    #!/usr/bin/env bash
    set -eu -o pipefail
    if [[ $(uname -s) != Linux ]]; then
        echo "the X11 clipboard tests run on Linux only"
        exit 0
    fi
    go test -count=1 -tags x11clipboardtest ./platform/linux/clipboard/

# Run the large blob round trip, which writes about 1.2 GiB of content.
test-blob-large:
    go test -tags bloblarge -timeout 30m ./core/blob/

# Run the large file transfer with an interruption and a resume.
test-share-large:
    go test -tags sharelarge -timeout 30m ./core/share/

# Build the binaries of the end-to-end scenarios.
e2e-build: lint-go build-unisond-host
    go build -trimpath -ldflags "{{debug_ldflags}}" -o bin/unison ./cmd/unison
    go build -trimpath -ldflags "{{debug_ldflags}}" -o bin/unison-relay ./cmd/unison-relay

# Run the end-to-end scenarios of the release matrix on this machine.
e2e *scenarios: e2e-build
    e2e/run.sh {{scenarios}}

# Lint and build the Go sources for Windows from any host.
vet-windows:
    GOOS=windows golangci-lint run
    GOOS=windows go build ./...

# Lint and build the Go sources for Linux amd64 from any host.
vet-linux:
    GOOS=linux GOARCH=amd64 golangci-lint run
    GOOS=linux GOARCH=amd64 go build ./...

# Lint the Go sources, including the security checks of gosec and the format.
lint-go:
    golangci-lint run

# Lint the protobuf definitions.
proto-lint:
    buf lint

# Lint the desktop frontend: ESLint, Prettier and svelte-check.
lint-desktop:
    cd desktop && yarn lint

# Lint the Rust shell of the desktop app: the format and the warnings.
lint-rust:
    cd desktop/src-tauri && cargo fmt --all --check
    cd desktop/src-tauri && cargo clippy --all-targets --locked -- -D warnings

# Check the Kotlin sources of the Android app with ktlint and detekt.
android-check:
    ./android/gradlew -p android ktlintCheck detekt

# Run the Android lint checks and the Kotlin checks.
android-lint: android-check
    ./android/gradlew -p android lintDebug

# Run every linter of the repository.
lint: lint-go lint-desktop lint-rust proto-lint android-lint

# Format the Go and the Rust sources of the desktop shell.
fmt: fmt-rust
    golangci-lint fmt

# Format the Rust shell of the desktop app.
fmt-rust:
    cd desktop/src-tauri && cargo fmt --all

# Update go.mod and go.sum.
tidy:
    go mod tidy

# Generate Go code from the protobuf definitions.
proto:
    buf generate

# Generate Java lite classes of the local and mobile APIs for the Android app.
proto-android:
    buf generate --template buf.gen.android.yaml --path proto/unison/local --path proto/unison/mobile proto

# Generate the TypeScript client of the local API for the desktop app.
desktop-proto:
    buf generate --template buf.gen.desktop.yaml --path proto/unison/local proto

relay_image := "unison-relay:" + version

# Build the relay container image.
relay-image:
    docker build --build-arg VERSION={{version}} --build-arg SENTRY_DSN='{{sentry_dsn}}' -t {{relay_image}} -f deploy/relay/Dockerfile .

relay_repo := "tinyops/unison-relay"
relay_platform := env("RELAY_PLATFORM", "linux/amd64")

# Build the relay image for the release platform and push it with the given tag.
[private]
relay-push tag:
    docker buildx build --platform {{relay_platform}} --build-arg VERSION={{tag}} --build-arg SENTRY_DSN='{{sentry_dsn}}' -t {{relay_repo}}:{{tag}} -f deploy/relay/Dockerfile --push .

# Push the relay image tagged with the version.
release-relay: (relay-push version)

relay_dev_tag := version + "-" + `git rev-parse --short HEAD`

# Push the relay image tagged with the version and the short commit hash.
release-relay-dev: (relay-push relay_dev_tag)

relay_host := env("RELAY_HOST", "kaiman")
relay_dir := env("RELAY_DIR", "/opt/unison-relay")

# Roll out the relay image with the given tag on the relay host.
[private]
relay-deploy tag:
    scp deploy/relay/docker-compose.proxy.yml {{relay_host}}:{{relay_dir}}/docker-compose.yml
    ssh {{relay_host}} 'set -e; cd {{relay_dir}}; touch .env; { grep -v "^RELAY_IMAGE=" .env || true; echo "RELAY_IMAGE={{relay_repo}}:{{tag}}"; } > .env.new; mv .env.new .env; docker compose pull; docker compose up -d --remove-orphans --wait; docker compose ps'

# Push the release relay image and run it on the relay host.
deploy-relay: release-relay (relay-deploy version)

# Push the dev relay image and run it on the relay host.
deploy-relay-dev: release-relay-dev (relay-deploy relay_dev_tag)

# Run the relay container from deploy/relay/docker-compose.yml.
relay-run:
    RELAY_IMAGE={{relay_image}} RELAY_VERSION={{version}} docker compose -f deploy/relay/docker-compose.yml up --build

# Scan the dependencies of every component for known vulnerabilities.
security-deps:
    osv-scanner scan source --recursive .

# Check the Go modules and the Go sources for known vulnerabilities.
security-go:
    govulncheck ./...

# Scan the working tree and the history for secrets.
security-secrets:
    gitleaks git . --redact --no-banner
    gitleaks dir . --redact --no-banner

# Check the deployment files for vulnerabilities and misconfigurations.
security-infra:
    trivy config --exit-code 1 deploy/

# Check the relay image for vulnerabilities.
security-image: relay-image
    trivy image --exit-code 1 {{relay_image}}

# Run every security check.
security: security-deps security-go security-secrets security-infra security-image

# Check the code before committing.
check: lint test vet-linux

# Run the desktop app with hot reload.
desktop-dev: lint-rust
    cd desktop && yarn tauri dev

# Build the desktop app bundle for the host platform.
desktop-build: lint-rust
    cd desktop && yarn tauri build

# Build the macOS bundle with the daemon, then sign and notarize it.
package-macos *args: lint-rust
    packaging/macos/package.sh {{args}}

# Check the layout, the property lists and the signature of the packaged macOS bundle.
verify-macos *args:
    packaging/macos/verify.sh {{args}}

# Build the Windows installer with the daemon, its startup entry and its firewall rule.
package-windows *args: lint-rust
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File packaging/windows/package.ps1 {{args}}

# Build the Windows installer from macOS with cargo-xwin and NSIS.
package-windows-cross: lint-rust
    packaging/windows/package-cross.sh

# Build the Linux package in a linux/amd64 Docker container.
package-linux *args: lint-rust
    packaging/linux/package.sh {{args}}

# Build the macOS disk image and the command line archive into release/.
release-macos ver=release_version *args:
    just release-macos-desktop "{{ver}}" {{args}}
    just release-macos-cli "{{ver}}"

# Build the macOS disk image into release/.
release-macos-desktop ver=release_version *args:
    #!/usr/bin/env bash
    set -euo pipefail
    root="{{justfile_directory()}}"
    UNISON_VERSION="{{ver}}" just --justfile "$root/Justfile" package-macos {{args}}
    mkdir -p "$root/{{release_dir}}"
    cp "$root/packaging/macos/build/Unison-{{ver}}.dmg" "$root/{{release_dir}}/unison-v{{ver}}-macos.dmg"
    echo "release: $root/{{release_dir}}/unison-v{{ver}}-macos.dmg"

# Build the macOS command line archive into release/.
release-macos-cli ver=release_version:
    #!/usr/bin/env bash
    set -euo pipefail
    root="{{justfile_directory()}}"
    out="$root/{{release_dir}}"
    stage="$(mktemp -d)"
    trap 'rm -rf "$stage"' EXIT
    mkdir -p "$stage/unison" "$out"
    ldflags="-s -w -X main.version={{ver}} {{sentry_ldflags}} -X github.com/lebe-dev/unison/internal/observability.buildEnvironment=release"
    cd "$root"
    for arch in arm64 amd64; do
        # clang compiles for the host architecture by default, so the target
        # architecture is named for every slice.
        cc_arch=arm64
        if [ "$arch" = amd64 ]; then
            cc_arch=x86_64
        fi
        echo "release: building the command line tools for darwin/$arch"
        GOOS=darwin GOARCH="$arch" CGO_ENABLED=1 CC="clang -arch $cc_arch" go build -trimpath -ldflags "$ldflags" -o "$stage/unison-$arch" ./cmd/unison
        GOOS=darwin GOARCH="$arch" CGO_ENABLED=1 CC="clang -arch $cc_arch" go build -trimpath -ldflags "$ldflags" -o "$stage/unisond-$arch" ./cmd/unisond
    done
    lipo -create -output "$stage/unison/unison" "$stage/unison-arm64" "$stage/unison-amd64"
    lipo -create -output "$stage/unison/unisond" "$stage/unisond-arm64" "$stage/unisond-amd64"
    rm -f "$stage"/unison-arm64 "$stage"/unison-amd64 "$stage"/unisond-arm64 "$stage"/unisond-amd64
    chmod 0755 "$stage/unison/unison" "$stage/unison/unisond"
    cat >"$stage/unison/README.txt" <<'EOF'
    Unison {{ver}} command line tools for macOS.

      unison    the command line client of the daemon
      unisond   the daemon

    Copy both files into a directory of PATH, for example /usr/local/bin.
    EOF
    tar -czf "$out/unison-v{{ver}}-macos-cli.tar.gz" -C "$stage" unison
    echo "release: $out/unison-v{{ver}}-macos-cli.tar.gz"

# Build the macOS artifacts of the current commit into release/.
release-macos-dev *args:
    just release-macos "{{release_dev_version}}" {{args}}

# Build the Windows installer and the command line archive into release/.
release-windows ver=release_version *args:
    just release-windows-desktop "{{ver}}" {{args}}
    just release-windows-cli "{{ver}}"

# Build the Windows installer into release/.
release-windows-desktop ver=release_version *args:
    #!/usr/bin/env bash
    set -euo pipefail
    root="{{justfile_directory()}}"
    case "$(uname -s)" in
        MINGW* | MSYS* | CYGWIN*)
            UNISON_VERSION="{{ver}}" just --justfile "$root/Justfile" package-windows {{args}}
            ;;
        *)
            UNISON_VERSION="{{ver}}" just --justfile "$root/Justfile" package-windows-cross
            ;;
    esac
    mkdir -p "$root/{{release_dir}}"
    cp "$root/packaging/windows/build/Unison-{{ver}}-setup.exe" "$root/{{release_dir}}/unison-v{{ver}}-windows.exe"
    echo "release: $root/{{release_dir}}/unison-v{{ver}}-windows.exe"

# Build the Windows command line archive into release/.
release-windows-cli ver=release_version:
    #!/usr/bin/env bash
    set -euo pipefail
    root="{{justfile_directory()}}"
    out="$root/{{release_dir}}"
    stage="$(mktemp -d)"
    trap 'rm -rf "$stage"' EXIT
    mkdir -p "$stage/unison" "$out"
    ldflags="-s -w -X main.version={{ver}} {{sentry_ldflags}} -X github.com/lebe-dev/unison/internal/observability.buildEnvironment=release"
    cd "$root"
    for name in unison unisond; do
        echo "release: building $name for windows/amd64"
        GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o "$stage/unison/$name.exe" ./cmd/$name
    done
    cat >"$stage/unison/README.txt" <<'EOF'
    Unison {{ver}} command line tools for Windows.

      unison.exe    the command line client of the daemon
      unisond.exe   the daemon

    Copy both files into a directory of PATH.
    EOF
    archive="$out/unison-v{{ver}}-windows-cli.zip"
    if command -v zip >/dev/null 2>&1; then
        (cd "$stage" && zip -qr "$archive" unison)
    else
        powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "Compress-Archive -Path (Join-Path '$stage' 'unison') -DestinationPath '$archive' -Force"
    fi
    echo "release: $archive"

# Build the Windows artifacts of the current commit into release/.
release-windows-dev *args:
    just release-windows "{{release_dev_version}}" {{args}}

# Build the Linux package and the command line archive into release/.
release-linux ver=release_version *args:
    just release-linux-desktop "{{ver}}" {{args}}
    just release-linux-cli "{{ver}}"

# Build the Linux package into release/.
release-linux-desktop ver=release_version *args:
    #!/usr/bin/env bash
    set -euo pipefail
    root="{{justfile_directory()}}"
    UNISON_VERSION="{{ver}}" just --justfile "$root/Justfile" package-linux {{args}}
    mkdir -p "$root/{{release_dir}}"
    cp "$root/packaging/linux/build/Unison_{{ver}}_amd64.deb" "$root/{{release_dir}}/unison-v{{ver}}-linux.deb"
    echo "release: $root/{{release_dir}}/unison-v{{ver}}-linux.deb"

# Build the Linux command line archive into release/.
release-linux-cli ver=release_version:
    #!/usr/bin/env bash
    set -euo pipefail
    root="{{justfile_directory()}}"
    out="$root/{{release_dir}}"
    stage="$(mktemp -d)"
    trap 'rm -rf "$stage"' EXIT
    mkdir -p "$stage/unison" "$out"
    ldflags="-s -w -X main.version={{ver}} {{sentry_ldflags}} -X github.com/lebe-dev/unison/internal/observability.buildEnvironment=release"
    cd "$root"
    for name in unison unisond; do
        echo "release: building $name for linux/amd64"
        GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o "$stage/unison/$name" ./cmd/$name
    done
    chmod 0755 "$stage/unison/unison" "$stage/unison/unisond"
    cat >"$stage/unison/README.txt" <<'EOF'
    Unison {{ver}} command line tools for Linux.

      unison    the command line client of the daemon
      unisond   the daemon

    Copy both files into a directory of PATH, for example /usr/local/bin.
    EOF
    tar -czf "$out/unison-v{{ver}}-linux-cli.tar.gz" -C "$stage" unison
    echo "release: $out/unison-v{{ver}}-linux-cli.tar.gz"

# Build the Linux artifacts of the current commit into release/.
release-linux-dev *args:
    just release-linux "{{release_dev_version}}" {{args}}

# Build the Android release APK into release/.
release-android ver=release_version:
    #!/usr/bin/env bash
    set -euo pipefail
    root="{{justfile_directory()}}"
    just --justfile "$root/Justfile" aar "{{ver}}" release
    UNISON_VERSION="{{ver}}" "$root/android/gradlew" -p "$root/android" assembleRelease
    apk_dir="$root/android/app/build/outputs/apk/release"
    apk="$(ls -t "$apk_dir"/*.apk 2>/dev/null | head -n 1 || true)"
    if [ -z "$apk" ]; then
        echo "release: no APK in $apk_dir" >&2
        exit 1
    fi
    mkdir -p "$root/{{release_dir}}"
    target="$root/{{release_dir}}/unison-v{{ver}}-android.apk"
    cp "$apk" "$target"
    if [[ "$apk" == *unsigned.apk ]]; then
        echo "release: $target is unsigned, set UNISON_ANDROID_KEYSTORE, UNISON_ANDROID_STORE_PASSWORD," >&2
        echo "release: UNISON_ANDROID_KEY_ALIAS and UNISON_ANDROID_KEY_PASSWORD to sign it" >&2
    fi
    echo "release: $target"

# Build the Android release APK of the current commit into release/.
release-android-dev:
    just release-android "{{release_dev_version}}"

# Build every artifact of every platform into release/.
release-all ver=release_version:
    just release-macos "{{ver}}"
    just release-windows "{{ver}}"
    just release-linux "{{ver}}"
    just release-android "{{ver}}"

# Build every artifact of the current commit into release/.
release-all-dev:
    just release-all "{{release_dev_version}}"

# Build the Windows installer and the test kit that runs on a Windows host.
windows-kit: package-windows-cross
    packaging/windows/kit.sh

# Build the Linux test kit that runs on an Ubuntu virtual machine.
linux-kit *args:
    packaging/linux/kit.sh {{args}}

linux_user := env("LINUX_USER", "user")
linux_host := env("LINUX_HOST", "")
linux_dir := env("LINUX_DIR", "unison-kit")

# Build the Linux test kit and copy it to the Linux test machine, sending only the changed files.
deploy-linux *args:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -z "{{linux_host}}" ]; then
        echo "deploy-linux: set LINUX_HOST in .env, for example ubuntu2204-test.tail712f59.ts.net" >&2
        exit 1
    fi
    dir="{{linux_dir}}"
    if [ -z "$dir" ]; then
        echo "deploy-linux: LINUX_DIR is empty" >&2
        exit 1
    fi
    just linux-kit {{args}}
    target="{{linux_user}}@{{linux_host}}"
    stage="packaging/linux/build/linux-kit"
    echo "deploy-linux: syncing the kit to $target:~/$dir"
    # rsync sends only the files that changed since the previous run, which
    # matters on a slow link: the test binaries are about 460 MB on disk.
    rsync -az --delete \
        --exclude 'deb/' \
        --exclude 'report/' \
        --exclude 'data/' \
        --exclude 'Unison-tests-*.tar.gz' \
        --exclude 'Unison-collect-*.tar.gz' \
        "$stage/" "$target:$dir/"
    ssh "$target" "ls -l \"\$HOME/$dir\""
    echo "deploy-linux: the kit is in ~/$dir on $target"
    echo "deploy-linux: run scripts/run-tests.sh from the desktop session of the machine"

# Open a shell on the Linux test machine.
linux-ssh *args:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -z "{{linux_host}}" ]; then
        echo "linux-ssh: set LINUX_HOST in .env, for example ubuntu2204-test.tail712f59.ts.net" >&2
        exit 1
    fi
    exec ssh {{linux_user}}@{{linux_host}} {{args}}

# Copy the test and collect archives of the Linux test machine into packaging/linux/build/reports.
linux-fetch:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -z "{{linux_host}}" ]; then
        echo "linux-fetch: set LINUX_HOST in .env, for example ubuntu2204-test.tail712f59.ts.net" >&2
        exit 1
    fi
    target="{{linux_user}}@{{linux_host}}"
    reports="packaging/linux/build/reports"
    mkdir -p "$reports"
    found=0
    for pattern in 'Unison-tests-*.tar.gz' 'Unison-collect-*.tar.gz'; do
        # The remote glob is expanded by the remote shell, so a machine without
        # archives of one kind is not an error.
        if ssh "$target" "ls ~/$pattern" >/dev/null 2>&1; then
            scp "$target:$pattern" "$reports/"
            found=1
        fi
    done
    if [ "$found" -eq 0 ]; then
        echo "linux-fetch: no archives on $target, run the scripts of the kit first" >&2
        exit 1
    fi
    ls -l "$reports"

# Check the Windows installer and the installed application.
verify-windows *args:
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File packaging/windows/verify.ps1 {{args}}

# Run the component tests of the desktop app.
desktop-test:
    cd desktop && yarn test

# Bind the Go core into an Android library that the app consumes.
aar ver=version env="debug": lint-go
    mkdir -p android/core bin
    go build -o bin/gobind golang.org/x/mobile/cmd/gobind
    PATH="{{justfile_directory()}}/bin:$PATH" go tool gomobile bind -target=android/arm64,android/arm,android/amd64 -androidapi 29 -javapkg=app.unison.core -ldflags "-X github.com/lebe-dev/unison/mobile.version={{ver}} {{sentry_ldflags}} -X github.com/lebe-dev/unison/internal/observability.buildEnvironment={{env}}" -trimpath -o android/core/core.aar ./mobile
    rm -f android/core/core-sources.jar

# Build the Android debug APK.
android-build: android-check aar
    ./android/gradlew -p android assembleDebug

apk_debug := "android/app/build/outputs/apk/debug/app-debug.apk"

# Install the current debug APK on the phone connected over USB.
deploy-android:
    #!/usr/bin/env bash
    set -eu -o pipefail
    phones="$(adb devices -l | awk 'NR > 1 && $2 == "device" && / usb:/ { print $1 }')"
    count="$(printf '%s\n' "$phones" | grep -c . || true)"
    if [ "$count" -eq 0 ]; then
        echo "no phone is connected over USB" >&2
        exit 1
    fi
    if [ "$count" -gt 1 ]; then
        echo "several phones are connected over USB, leave exactly one:" >&2
        printf '%s\n' "$phones" >&2
        exit 1
    fi
    # Always rebuild so a stale APK is never installed; Gradle and the Go build cache keep it fast.
    just android-build
    adb -s "$phones" install -r {{apk_debug}}

# Run the Android unit tests.
android-test: android-check
    ./android/gradlew -p android testDebugUnitTest

# Run the Android UI tests on the connected device or emulator.
android-test-ui: android-check aar
    ./android/gradlew -p android connectedDebugAndroidTest

# Publish the keyboard bridge contract to the Maven local repository and to android/build/maven.
bridge-publish: android-check
    ./android/gradlew -p android :bridge-aidl:publish :bridge-aidl:publishToMavenLocal

# Remove build artifacts.
clean:
    rm -rf bin
