#!/usr/bin/env bash
# test.sh — idempotent: builds and runs all tests.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
./build.sh
go test ./...
