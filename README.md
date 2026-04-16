# proto-repo

gRPC wrappers around `git` — an importer that clones/pulls/zips repositories
and a bag of per-subcommand RPCs that shell out to `git <cmd>` inside a
resolved repo path.

## Layout

```
repo.proto                 Repo, RepoList, RepoSource, RepoMsg, Importer
subcommands.proto          SubCommandReq + the SubCommands service
genpb/                     protoc output (committed so build.sh can skip regen)
importer/                  implements Importer (Download/Clone/Pull/Where/Zip)
subcommands/               implements SubCommands (one file per subcommand)
cmd/importerd/             gRPC daemon; registers Importer + SubCommands
docs/pull/                 one-shot scraper that rebuilds docs/git-links/*.csv
docs/git-links/            one CSV per section of https://git-scm.com/docs/git
docs/subcommand-urls.csv   flat url,subcommand list for the main porcelain set
setup.sh / build.sh / test.sh / LET_IT_RIP.sh
```

Build scripts stack: `build → setup`, `test → build`, `LET_IT_RIP → test + run`.
All four are idempotent.

## Running

```
./LET_IT_RIP.sh importerd --addr :7777 --scratch-dir ./scratch
```

Starts `importerd`, which registers both the `Importer` and `SubCommands`
services on a single gRPC listener. SIGINT/SIGTERM trigger `GracefulStop` so
in-flight streams drain cleanly.

## Mirror GitHub repos locally

```
# Mode 1 — everything under a user/org:
./LET_IT_RIP.sh ghbackup --owner <login> --dest <dir>
   [--include-forks] [--include-archived]

# Mode 2 — an explicit list (one `owner/name` or github URL per line;
# '#' introduces a comment):
./LET_IT_RIP.sh ghbackup --list-file <path> --dest <dir>
```

`cmd/ghbackup` is a thin CLI around `Importer.Download`. It clones each
repo into `<dest>/<name>` and is incremental — re-running fetches and
fast-forwards existing checkouts rather than re-cloning. Auth uses
`$GH_TOKEN`, `$GITHUB_TOKEN`, or `gh auth token` (in that order); git
itself authenticates through whatever credential helper is configured,
so run `gh auth setup-git` once if private clones prompt for a password.

Cron-friendly flags:

- `--quiet` suppresses per-repo "ok" lines; failures + final summary
  still print.
- `--lock-file <path>` acquires an `O_EXCL` lock before running; if the
  file already exists another run is assumed to be active and ghbackup
  exits 0 without doing work. Stale locks need manual cleanup — the
  tradeoff for never running two concurrent mirrors against the same
  `--dest`.

## Two ways to call SubCommands

`subcommands/` exposes every command twice:

- **String-args RPCs** — `Add`, `Commit`, `Log`, … each take a
  `SubCommandReq { Repo, repeated string args }` and shell out to
  `git <cmd> args...`. Thin, fast to add, no type safety — the server
  doesn't parse args.
- **Structured RPC** — `Execute(Subcommand)` takes a typed `oneof args`
  (`GitAddArguments`, `GitCommitArguments`, …) that the server compiles
  to argv. Type-checked at the proto boundary; catches typos at compile
  time for generated clients.

Both paths funnel through the same `run` / `runClone` / `runMkdir` helpers,
so behavior matches (Clean defaults to `--dry-run`, Pull defaults to
`--ff-only`, Clone rejects path-source repos, etc.).

## OptBool and friends

Proto3 scalar booleans can't distinguish "unset" from "false", which matters
for flags like `--[no-]tags`. Structured arguments use:

- `OptBool { UNSPECIFIED | TRUE | FALSE }` — tri-state for `--flag` /
  `--no-flag` / omit
- `OptInt { int64 value }` wrapper — distinguishes unset from `0`
- Dedicated enums (`FastForward`, `RecurseSubmodules`, …) for flags that
  take a named value

argv builders skip unspecified fields, so a zero-valued `GitXArguments{}`
produces a bare `git x` invocation.

## TODO: missing porcelain subcommands

`docs/subcommand-urls.csv` lists the 46 commands linked from the main
porcelain section of `https://git-scm.com/docs/git`. 38 are implemented in
`subcommands/`; the rest are gaps or deliberate omissions:

