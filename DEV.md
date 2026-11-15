# Development

Everything runs through `just`. Install the toolchains below, then `just` alone lists the
available recipes.

## Toolchains

| Tool | Version | Used for |
|------|---------|----------|
| Go | 1.27 | daemon, CLI, relay, core |
| just | 1.59 | entry point for all tasks |
| golangci-lint | 2.x | linting and formatting |
| buf | 1.x | protobuf generation |
| Node.js | current LTS | Tauri and Svelte frontend |
| Rust | stable | Tauri |
| JDK | 21 | Android builds |
| Android SDK | API 29 min, current target | Android builds |
| Android NDK | 28.2.13676358 | gomobile |
| gomobile | golang.org/x/mobile (latest) | Android core binding |
| Xcode | current | macOS builds and signing |

Target platforms: macOS 13 Ventura and newer, Windows 10 22H2 / 11, Android 10 (API 29) and newer.

## Install

Go, the task runner and the linters:

```sh
brew install go just golangci-lint buf
```

Node.js and Rust for the Tauri frontend:

```sh
brew install node
rustup toolchain install stable
```

The Tauri CLI comes from the desktop package, install the frontend dependencies once:

```sh
cd desktop && yarn install
```

Android builds need a JDK and the Android SDK:

```sh
brew install --cask temurin@21 android-studio
```

macOS builds need the command line tools. The Android app embeds the Go core as an AAR, which
`gomobile` builds, so it needs the NDK as well:

```sh
xcode-select --install
go install golang.org/x/mobile/cmd/gomobile@latest
gomobile init
sdkmanager "ndk;28.2.13676358"
```

`gomobile` is pinned as a tool in `go.mod` and uses the NDK from `ANDROID_HOME/ndk`, or
`ANDROID_NDK_HOME` when set.

## Recipes

```sh
just build                # host binaries into bin/
just test                 # unit tests
just test-keychain        # Keychain integration tests against the login Keychain
just lint                 # linters and format check
just fmt                  # format the Go sources
just tidy                 # update go.mod and go.sum
just proto                # generate Go code from proto/ with buf
just proto-lint           # lint the protobuf definitions
just check                # lint, proto lint and tests, run before committing
just aar                  # bind the Go core into android/core/core.aar
just android-build        # build the Android debug APK
just build-unisond-all    # unisond for darwin/arm64, darwin/amd64, windows/amd64
just desktop-dev          # run the desktop app with hot reload
just desktop-build        # build the desktop app bundle for the host platform
just desktop-lint         # svelte-check, rustfmt and clippy for the desktop app
just clean                # remove bin/
```

Binaries are built without cgo so every target can be cross-compiled from any host.

The macOS Keychain keystore in `platform/darwin/keystore` uses cgo: it links Security.framework
and needs a host build to reach the Keychain. Without cgo, for example in a cross-compiled
binary, its operations return `keystore.ErrUnsupported` and building still works. The Keychain
integration tests use a separate service and are hidden behind the `keychaintest` build tag, so
`just test` never touches the Keychain. Run them with `just test-keychain`; they create items
under a random `app.unison.test.*` service and delete the items they created.

The protobuf code generators are pinned as tools in `go.mod`, so `just proto` needs only `buf` and
`go` installed. Generated Go code is committed and builds do not require `buf`.

The desktop app serves the frontend on `127.0.0.1:1420` during development and puts the
bundle in `desktop/src-tauri/target/release/bundle`.

`just aar` binds the `mobile` package with gomobile for API 29 and the `arm64-v8a`,
`armeabi-v7a` and `x86_64` ABIs. The library is written to `android/core/core.aar`, which the
app module depends on as a local file, so the AAR is not committed. The generated Java API
lives in the `app.unison.core.mobile` package; `android-build` runs `aar` first because the
app cannot be compiled without it.

The version of all binaries comes from the `VERSION` file and is injected at build time, so
`bin/unisond --version`, `just build` and the core version shown by the app always agree.
