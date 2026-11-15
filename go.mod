module github.com/lebe-dev/unison

go 1.27

tool (
	connectrpc.com/connect/cmd/protoc-gen-connect-go
	golang.org/x/mobile/cmd/gobind
	google.golang.org/protobuf/cmd/protoc-gen-go
)

require (
	connectrpc.com/connect v1.21.1
	google.golang.org/protobuf v1.36.12
	lukechampine.com/blake3 v1.4.1
)

require (
	github.com/klauspost/cpuid/v2 v2.0.9 // indirect
	golang.org/x/mobile v0.0.0-20261011034528-197770444867 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/tools v0.52.0 // indirect
)
