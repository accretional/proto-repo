#!/usr/bin/env bash
# build.sh — idempotent: regenerates proto stubs and builds all Go targets.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

./setup.sh

GOBIN="$(go env GOBIN)"
[ -n "$GOBIN" ] || GOBIN="$(go env GOPATH)/bin"
case ":$PATH:" in
  *":$GOBIN:"*) ;;
  *) export PATH="$GOBIN:$PATH" ;;
esac

mkdir -p genpb
protoc \
  -I . \
  --go_out=. --go_opt=module=github.com/accretional/proto-repo \
  --go-grpc_out=. --go-grpc_opt=module=github.com/accretional/proto-repo \
  repo.proto subcommands.proto

# Generated stubs may have introduced new imports (grpc, etc.); resolve them.
go mod tidy

go build ./...
