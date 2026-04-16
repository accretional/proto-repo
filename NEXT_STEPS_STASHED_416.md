# Stashed next steps — 2026-04-16

Parking lot for the follow-on work queued after the stubgen commit
(`752ad62`). Nothing below is blocking; resume in the order listed
unless the gluon issue resolves the first dependency.

## Goal

Make the nine generated services under `gluon/` actually callable from
`importerd`, completing the TODO section in `README.md`. Today the
`.proto` files exist and compile, but no Go server implements them and
`importerd` doesn't register them.

## Concrete steps (in order)

1. **Export the shared runner from `subcommands/`.**
   Rename `Server.run` → `Server.Run` and `Server.runMkdir` → `Server.RunMkdir`
   in `subcommands/server.go`. That's the only change inside the subcommands
   package; the existing per-command files keep calling the renamed methods.
   Chose export over adding a new shim so there's only one path.

2. **Extend `cmd/stubgen` with a second pass that emits `<cmd>.go` delegators.**
   One file per RPC, ~4 lines each:

   ```go
   package plumbing

   import (
       "context"

       pb "github.com/accretional/proto-repo/genpb"
       "github.com/accretional/proto-repo/subcommands"
   )

   type Server struct {
       sub *subcommands.Server
       // plus the generated pb.UnimplementedPlumbingServer embedded in one
       // shared plumbing.go, not here
   }

   func (s *Server) Version(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
       return s.sub.Run(ctx, req.GetRepo(), "version", req.GetArgs()...), nil
   }
   ```

   126 files total across the nine packages. One shared `<pkg>.go` per
   package declares the `Server` struct + constructor; stubgen emits
   that too. Add tests for the delegator generator (parse back one of
   the emitted files, assert it references the expected subcommand
   name).

3. **Wire the gluon protos into `build.sh`.**
   Currently `build.sh` compiles only the four root-level protos. Add
   `gluon/*/*.proto` to the protoc invocation so `gluon/<pkg>/pb/`
   lands in the module. `GLUON_EXAMPLE.sh` does this already; copy
   the relevant lines over. Keep `gluon/*/pb/` gitignored — regenerating
   is cheap and the committed `.proto` files are the source of truth.

4. **Register the nine services in `cmd/importerd/main.go`.**
   One `pb.Register<Pkg>Server(srv, <pkg>.New(subSrv))` line per
   package, alongside the existing Importer and SubCommands
   registrations. `subSrv` is the shared `subcommands.Server` —
   constructing it once and passing it into each per-section server
   keeps the runner singleton (and shares `ScratchDir`).

## Decision point

Issue https://github.com/accretional/gluon/issues/1 requested an
`external types` mode for `codegen.OnboardSource` so gluon can generate
these protos directly instead of stubgen doing it by hand. If that
lands before step 2 starts, collapse step 2 (and stubgen entirely) into
a call to gluon's codegen and skip the hand-written generator. If it
doesn't land, proceed with stubgen.

## What's ready to build on

- `gluon/<pkg>/<pkg>.proto` × 9 — compiled via `GLUON_EXAMPLE.sh`;
  commit `91cd6b4`.
- `cmd/stubgen/main.go` — the CSV-driven proto generator, with tests;
  commit `91cd6b4`.
- `GLUON_EXAMPLE.sh` — end-to-end pipeline (regen + protoc + go build +
  `--check` mode); commit `752ad62`.

## Estimated size

Steps 1 + 3 + 4 are each ~20 lines. Step 2 is ~150 lines of generator
code + 126 one-off generated files. Whole effort is a half-day sitting.
