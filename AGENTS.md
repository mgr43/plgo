# AGENTS.md — plgo

## What This Project Is

plgo is a **code generator and runtime library** for writing PostgreSQL extensions (stored procedures/triggers) in Go. It has two main components:

1. **CLI tool** (`cmd/plgo/` directory) — Parses a user's Go package, generates CGo wrapper code, builds a `.so` shared object, and emits PostgreSQL extension files (SQL, Makefile, `.control`). With `--installer`, also produces a self-contained `.sh` script for zero-dependency deployment.
2. **Runtime library** (`pl.go` at root) — A single-file CGo bridge between Go and PostgreSQL's C API (SPI, Datum conversion, elog, triggers). This file is **copied into the generated build**, not linked as a normal Go dependency.

## Architecture & Code Generation Pipeline

The CLI (`cmd/plgo/plgo.go` → `main()`) runs this pipeline:

1. **Parse** user's Go package with `go/ast` — `modulewriter.go:NewModuleWriter()` invokes `FuncVisitor` (`visitors.go`) to collect all **exported functions** as `CodeWriter` objects.
2. **Classify** each function into `VoidFunction`, `Function`, `TriggerFunction`, or `SetOfFunction` (`functions.go:NewCode()`). Classification depends on params/return types: trigger functions must accept `*plgo.TriggerData` and return `*plgo.TriggerRow`; set-returning functions return `plgo.SetOf[T]`.
3. **Generate** three files into a temp directory:
   - `package.go` — user's code with `plgo` imports removed (via `Remover` AST visitor) and exported functions renamed to `__FuncName`
   - `pl.go` — copy of root `pl.go` with package changed to `main`, include paths adjusted via `pg_config`, and `PG_FUNCTION_INFO_V1` macros injected at the `//{funcdec}` placeholder
   - `methods.go` — CGo `//export` wrappers that call `__FuncName` with Datum↔Go conversion
4. **Build** with `go build -buildmode=c-shared` → produces `.so`/`.dll`
5. **Emit** extension files into `build/`: SQL (`CREATE FUNCTION`), `.control`, `Makefile`
6. **Optionally** (`--installer` flag): bundle `.so` + `.sql` + `.control` into a self-contained `.sh` installer script via `WriteInstaller()` — the `.so` is base64-encoded, and the shell header auto-detects `pg_config` on the target

## Key Files

| File                                             | Role                                                                                                                                                                          |
| ------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `pl.go`                                          | Runtime: CGo bridge with all Datum↔Go conversions, SPI wrappers, trigger support. **Template** — gets `//{exportedfuncs}` and `//{pgmodulemagic}` replaced during generation |
| `cmd/plgo/plgo.go`                               | CLI entry point — orchestrates parse → generate → build → emit                                                                                                                |
| `cmd/plgo/modulewriter.go`                       | Orchestrates code generation; writes SQL/control/Makefile                                                                                                                     |
| `cmd/plgo/functions.go`                          | `CodeWriter` types — generate wrapper Go code and SQL `CREATE FUNCTION` statements. Contains `datumTypes` map (Go→PG type mapping)                                            |
| `cmd/plgo/visitors.go`                           | AST visitors: `FuncVisitor` (collects exported funcs), `Remover` (strips `plgo` imports/selectors)                                                                            |
| `cmd/plgo/pathnames.go` / `pathnames_windows.go` | Platform-specific path handling and `pg_config` integration                                                                                                                   |
| `cmd/plgo/plgo_test.go`                          | Comprehensive unit tests (160 test cases) — AST parsing, code generation, SQL output, type coverage, SETOF                                                                    |
| `cmd/plgo/installer.go`                          | Self-contained `.sh` installer generator: shell template, `WriteInstaller()`, base64 payload encoding                                                                         |
| `cmd/plgo/installer_test.go`                     | Installer unit tests: base64 round-trip, template syntax validation (`sh -n`), full `WriteInstaller` round-trip                                                               |
| `integration/integration_test.go`                | Testcontainers-based integration test setup: builds Docker image, starts PG 18 container, provides shared `*sql.DB`                                                           |
| `integration/extension_test.go`                  | Integration test functions: 13 tests covering extension loading, function calls, triggers, SPI, SETOF                                                                         |
| `integration/installer_test.go`                  | Installer integration tests: 9 subtests verifying install, `--create-extension`, and `--uninstall` in a clean PG container                                                    |
| `integration/Dockerfile`                         | Multi-stage Docker image: builds plgo CLI + extensions from source, installs into PG 18                                                                                       |
| `integration/Dockerfile.installer`               | Clean PG 18 image with only installer `.sh` scripts (no pre-installed extensions) for installer tests                                                                         |
| `example/example_methods.go`                     | Reference for valid user code patterns (void, scalar, array, bytea, trigger, nullable returns, SETOF)                                                                         |
| `test/plgotest.go`                               | Integration test functions that run inside PostgreSQL (SPI, type conversions)                                                                                                 |
| `Dockerfile`                                     | Build verification image: Go 1.26 builder, runs unit tests, builds extensions                                                                                                 |
| `Makefile`                                       | Root Makefile with build, install, test-unit, test-integration, fmt, clean targets                                                                                            |
| `CHANGELOG.md`                                   | Documents all changes from the modernization effort                                                                                                                           |

