set shell := ["bash", "-eu", "-o", "pipefail", "-c"]

version := trim(read("VERSION"))
ldflags := "-s -w -X main.version=" + version

# Binaries are pure Go so every target can be cross-compiled without a C toolchain.
export CGO_ENABLED := "0"

# The daemon reaches the Keychain and the pasteboard through cgo on macOS.
daemon_cgo := if os() == "macos" { "1" } else { "0" }

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
build:
    go build -trimpath -ldflags "{{ldflags}}" -o bin/ ./cmd/...

# Build unisond for the given OS and architecture.
build-unisond os arch:
    GOOS={{os}} GOARCH={{arch}} go build -trimpath -ldflags "{{ldflags}}" -o bin/unisond-{{os}}-{{arch}}{{ if os == "windows" { ".exe" } else { "" } }} ./cmd/unisond

# Build unisond for every supported desktop target.
build-unisond-all:
    just build-unisond darwin arm64
    just build-unisond darwin amd64
    just build-unisond windows amd64

# Build unisond for the host with the Keychain and pasteboard of the platform.
build-unisond-host:
    CGO_ENABLED={{daemon_cgo}} go build -trimpath -ldflags "{{ldflags}}" -o bin/unisond ./cmd/unisond

# Run the daemon of the default data directory, with its log on stderr.
run-daemon *args: build-unisond-host
    bin/unisond --log-stderr {{args}}

# Run a second daemon next to the first one, for pairing on a single machine.
run-daemon-2 *args: build-unisond-host
    bin/unisond --log-stderr --data-dir "{{daemon_second_dir}}" --name unisond-2 --clipboard=none {{args}}

# Run the unit tests.
test:
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

# Run the large blob round trip, which writes about 1.2 GiB of content.
test-blob-large:
    go test -tags bloblarge -timeout 30m ./core/blob/

# Run the large file transfer with an interruption and a resume.
test-share-large:
    go test -tags sharelarge -timeout 30m ./core/share/

# Build the binaries of the end-to-end scenarios.
e2e-build: build-unisond-host
    go build -trimpath -ldflags "{{ldflags}}" -o bin/unison ./cmd/unison
    go build -trimpath -ldflags "{{ldflags}}" -o bin/unison-relay ./cmd/unison-relay

# Run the end-to-end scenarios of the release matrix on this machine.
e2e *scenarios: e2e-build
    e2e/run.sh {{scenarios}}

# Vet and build the Go sources for Windows from any host.
vet-windows:
    GOOS=windows go vet ./...
    GOOS=windows go build ./...

# Run the linters and format checks.
lint:
    golangci-lint run

# Format the Go sources.
fmt:
    golangci-lint fmt

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

# Lint the protobuf definitions.
proto-lint:
    buf lint

relay_image := "unison-relay:" + version

# Build the relay container image.
relay-image:
    docker build --build-arg VERSION={{version}} -t {{relay_image}} -f deploy/relay/Dockerfile .

# Run the relay container from deploy/relay/docker-compose.yml.
relay-run:
    RELAY_IMAGE={{relay_image}} RELAY_VERSION={{version}} docker compose -f deploy/relay/docker-compose.yml up --build

# Check the code before committing.
check: lint proto-lint test

# Run the desktop app with hot reload.
desktop-dev:
    cd desktop && yarn tauri dev

# Build the desktop app bundle for the host platform.
desktop-build:
    cd desktop && yarn tauri build

# Build the macOS bundle with the daemon, then sign and notarize it.
package-macos *args:
    packaging/macos/package.sh {{args}}

# Check the layout, the property lists and the signature of the packaged macOS bundle.
verify-macos *args:
    packaging/macos/verify.sh {{args}}

# Build the Windows installer with the daemon, its startup entry and its firewall rule.
package-windows *args:
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File packaging/windows/package.ps1 {{args}}

# Check the Windows installer and the installed application.
verify-windows *args:
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File packaging/windows/verify.ps1 {{args}}

# Run the component tests of the desktop app.
desktop-test:
    cd desktop && yarn test

# Lint the desktop app: frontend checks, component tests, rustfmt and clippy.
desktop-lint:
    cd desktop && yarn lint
    cd desktop && yarn test
    cd desktop/src-tauri && cargo fmt --check
    cd desktop/src-tauri && cargo clippy --all-targets -- -D warnings

# Bind the Go core into an Android library that the app consumes.
aar:
    mkdir -p android/core bin
    go build -o bin/gobind golang.org/x/mobile/cmd/gobind
    PATH="{{justfile_directory()}}/bin:$PATH" go tool gomobile bind -target=android/arm64,android/arm,android/amd64 -androidapi 29 -javapkg=app.unison.core -ldflags "-X github.com/lebe-dev/unison/mobile.version={{version}}" -trimpath -o android/core/core.aar ./mobile
    rm -f android/core/core-sources.jar

# Build the Android debug APK.
android-build: aar
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
    if [ ! -f {{apk_debug}} ]; then
        just android-build
    fi
    adb -s "$phones" install -r {{apk_debug}}

# Run the Android unit tests.
android-test:
    ./android/gradlew -p android testDebugUnitTest

# Run the Android UI tests on the connected device or emulator.
android-test-ui:
    ./android/gradlew -p android connectedDebugAndroidTest

# Run the Android lint checks.
android-lint:
    ./android/gradlew -p android lintDebug

# Publish the keyboard bridge contract to the Maven local repository and to android/build/maven.
bridge-publish:
    ./android/gradlew -p android :bridge-aidl:publish :bridge-aidl:publishToMavenLocal

# Remove build artifacts.
clean:
    rm -rf bin
