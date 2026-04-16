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

## Repo identity

Every RPC takes a `Repo` whose `RepoSource` oneof is one of:

- `uri` — clone URL; checkout lands in `<scratch>/<basename(uri)>`
- `gh`  — `{owner, name}`; checkout lands in `<scratch>/<name>`
- `path` — explicit local path; used as-is, never mkdir'd by Importer

`path` sources are what let you run SubCommands against a repo the daemon did
not clone. `uri`/`gh` sources route through `<scratch-dir>` so the server can
find them later (e.g. `Where` without stat).

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
