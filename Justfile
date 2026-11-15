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

# Run the linters and format checks.
lint:
    golangci-lint run

# Format the Go sources.
fmt:
    golangci-lint fmt

# Update go.mod and go.sum.
tidy:
    go mod tidy

# Check the code before committing.
check: lint test

# Remove build artifacts.
clean:
    rm -rf bin
