# filelist2db — Copilot Agent Instructions

Trust these instructions first. Only search the codebase yourself if information here is
missing or you find it to be incorrect for your specific change.

## What this repo is

`filelist2db` is a small **Go** command-line tool that reads one or more "file list" text
files (typically produced by a Unix `find ... -printf` command, one line per file with
size/uid/inode/mtime/atime/ctime/path fields) and imports them into a **SQLite** database.
After import it can rebuild summary tables: a per-directory cumulative size/file-count
summary and a file-size histogram ("bin") table. There is no server/UI component — it's a
single binary invoked from the shell.

- Language: Go (module `github.com/524D/filelist2db`, `go.mod` declares `go 1.25.0`; the
  installed toolchain in this environment is `go1.27.0`, which is backward compatible).
- Size: very small repo — ~2,700 lines of Go across 6 files, no other languages/frameworks.
- Runtime deps: `github.com/schollz/progressbar/v3` (progress bars), `modernc.org/sqlite`
  (pure-Go/cgo-free SQLite driver — no CGO or system SQLite library required).
- No web framework, no REST API, no external services.

## Repository layout

- `main.go` — CLI entry point: flag parsing (`-db`, `-build-dir-summary`,
  `-build-bin-table`, `-cpuprofile`), file-list line parsing (regex-based), filename
  decoding (extracts computer/share name, base path, scan timestamp from the list
  filename), and the main import loop.
- `main_test.go` — the **only** test file in the repo (707 lines, all tests are in
  package `main` at the repo root, not in subpackages). Tests cover filename decoding,
  DB import, dir-summary rebuild, and search, using `datafillersqlite` +
  `dataprovidersqlite` together against temp SQLite files.
- `dataprovider/dataprovider.go` — defines the read-side interface `DataProvider` and
  shared types (`FileInfo`, `SearchResult`, `SearchSelection`, `SubDirStats`, `TimeBin`,
  `UidT`). Pure types/interfaces, no logic.
- `datafiller/datafiller.go` — defines the write-side interface `DataFiller`
  (`SetSourceInfo`, `AddFile`, `RebuildDirTable`, `RebuildBinTable`, transactions,
  `Finalize`). Pure interface, no logic.
- `datafillersqlite/datafillersqlite.go` (~926 lines) — SQLite implementation of
  `DataFiller`: schema creation (`CREATE TABLE`/`CREATE INDEX` statements), batched
  inserts, path-element interning tables (`path`, `path_elem`, `simple_path_elem`,
  `simple_path_translate`) used for fast/simplified-name search, dir/bin table rebuild
  logic.
- `dataprovidersqlite/dataprovidersqlite.go` (~627 lines) — SQLite implementation of
  `DataProvider`: prepared read-only queries, `Search`, `SearchBySimpleName`,
  `DirInfo`, `DataSources`.
- `testdata/` — sample `.lst` file(s) used by `run.sh` and available for tests. NOTE:
  `.gitignore` lists `/testdata`, so this directory's _contents_ are not tracked by git
  even though a sample file currently exists on disk — don't assume new files placed
  there will be committed.
- `run.sh` — a manual/local dev script (not CI) that builds the binary, runs it against
  sample data with CPU profiling enabled, and opens `go tool pprof` in a browser. Not
  usable in a headless/agent environment (it launches `chrome` and needs local data
  files outside the repo) — do not attempt to run it as a validation step.
- `README.md` — short usage examples for the CLI flags.
- `TODO.txt` — informal backlog of planned features/bugs (not actionable tasks unless
  explicitly referenced).
- `.vscode/launch.json` — two local debug configs (VS Code "Go" debugger), reference
  paths outside the repo; not relevant to CI/agent validation.
- No `.github/workflows` exist yet (no CI pipeline is configured in this repo) and there
  are no Makefiles, linters configs (e.g. `.golangci.yml`), or Dockerfiles.
