# gluon/

Generated gRPC service stubs for the per-section git commands catalogued in
`docs/git-links/*.csv`. One service per category, one RPC per unique
`git-<cmd>[1]` entry. All RPCs follow the thin-runner shape already in
`subcommands/`:

```proto
rpc <Cmd>(subcommands.SubCommandReq) returns (repo.RepoMsg);
```

so server implementations can delegate to a shared runner rather than
reimplementing argv construction per command.

## How it was produced

`cmd/stubgen` reads each CSV, filters to `[1]` entries (skipping `[5]`
and `[7]` man-page links, which describe file formats and concepts, not
commands), dedupes by command name, and writes one `.proto` per section:

```
go run ./cmd/stubgen          # regenerates everything in gluon/
go run ./cmd/stubgen --help   # flags
```

| Section | Output | RPCs |
| --- | --- | --- |
| Ancillary Commands | `gluon/ancillary/ancillary.proto` | 28 |
| Interacting With Others | `gluon/interaction/interaction.proto` | 10 |
| Internal Helper Commands | `gluon/helper/helper.proto` | 18 |
| Interrogation Commands | `gluon/interrogation/interrogation.proto` | 24 |
| Manipulation Commands | `gluon/manipulation/manipulation.proto` | 20 |
| Other | `gluon/misc/misc.proto` | 8 |
| Reset, Restore, Revert | `gluon/revert/revert.proto` | 3 |
| Syncing Repositories | `gluon/syncing/syncing.proto` | 11 |
| Plumbing Commands | `gluon/plumbing/plumbing.proto` | 4 |

All 9 protos parse under the existing `-I .` protoc invocation in
`build.sh`; a quick `protoc … gluon/plumbing/plumbing.proto` produced
valid `plumbing_grpc.pb.go` / `plumbing.pb.go` files that reference the
existing `genpb.SubCommandReq` / `genpb.RepoMsg` via import.

## Why not drive this off gluon v1/v2 directly

`accretional/gluon`'s `codegen` package (v1) generates `.proto` files
from a Go interface: `OnboardSource(src) → ServiceBundle{Proto, …}`.
That's the right overall shape, but `GenerateProto` / `GeneratePackageProto`
emit self-contained protos — every referenced struct is inlined as a
proto message in the same file, with no `import` directives. For the
nine packages we need here, every RPC must reference the *existing*
`repo.RepoMsg` and `subcommands.SubCommandReq` types so the new services
can ride on the same runner plumbing as `subcommands/`. Gluon's output
shape would duplicate those types nine times.

`accretional/gluon` v2 is a pipeline for parsing EBNF grammars into
`FileDescriptorProto` — orthogonal to this task.

`accretional/proto-expr`'s `Protosh` is a scripting runtime for chaining
RPC handlers; also orthogonal.

So `cmd/stubgen` is a ~200-line direct generator that mimics gluon's
naming conventions (CamelCase RPC names, snake-case file stems,
one proto per service) and emits the right `import` statements. When
gluon grows a "reference external proto types" mode, this generator is
a 5-minute port.

## What's still TODO before these compile into the server

These protos aren't yet part of the main build. To wire them in:

1. Extend `build.sh` to protoc-compile everything in `gluon/*.proto`
   alongside the root protos.
2. Export the runner from `subcommands/` (`subcommands.Run` /
   `subcommands.RunMkdir`) so the new per-section servers can delegate
   without reimplementing resolution + exec.
3. Add one `gluon/<pkg>/<cmd>.go` per RPC — each a three-line
   delegator. Another codegen pass against the same CSVs can generate
   these once step 2 lands.
4. Register each service in `cmd/importerd/main.go`.

The README at the repo root flagged all of this as "hold off until gluon
is ready." The `.proto` emission is the easy half; exporting the
runner + generating stubs is the second pass.
