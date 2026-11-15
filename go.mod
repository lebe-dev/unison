module github.com/lebe-dev/unison

go 1.27

tool (
	connectrpc.com/connect/cmd/protoc-gen-connect-go
	golang.org/x/mobile/cmd/gobind
	google.golang.org/protobuf/cmd/protoc-gen-go
)

require (
	connectrpc.com/connect v1.21.1
	github.com/google/uuid v1.6.0
	github.com/quic-go/quic-go v0.65.0
	golang.org/x/crypto v0.59.0
	golang.org/x/sys v0.50.0
	google.golang.org/protobuf v1.36.12
	lukechampine.com/blake3 v1.4.1
	modernc.org/sqlite v1.62.0
)

require (
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/klauspost/cpuid/v2 v2.0.9 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/mobile v0.0.0-20261011034528-197770444867 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/tools v0.52.0 // indirect
	modernc.org/libc v1.80.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)
