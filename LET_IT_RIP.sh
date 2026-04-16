#!/usr/bin/env bash
# LET_IT_RIP.sh — idempotent: full setup → build → test → run a cmd binary.
# Default target is the importer gRPC server (cmd/importerd). Override:
#   ./LET_IT_RIP.sh importerd --addr :7777 --scratch-dir ./scratch
#   ./LET_IT_RIP.sh indexer --org foo --out-dir ./out
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
./test.sh

target="${1:-importerd}"
shift || true
exec go run "./cmd/$target" "$@"