## Supported Go↔PostgreSQL Type Mappings

Defined in `cmd/plgo/functions.go:datumTypes`. Supported Go types: `string`, `[]byte`, `int16`–`int64`, `float32/64`, `bool`, `time.Time`, and their array variants. Only single return values are allowed (no multiple returns). Pointer return types (`*string`, `*int64`, etc.) enable SQL NULL returns. `plgo.SetOf[T]` enables `RETURNS SETOF <type>` for set-returning functions.

## Build & Test Workflow

```sh
# Install the CLI tool
make install    # or: cd cmd/plgo && go install .

# Build an extension from a Go package
plgo [path/to/package]    # generates build/ directory

# Build with self-contained installer script
plgo --installer [path/to/package]    # also produces install-<ext>.sh

# Run unit tests (no database needed) — 160 test cases
make test-unit

# Run integration tests (requires Docker — uses testcontainers-go)
make test-integration
# or directly: go test -tags integration -v -timeout 5m ./integration/

# Format Go files
make fmt

# Clean build artifacts
make clean
```

**Prerequisite:** `postgresql-server-dev-X.Y` package and `pg_config` in PATH (for local builds), or Docker (for integration tests).

## Integration Test Architecture

Integration tests live in `integration/` and use [testcontainers-go](https://golang.testcontainers.org/) to automate the full pipeline:

1. `integration/Dockerfile.test` — Multi-stage Docker build: compiles plgo CLI from source, builds both `example` and `test` extensions, installs them into a `postgres:18` runtime image.
2. `integration/integration_test.go` — `TestMain` uses testcontainers to build the Docker image and start the container. A shared `*sql.DB` connection is provided to all test functions.
3. `integration/extension_test.go` — 13 test functions that run SQL assertions via `database/sql` (extension loading, function results, trigger behavior, SETOF row counts).

Tests are gated behind `//go:build integration` so `go test ./...` skips them. Run with `go test -tags integration ./integration/`.

## Conventions & Patterns

- User code **must be `package main`** — the parser explicitly checks for this (`modulewriter.go:55`)
- Only **exported functions** become stored procedures; they are renamed to `__FuncName` in generated code
- The `pl.go` root file uses **placeholder comments** (`//{exportedfuncs}`, `//{pgmodulemagic}`, `//{windowsCFLAGS}`) for code injection during generation — do not remove these
- Error handling in generated extensions uses `plgo.NewErrorLogger` / `plgo.NewNoticeLogger` which write to PostgreSQL's `elog()` — not stdout/stderr
- DB access within procedures uses `plgo.Open()` / `db.Prepare()` / `stmt.Query()` — a thin wrapper over PostgreSQL SPI, not `database/sql`
- **Goroutines are dangerous** in this context due to PostgreSQL stack depth limits; avoid them in procedures that touch the DB
- Windows support uses build tags (`//go:build windows` / `//go:build !windows`) for platform-specific path handling
- **Integration tests** use testcontainers-go (`integration/` directory): a `Dockerfile.test` builds extensions into a PG 18 image, `TestMain` manages the container lifecycle, and test functions run SQL assertions via `database/sql`. Gated behind `//go:build integration`.
