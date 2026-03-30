# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added

- **`SETOF` (set-returning functions)** — Functions can now return multiple rows using `plgo.SetOf[T]`:
  - New generic type `plgo.SetOf[T any]` — user returns a slice, plgo generates the SRF protocol
  - `SetOfFunction` code generator — emits `RETURNS SETOF <type>` SQL and SRF wrapper code
  - Full PostgreSQL SRF C bridge: 12 helper functions wrapping `funcapi.h` macros (`SRF_IS_FIRSTCALL`, `SRF_RETURN_NEXT`, `SRF_RETURN_DONE`, etc.)
  - Go-side SRF helpers: `srfIsFirstCall`, `srfInit`, `srfNext` with proper PG memory context management
  - Supports all scalar element types: `SetOf[string]`, `SetOf[int32]`, `SetOf[float64]`, `SetOf[bool]`, `SetOf[[]byte]`, etc.
  - 31 new unit tests for SETOF (AST parsing, SQL generation, code generation, visitor)
  - Integration tests: `GenerateInts(5)` → 5 rows, `GenerateWords()` → 4 rows, empty set → 0 rows
  - Example functions: `GenerateSeries`, `RepeatString`
- **Go modules support** — Project now uses `go.mod` with `go 1.22` minimum; no more `GOPATH` dependency
- **PostgreSQL 18 compatibility** — Verified and tested against PostgreSQL 18 C API
- **Docker-based integration tests** — Multi-stage `Dockerfile` (Go 1.26 builder + PG 18 runner) with SQL test harness
- **Comprehensive unit tests** — 129 test cases (38 top-level + 91 subtests) covering:
  - AST parsing for all scalar and array types as both params and returns
  - SQL `CREATE FUNCTION` generation for every type combination
  - Code generation including `//export` directives and `fcinfo.Scan` patterns
  - Trigger function classification and code generation
  - Visitor correctness (`Remover` strips `plgo` imports/selectors)
  - `datumTypes` map completeness
- **`make fmt` target** — Runs `gofumpt` on Go files and `pg_format` on SQL files
- **SQL test harness** — `test/sql/` scripts with `test/expected/` output files, diff-based verification
- **Trigger integration test** — `CreatedTimeTrigger` is now actually tested end-to-end (CREATE TABLE → CREATE TRIGGER → INSERT → verify)
- **New example functions** — `AddDays` (time.Time), `ScaleArray` ([]int64), `MaybeUpper` (*string nullable return)

### Changed

- **Directory restructure** — CLI tool moved from `plgo/` to `cmd/plgo/` (standard Go project layout)
- **Replaced deprecated `io/ioutil`** — Migrated to `os.ReadFile`, `os.WriteFile`, `os.MkdirTemp`
- **Updated build tags** — Changed `// +build` to `//go:build` syntax
- **`readPlGoSource()` reworked** — Now uses `go list -m -json` to locate `pl.go` in the module cache (works with Go modules, not just `GOPATH`)
- **Install instructions** — Changed from `go get -u` to `go install gitlab.com/microo8/plgo/cmd/plgo@latest`
- **`Makefile`** — New root Makefile with `build`, `install`, `test`, `test-unit`, `test-integration`, `fmt`, `clean` targets
- **`Dockerfile`** — Updated to Go 1.26 + PostgreSQL 18
- **Documentation** — Complete README rewrite with quick start, type tables, SPI API reference, trigger guide, architecture overview
- **Godoc comments** — All exported types and functions in `pl.go` now have comprehensive godoc comments with examples

### Removed

- `test/bgw/` — Broken background worker experiment (hardcoded PG paths, never tested in CI)
- `test/types/` — Broken custom type experiment (raw CGo bypassing plgo, never tested)
- `test/plgo_test.sql` — Old manual SQL file, superseded by `test/sql/` scripts
- `generate_windows/` — Unmaintained PowerShell MSVC-era script
- `test/runtest.sh` — Replaced by `Makefile` targets and Docker-based test runner

