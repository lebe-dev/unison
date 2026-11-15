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
cargo install tauri-cli --version "^2"
```

Android builds need a JDK and the Android SDK:

```sh
brew install --cask temurin@21 android-studio
```

macOS builds need the command line tools. `gomobile` is required once the core is exposed to
Android:

```sh
xcode-select --install
go install golang.org/x/mobile/cmd/gomobile@latest
```

## Recipes

```sh
just build                # host binaries into bin/
just test                 # unit tests
just lint                 # linters and format check
just fmt                  # format the Go sources
just tidy                 # update go.mod and go.sum
just proto                # generate Go code from proto/ with buf
just proto-lint           # lint the protobuf definitions
just check                # lint, proto lint and tests, run before committing
just build-unisond-all    # unisond for darwin/arm64, darwin/amd64, windows/amd64
just clean                # remove bin/
```

Binaries are built without cgo so every target can be cross-compiled from any host.

The protobuf code generators are pinned as tools in `go.mod`, so `just proto` needs only `buf` and
`go` installed. Generated Go code is committed and builds do not require `buf`.

The version of all binaries comes from the `VERSION` file and is injected at build time, so
`bin/unisond --version` and `just build` always agree.
