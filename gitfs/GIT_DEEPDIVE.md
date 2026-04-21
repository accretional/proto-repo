# Git on-disk format — deep dive

A working reference for the .git directory, written against this repo's own
`.git` so every example is verifiable with `xxd`, `git cat-file`, and a few
lines of Python.

The companion file is [`gitfs.proto`](./gitfs.proto): a 1:1 protobuf encoding
of every structure described below, with a parser + tests in this directory
that load real files from this repo's `.git` and assert the encoded values.

> Authoritative spec: <https://git-scm.com/docs/gitformat-pack>,
> <https://git-scm.com/docs/index-format>, and the `Documentation/technical/`
> directory in the git source tree. This document focuses on the shape that
> matters for *reading* a repository, not constructing one.

---

## 1. The .git directory

```
.git/
  HEAD                       # symbolic ref → refs/heads/<current>
  config                     # INI-style configuration
  description                # bare-repo human-readable name (gitweb)
  index                      # binary staging area (DIRC)
  COMMIT_EDITMSG             # last commit message buffer
  packed-refs                # compact ref store (header + sorted lines)
  hooks/                     # *.sample shell hook templates
  info/
    exclude                  # repo-local gitignore
  logs/
    HEAD                     # reflog of HEAD
    refs/heads/<name>        # per-branch reflog
    refs/remotes/<r>/<name>
  objects/
    XX/<38-hex>              # loose object (zlib-compressed)
    pack/pack-<sha>.pack     # packfile
    pack/pack-<sha>.idx      # index over the packfile
    info/                    # alternates etc.
  refs/
    heads/<name>             # 41-byte file: <40-hex sha>\n
    tags/<name>
    remotes/<remote>/<name>
```

Snapshot from this repo at HEAD `17ba27ac…`:

```
.git/HEAD                          → "ref: refs/heads/main\n"
.git/refs/heads/main               → "17ba27acbb1af59a2e088ecd786053e706331c29\n"
.git/refs/remotes/origin/HEAD      → "ref: refs/remotes/origin/main\n"
.git/refs/remotes/origin/main      → "17ba27acbb1af59a2e088ecd786053e706331c29\n"
.git/packed-refs                   → "# pack-refs with: peeled fully-peeled sorted \n"
                                     "96fa7c79… refs/remotes/origin/main\n"
.git/objects/                      → 149 fanout dirs (XX/) + pack/ + info/
.git/objects/pack/pack-728a9f8b…   → 38 packed objects, version 2
.git/index                         → DIRC v2, 114 entries, 11208 bytes
.git/logs/HEAD                     → 6 reflog entries (clone + 5 commits)
```

Two sources of truth coexist:

1. **Loose objects** under `objects/XX/<rest>` — one zlib-compressed file per
   object, named by the object's SHA-1.
2. **Packfiles** under `objects/pack/` — many objects bundled into one
   `.pack` file with a side `.idx` for O(log n) lookup. `git gc` (or `git
   clone` for the initial fetch) creates these.

A repo can hold the *same* object loose AND packed at once. Readers must
check both; this repo currently has both (the clone produced one pack;
subsequent commits land loose).

---

## 2. The object store

Every object has the same uncompressed framing:

```
<type> SP <size-as-decimal-string> NUL <body>
```

The SHA-1 of that *uncompressed* blob (header included) is the object name.
The on-disk file is `zlib.compress(<that whole thing>)`. Four types: `blob`,
`tree`, `commit`, `tag`.

Verifying it with this repo's HEAD commit:

```
$ python3 -c "import zlib; \
    print(zlib.decompress(open('.git/objects/17/ba27ac…','rb').read())[:80])"
b'commit 1036\x00tree 14858f70…\nparent 752ad622…\nauthor Fred <…> 1776334774 -0700\n…'
```

`hashlib.sha1(<uncompressed bytes>).hexdigest()` = `17ba27ac…`. Match.

### 2.1 Blob

Body is uninterpreted bytes — a snapshot of file content. No filename, no
mode, no permissions; those live in the *tree* that points to the blob.
Two files with identical content share one blob.

### 2.2 Tree

