set shell := ["bash", "-eu", "-o", "pipefail", "-c"]

version := trim(read("VERSION"))
ldflags := "-s -w -X main.version=" + version

# Binaries are pure Go so every target can be cross-compiled without a C toolchain.
export CGO_ENABLED := "0"

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

# Run the unit tests.
test:
    go test ./...

# Run the Keychain integration tests against the real login Keychain.
test-keychain:
    CGO_ENABLED=1 go test -tags keychaintest ./platform/darwin/keystore/

# Run the pasteboard integration tests against the real pasteboard.
test-pasteboard:
    CGO_ENABLED=1 go test -tags pasteboardtest ./platform/darwin/clipboard/

# Run the Win32 clipboard integration tests against the real clipboard (Windows).
test-clipboard:
    go test -tags clipboardtest ./platform/windows/clipboard/

# Run the large blob round trip, which writes about 1.2 GiB of content.
test-blob-large:
    go test -tags bloblarge -timeout 30m ./core/blob/

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

# Lint the protobuf definitions.
proto-lint:
    buf lint

# Check the code before committing.
check: lint proto-lint test

# Run the desktop app with hot reload.
desktop-dev:
    cd desktop && yarn tauri dev

# Build the desktop app bundle for the host platform.
desktop-build:
    cd desktop && yarn tauri build

# Lint the desktop app: frontend checks, rustfmt and clippy.
desktop-lint:
    cd desktop && yarn lint
    cd desktop/src-tauri && cargo fmt --check
    cd desktop/src-tauri && cargo clippy --all-targets -- -D warnings

# Bind the Go core into an Android library that the app consumes.
aar:
    mkdir -p android/core
    gomobile bind -target=android/arm64,android/arm,android/amd64 -androidapi 29 -javapkg=app.unison.core -ldflags "-X github.com/lebe-dev/unison/mobile.version={{version}}" -trimpath -o android/core/core.aar ./mobile
    rm -f android/core/core-sources.jar

# Build the Android debug APK.
android-build: aar
    ./android/gradlew -p android assembleDebug

# Run the Android unit tests.
android-test:
    ./android/gradlew -p android testDebugUnitTest

# Run the Android lint checks.
android-lint:
    ./android/gradlew -p android lintDebug

# Remove build artifacts.
clean:
    rm -rf bin
