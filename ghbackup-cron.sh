#!/usr/bin/env bash
# ghbackup-cron.sh — cron-friendly ghbackup wrapper.
#
# cron runs with a minimal PATH (often just /usr/bin:/bin) and without any
# shell profile. This wrapper adds the two directories where git + gh +
# go typically live on macOS, then execs `go run ./cmd/ghbackup` with the
# arguments you supply. Use this as the cron command; put the usual
# --owner/--list-file/--dest/--lock-file flags in the crontab entry.
#
# Usage (from cron or manually):
#
#   ./ghbackup-cron.sh --owner accretional \
#                      --dest /Volumes/wd_office_1/backup/repos \
#                      --quiet \
#                      --lock-file /tmp/ghbackup-accretional.lock
#
# See ghbackup-crontab.sample for ready-to-install crontab entries.

set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

# cron's default PATH on macOS is /usr/bin:/bin:/usr/sbin:/sbin. Add the
# two locations where git/gh/go land on a typical dev machine so the
# underlying binaries resolve.
export PATH="/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:${PATH:-}"

# GH_TOKEN / GITHUB_TOKEN come through from the cron environment if set
# there; otherwise ghbackup falls back to `gh auth token`, which requires
# $HOME to be readable so it can find ~/.config/gh/hosts.yml.
: "${HOME:?HOME must be set in the cron environment so gh auth resolves}"

exec go run ./cmd/ghbackup "$@"