Body is a sequence of entries with **no separator**:

```
<mode-as-octal-ascii> SP <name> NUL <20 raw bytes of SHA-1>
```

`mode` distinguishes `100644` (file), `100755` (executable file),
`120000` (symlink), `040000` (sub-tree), `160000` (gitlink → submodule).
Note the tree stores the SHA-1 *binary*, not hex — 20 bytes, not 40.

This repo's HEAD tree (`14858f70…`) has 29 entries. First three:

```
100644 .gitignore                f1b08e5e9717c0951e521ec5a10e1b1e4e5dff3c
100755 GLUON_EXAMPLE.sh          635068c9823a1b3880a8c132b415d6e2acf96a98
100755 LET_IT_RIP.sh             0bca1f6a143b7ea87e645ff7c8fccdd607b00759
```

Entries are sorted by name with sub-tree names treated as if they had a
trailing `/` (this affects how `cmd` sorts vs `cmd.go` etc.). Tree hashing is
sensitive to this ordering — tools that build trees must match git's sort.

### 2.3 Commit

Body is line-oriented headers, a blank line, then the message:

```
tree <40-hex>
parent <40-hex>            # zero (root), one (normal), or many (merge)
author <name> <<email>> <unix-ts> <tz>
committer <name> <<email>> <unix-ts> <tz>
[gpgsig -----BEGIN PGP SIGNATURE-----            # optional, multi-line
 …                                                # continuation lines
                                                  #   start with a single
                                                  #   leading space (RFC 822)
 -----END PGP SIGNATURE-----]
[encoding <iana-name>]
                                                  # blank line
<message body, free-form bytes>
```

Multi-line header values are folded by leading-space continuation, so a
parser must look for a header *line* whose first column is non-space and
fold subsequent space-prefixed lines into the value.

### 2.4 Tag (annotated)

Same line-oriented shape as commit but with different headers:

```
object <40-hex>          # what the tag points at
type <object type>       # commit | tag | tree | blob
tag <name>
tagger <name> <<email>> <unix-ts> <tz>

<message>
```

Lightweight tags (the common case) are just a `refs/tags/<name>` file
holding a sha; no tag object exists.

---

## 3. Refs

A ref maps a name like `refs/heads/main` to either a **direct** SHA-1 or a
**symbolic** pointer to another ref.

### 3.1 Loose refs

A loose ref is a single file containing `<40-hex>\n`. A symbolic loose ref
is `ref: <target>\n`. `.git/HEAD` is the canonical symbolic ref:

```
$ cat .git/HEAD
ref: refs/heads/main
```

### 3.2 Packed refs

To avoid millions of tiny files in long-lived repos, `git pack-refs` bundles
loose refs into a sorted text file:

```
# pack-refs with: peeled fully-peeled sorted
96fa7c79aaba0a656d9f1bf23da5a19ce818bf72 refs/remotes/origin/main
^<peeled-sha>                # OPTIONAL line — for annotated tags only,
                              # gives the underlying commit SHA so peeling
                              # to commit doesn't require reading the tag
                              # object.
```

A loose ref of the same name **shadows** the packed entry (loose wins). When
you delete a ref, both stores must be cleaned.

### 3.3 Resolving a ref

```
HEAD → "ref: refs/heads/main"
  → look up refs/heads/main:
     loose .git/refs/heads/main? yes → 17ba27ac…  (done)
     else packed-refs entry?     → that sha
```

Symbolic refs may chain (HEAD → some other symref) but in practice are one
hop. Resolution must guard against cycles.

---

## 4. The index

The staging area: a single binary file at `.git/index` describing the next
commit's tree before it's committed.

```
4 bytes  signature   "DIRC"  (dircache)
4 bytes  version     2, 3, or 4
4 bytes  num entries
N×       entry       (variable length, see below)
*        extensions  (TREE cache, REUC, link, FSMN, etc.)
20 bytes SHA-1 of all preceding bytes
```

This repo's index: `DIRC` v2, 114 entries, 11208 bytes, trailing SHA
`81920e24…`.

### 4.1 Index entry (v2/v3)