- `am` — apply mbox patches. Useful; not yet added.
- `format-patch` — generate patches from commits. Useful; not yet added.
- `grep` — git-aware grep across tracked files. Useful; not yet added.
- `scalar` — Microsoft's partial-clone helper. Nice-to-have.
- `citool`, `gui`, `gitk` — GUIs. Intentionally skipped (no sensible
  gRPC surface).

Adding one follows the same per-command shape as the existing 38: an
`.proto` RPC + `GitFooArguments` message, a file under `subcommands/` for
the string-args wrapper, an `argvFoo` builder in `structured.go`, and an
Execute smoke test in `execute_test.go`.

## Repo identity

Every RPC takes a `Repo` whose `RepoSource` oneof is one of:

- `uri` — clone URL; checkout lands in `<scratch>/<basename(uri)>`
- `gh`  — `{owner, name}`; checkout lands in `<scratch>/<name>`
- `path` — explicit local path; used as-is, never mkdir'd by Importer
- `gh_owner` — a user or org login; **Importer only**. Expands server-side
  to one entry per repo the owner has, filtered by `GithubOptions`
  (forks/archived excluded by default). Uses the GitHub API's authoritative
  `clone_url` so Enterprise hosts and SSH-preferred accounts work.

`path` sources are what let you run SubCommands against a repo the daemon did
not clone. `uri`/`gh` sources route through `<scratch-dir>` so the server can
find them later (e.g. `Where` without stat). `gh_owner` is a request to
"treat every repo in this account as an input" — any streaming RPC
(Download/Clone/Pull/Where) emits N messages for one `gh_owner` input.

## Errors vs. gRPC errors

Both services prefer structured failures: resolve errors, non-zero `git` exits,
and `.git` missing all land in `RepoMsg.errs` while the RPC itself returns
`nil`. gRPC errors are reserved for transport/framing problems. This keeps
streaming RPCs (`Download`, `Clone`, `Pull`, `Where`, `Zip`) useful even when
one repo in the list is broken — the stream doesn't abort partway.

## TODO: per-category gRPC services

The `docs/git-links/*.csv` files partition every command on
`https://git-scm.com/docs/git` into nine sections. The intent is one gRPC
service per section, each with one RPC per unique `git-<cmd>[1]` entry:

| package         | source CSV                      |
| --------------- | ------------------------------- |
| `plumbing/`     | `plumbing_commands.csv`         |
| `syncing/`      | `_syncing_repositories.csv`     |
| `revert/`       | `_reset_restore_and_revert.csv` |
| `misc/`         | `_other.csv`                    |
| `manipulation/` | `_manipulation_commands.csv`    |
| `interrogation/`| `_interrogation_commands.csv`   |
| `helper/`       | `_internal_helper_commands.csv` |
| `ancillary/`    | `_ancillary_commands.csv`       |
| `interaction/`  | `_interacting_with_others.csv`  |

Each file ends up ~150 trivial Go stubs, all following the `SubCommandReq →
run(git, <cmd>, args...) → RepoMsg` pattern already in `subcommands/`. Writing
them by hand is busywork; this is the use case
[`github.com/accretional/gluon`](https://github.com/accretional/gluon) is
being built to automate. **Hold off on implementing these nine packages until
gluon is ready.** When it is:

1. Generate one `<pkg>.proto` per row of the table above, deduped by URL and
   filtered to `[1]` section entries (skip `[5]`/`[7]` man-page links).
2. Generate one `<pkg>/<cmd>.go` per RPC — each delegates to a shared runner
   (export `Run` / `RunMkdir` from `subcommands/` when that time comes).
3. Register every service in `cmd/importerd/main.go` next to the existing
   `Importer` and `SubCommands` registrations.

Until then, `subcommands/` covers the main porcelain set (`add`, `commit`,
`log`, `status`, …) which is enough for the common flows.

## Regenerating `docs/git-links/`

```
go run ./docs/pull
```

Walks the rendered DOM of `https://git-scm.com/docs/git` and emits one CSV per
`<h3>` section containing every `/docs` anchor inside a child `dlist`/`ulist`.
