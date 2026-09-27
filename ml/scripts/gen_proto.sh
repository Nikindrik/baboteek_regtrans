#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if ! command -v protoc >/dev/null 2>&1; then
  echo "ERROR: protoc not found. Ubuntu/WSL: sudo apt update && sudo apt install -y protobuf-compiler" >&2
  exit 1
fi

if ! command -v protoc-gen-go >/dev/null 2>&1; then
  echo "Installing protoc-gen-go v1.33.0..."
  go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.33.0
fi
if ! command -v protoc-gen-go-grpc >/dev/null 2>&1; then
  echo "Installing protoc-gen-go-grpc v1.3.0..."
  go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.3.0
fi

export PATH="$PATH:$(go env GOPATH)/bin"

protoc \
  --go_out=. --go_opt=paths=source_relative \
  --go-grpc_out=. --go-grpc_opt=paths=source_relative \
  proto/mosgortrans.proto

echo "Generated: proto/mosgortrans.pb.go proto/mosgortrans_grpc.pb.go"