- Build artifacts/scratch files you will see locally but must **not** commit or rely on:
  `*.sqlite`, `*.sqlite-journal`, `*.exe`, `cpuprofile*.prof`, `__debug_bin*.exe` — all
  covered by `.gitignore`. `cpuprofile_0.prof`, `cpuprofile_0_2.prof`, `db*.sqlite`,
  `filelist2db.exe`, `__debug_bin.exe` etc. may appear untracked in a working tree from
  previous local runs; ignore them.

## Build, test, and validation — verified commands

Run all commands from the repo root. No environment setup beyond a working Go toolchain
is required (no env vars, no config files, no external services, no network access
needed at build/test time beyond initial module download which is already vendored via
`go.sum`).

1. **Build** (verified working, no output on success):
   ```
   go build ./...
   ```
2. **Vet / static check** (verified working, no issues currently):
   ```
   go vet ./...
   ```
3. **Test** (verified working — takes ~4s on a clean tree):
   ```
   go test ./...
   ```
   Expected output: `ok github.com/524D/filelist2db ...` plus `?  ... [no test files]`
   lines for `datafiller`, `datafillersqlite`, `dataprovider`, `dataprovidersqlite` (all
   tests live in the root `main` package). This is normal, not a failure.
4. **Format check** (verified — currently reports pre-existing formatting issues that
   are not your responsibility to fix unless your task touches those files):
   ```
   gofmt -l .
   ```
   As of onboarding, this lists `dataprovider/dataprovider.go`,
   `dataprovidersqlite/dataprovidersqlite.go`, and `main.go` as not gofmt-clean. If you
   edit one of these files, run `gofmt -w <file>` on the lines you touched rather than
   reformatting the whole file, to keep diffs minimal.
5. There is no lint config, no CI workflow, and no pre-commit hook in this repo — `go
build ./...`, `go vet ./...`, and `go test ./...` are the full validation surface.
   Treat all three passing as sufficient confidence for a change.

### Known pitfalls observed during validation

- Building the CLI writes a `filelist2db.exe`/`filelist2db` binary into the repo root;
  it's gitignored, safe to leave or delete.
- `go test ./...` creates SQLite files under the OS temp dir via `t.TempDir()`; on
  Windows you may see a harmless `TempDir RemoveAll cleanup: ... process cannot access
the file` warning in test output if a SQLite connection wasn't closed before cleanup —
  this does not fail the test run itself unless it's the actual assertion that failed.
- Do not run `run.sh` in an automated/headless environment — it depends on local data
  files outside the repo and launches a browser.

## Making changes efficiently

- The read/write split is intentional: `dataprovider`/`datafiller` are storage-agnostic
  interfaces; `dataprovidersqlite`/`datafillersqlite` are the (only) implementations.
  When adding a new field or query capability, update the interface first, then the
  SQLite implementation, then callers in `main.go`, then add/adjust tests in
  `main_test.go`.
- SQL schema (table/index creation) lives entirely in
  `datafillersqlite/datafillersqlite.go` inside `createTables` — search for `CREATE
TABLE` / `CREATE INDEX` there rather than looking for migration files (there are
  none; schema is created idempotently with `IF NOT EXISTS` on every run).
- File-list line parsing and filename decoding (`fileListLineRE`, `findFilenameRE`) are
  in `main.go`; if changing the accepted `.lst`/`.txt` filename or line format, update
  both the regex and the corresponding tests in `main_test.go`
  (`TestDecodeFindFilename*`-style cases and `parseFileInfoLine` coverage).
- Progress reporting uses `github.com/schollz/progressbar/v3`; follow the existing
  pattern (`progressbar.NewOptions(100, ...)` + a `progress(current, total int64)`
  closure) if adding a new long-running batch operation.
- Batch/commit sizing conventions already in the code: transactions commit every 10,000
  lines during import (`main.go`), and dir/bin table rebuilds take an explicit
  `batchSize` parameter (100,000 is used by `main.go`'s callers).
