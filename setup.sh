#!/usr/bin/env bash
# setup.sh — idempotent: install/verify build prerequisites for proto-repo.
# Safe to re-run; everything below short-circuits when already satisfied.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "setup.sh: required tool not found on PATH: $1" >&2
    return 1
  }
}

need go
need git
need protoc

# protoc plugins live in $(go env GOBIN) (or GOPATH/bin). `go install` is a
# no-op when the binary at the requested version is already cached.
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1

# Make sure the install dir is on PATH for downstream scripts in this shell.
GOBIN="$(go env GOBIN)"
[ -n "$GOBIN" ] || GOBIN="$(go env GOPATH)/bin"
case ":$PATH:" in
  *":$GOBIN:"*) ;;
  *) export PATH="$GOBIN:$PATH" ;;
esac

# Best-effort: download whatever is currently referenced. We do NOT run
# `go mod tidy` here because generated code (created by build.sh) is the
# source of truth for grpc/protobuf imports — tidying before generation
# would prune those deps.
go mod download 2>/dev/null || true