```
ctime_seconds      uint32   # stat() ctime, file creation
ctime_nanoseconds  uint32
mtime_seconds      uint32   # stat() mtime
mtime_nanoseconds  uint32
dev                uint32   # stat() device
ino                uint32   # stat() inode
mode               uint32   # 0o100644 / 0o100755 / 0o120000 / 0o160000
uid                uint32
gid                uint32
size               uint32
sha1               20 bytes
flags              uint16   # high bit = assume-valid; next = extended (v3+);
                            # next 2 = stage (0 normal; 1/2/3 = merge sides);
                            # low 12 bits = name length (capped at 0x0FFF)
[ext_flags         uint16]  # only if extended bit set (v3+)
name               null-terminated, then padded with NULs so total entry
                            # length is multiple of 8 bytes (v2/v3 only)
```

First entry from this repo's index (parsed live):

```
mode=0o100644  size=884  sha=f1b08e5e9717c0951e521ec5a10e1b1e4e5dff3c
name=".gitignore"
```

Stat fields let `git status` skip rehashing files whose ctime/mtime/size
match what's on disk. They are advisory — not part of git's content
identity.

### 4.2 Extensions

After all entries, optional sections each starting with a 4-byte signature:

| Sig    | Meaning                                                |
|--------|--------------------------------------------------------|
| `TREE` | Cached tree hashes by directory — speeds up commit.    |
| `REUC` | Resolved unmerged-conflict cache.                      |
| `link` | Split index pointer (large repos).                     |
| `FSMN` | Filesystem-monitor cookie.                             |
| `UNTR` | Untracked-files cache.                                 |
| `EOIE` | End-of-index marker (offsets for parallel parsers).    |
| `IEOT` | Index entry offset table.                              |

Extensions whose signature starts with an uppercase letter are *required*
for older git versions to refuse the index; lowercase signatures are
optional. A reader can safely skip unknown lowercase ones.

### 4.3 Version 4 path compression

Index v4 prefix-compresses entry names against the previous entry. Out of
scope for the parser here (this repo is v2), but the proto schema permits
it.

---

## 5. Reflog

Per-ref append-only log of where the ref pointed and why it moved.

```
.git/logs/HEAD
.git/logs/refs/heads/<name>
.git/logs/refs/remotes/<r>/<name>
```

Each line:

```
<old-sha> SP <new-sha> SP <committer> TAB <message>\n
```

`<committer>` is the same `Name <email> unix-ts tz` shape as commit
headers. `<old-sha>` is `0000…0000` for ref creation; `<new-sha>` is
`0000…0000` for ref deletion. The TAB separator before the message is
load-bearing — message bodies may contain spaces freely.

This repo's `.git/logs/HEAD`, first line:

```
0000…0000 96fa7c79… Fred <…> 1776312788 -0700	clone: from https://…
```

Reflog is local-only — never pushed. It powers `git reflog`, `HEAD@{n}`,
and is git's safety net for "I lost a commit" recovery.

---

## 6. Packfiles

Loose-only storage costs one zlib-compressed file per object plus an inode.
Pack format consolidates many objects into one file with delta compression.

### 6.1 Pack file (`.pack`)

```
4 bytes  magic       "PACK"
4 bytes  version     2 or 3
4 bytes  object count
N×       packed object
20 bytes SHA-1 of all preceding bytes
```

This repo's pack: magic `PACK`, version 2, 38 objects.

A packed object's variable-length header encodes type (3 bits) and size
(MSB-continuation varint, low 4 bits in the first byte then 7 bits per
continuation). Types 1-4 are `commit/tree/blob/tag`; types 6 and 7 are
deltas (against an earlier object in the pack by negative offset, or
against an arbitrary object by SHA respectively); type 5 is reserved.

Body is zlib-compressed. Deltas decode to a copy/insert script applied
against the base object's bytes.

### 6.2 Pack index (`.idx`)

Lookup table by SHA. Version 2 layout:

```
4 bytes  magic         "\xfftOc"   (\377tOc)
4 bytes  version       2
256×4    fanout table  count of objects with first byte ≤ i
N×20     SHA-1s        sorted
N×4      CRC32s        of the packed data per object
N×4      offsets       into the .pack (high bit set ⇒ index into 64-bit
                       table for packs > 4 GiB)
[M×8     large offsets]
20 bytes pack file's SHA-1
20 bytes idx file's SHA-1 (over preceding bytes)
```

This repo's idx: `\xfftOc` v2, 38 entries, 2136 bytes.

The fanout table makes lookup O(log n) within a 1/256 slice rather than
O(log n) over the full table — a constant-factor win that matters at
million-object scale.

---

## 7. Config

Standard INI-ish format:

```
[section]
    key = value
[section "subsection"]
    key = value
[section "subsection with quotes"]
    key = "value with spaces"
```

This repo's `.git/config`:

```ini
[core]
    repositoryformatversion = 0
    filemode = true
    bare = false
    logallrefupdates = true
    ignorecase = true
    precomposeunicode = true
[remote "origin"]
    url = https://github.com/accretional/proto-repo.git
    fetch = +refs/heads/*:refs/remotes/origin/*
[branch "main"]
    remote = origin
    merge = refs/heads/main
```

Config has hierarchy: `/etc/gitconfig` → `~/.gitconfig` → repo `.git/config`
→ in-tree `.gitattributes` (for some keys). The proto here models a single
file's contents; merging is a layer above.

`repositoryformatversion = 0` declares the "classic" format. Version 1
allows extension keys under `[extensions]` (e.g., `partialclone`,
`worktreeConfig`, `objectFormat = sha256`). A reader that doesn't recognize
an extension must refuse to operate on that repo.

---

## 8. Hooks, info/exclude, COMMIT_EDITMSG, ORIG_HEAD, MERGE_*

- **`hooks/*.sample`** — shipped templates; rename to `<event>` (no suffix)
  and `chmod +x` to enable. Not part of repo content; lives only locally.
- **`info/exclude`** — local gitignore overlay, never tracked or pushed.
- **`COMMIT_EDITMSG`** — buffer the editor wrote during the last commit;
  always overwritten on next commit.
- **`ORIG_HEAD`** — set by destructive ops (reset, merge, rebase) to the
  previous HEAD so `git reset --hard ORIG_HEAD` can undo.
- **`FETCH_HEAD`** — refs fetched in the most recent `git fetch`.
- **`MERGE_HEAD`, `MERGE_MSG`, `MERGE_MODE`** — present only mid-merge.
- **`SHALLOW`** — list of shaft-end SHAs in a shallow clone.

These are *operational state* rather than repo content — useful to capture
in a snapshot but not in a fetch-and-replay model.

---

## 9. Identity guarantees

A repository's *content* is the closure of:

```
HEAD → commit → (tree + parents) → trees + blobs (recursively)
```

Plus every ref that's not reachable from HEAD (other branches, tags). All
hashed by SHA-1 (or SHA-256 with the experimental sha256 object format),
making content addressable and tamper-evident.

A repository's *state* additionally includes:

```
config, hooks, index, reflog, ORIG_HEAD/FETCH_HEAD/MERGE_*, info/exclude
```

These are local-only, not transferred by clone/push/fetch (with one
exception: server-side hooks).

`gitfs.proto` separates these two layers: `Object` and `Ref` model content;
`Index`, `Reflog`, `Config`, and the `Repository.local_state` block model
operational state. A serialized `Repository` message can faithfully
round-trip the directory at one moment in time.

---

## 10. What this directory implements

| File                  | Purpose                                                      |
|-----------------------|--------------------------------------------------------------|
| `gitfs.proto`         | Protobuf encoding of every structure above                   |
| `parser.go`           | Parser: reads a real `.git/` into `*pb.Repository`           |
| `parser_test.go`      | Tests assert parsed values match this repo's `.git`          |

The parser is a *reader*, not a writer — it produces protos from on-disk
git, not the other way around. Sufficient to validate the schema's shape;
writing protos back to a working `.git` would also need to compute SHAs,
zlib-encode objects, and pack-resolve deltas (deferred — see proto comments
for the format details a writer would need).
