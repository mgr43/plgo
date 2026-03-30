<p align="center">
  <h1 align="center">plgo</h1>
  <p align="center">Write PostgreSQL extensions in Go. No C required.</p>
</p>

<p align="center">
  <a href="https://goreportcard.com/report/gitlab.com/microo8/plgo"><img src="https://goreportcard.com/badge/gitlab.com/microo8/plgo" alt="Go Report Card"></a>
  <a href="https://opensource.org/licenses/MIT"><img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="MIT License"></a>
  <img src="https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white" alt="Go 1.22+">
  <img src="https://img.shields.io/badge/PostgreSQL-14--18-336791?logo=postgresql&logoColor=white" alt="PostgreSQL 14–18">
</p>

---

**plgo** turns ordinary Go functions into PostgreSQL stored procedures and triggers — automatically. You write pure Go, and plgo generates all the C glue code, SQL definitions, and build files needed to install your functions as a native PostgreSQL extension.

```go
package main

import "gitlab.com/microo8/plgo"

// This becomes SELECT hello('world') → 'Hello, world!'
func Hello(name string) string {
    return "Hello, " + name + "!"
}
```

```
$ plgo .
$ cd build && sudo make install
```

```sql
CREATE EXTENSION mypackage;
SELECT hello('world');
--  hello
-- ─────────────────
--  Hello, world!
```

That's it. No CGo, no Makefiles, no SQL boilerplate. Just Go.

> 📖 **New here?** Read the **[full tutorial](docs/tutorial.md)** — a complete walkthrough
> that takes you from zero to a working extension with triggers, SPI queries,
> tests, and Docker deployment.

---

## ✨ Why plgo?

| | PL/pgSQL | PL/Python | C extension | **plgo** |
|---|---|---|---|---|
| Language | SQL dialect | Python | C | **Go** |
| Type safety | ❌ Runtime | ❌ Runtime | ✅ Compile-time | **✅ Compile-time** |
| Performance | Interpreted | Interpreted | Native | **Native (compiled to .so)** |
| Access to Go ecosystem | ❌ | ❌ | ❌ | **✅ Full `go get` ecosystem** |
| Boilerplate | Low | Low | 🔴 Enormous | **✅ Zero** |
| Database access (SPI) | Built-in | Via adapter | Raw C API | **✅ Clean Go API** |
| Trigger support | ✅ | ✅ | ✅ | **✅** |

**plgo gives you native C-extension performance with Go's safety and ecosystem.** Use any Go library — JSON processing, compression, HTTP clients, crypto — directly inside PostgreSQL.

---

## 📦 Quick Start

### Prerequisites

- **Go 1.22+**
- **PostgreSQL dev headers** — `sudo apt-get install postgresql-server-dev-18` (replace `18` with your version)
- **pg_config** in your PATH

### Install

```bash
go install gitlab.com/microo8/plgo/cmd/plgo@latest
```

### Write your functions

Create a directory for your extension (the directory name becomes the extension name):

```bash
mkdir myext && cd myext
go mod init myext
go get gitlab.com/microo8/plgo
```

Create `main.go`:

```go
package main

import (
    "log"
    "strings"

    "gitlab.com/microo8/plgo"
)

// Reverse returns the input string reversed.
func Reverse(s string) string {
    runes := []rune(s)
    for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
        runes[i], runes[j] = runes[j], runes[i]
    }
    return string(runes)
}

// ConcatAll concatenates every value of a column in a table.
// Demonstrates SPI database access from within a stored procedure.
func ConcatAll(tableName, colName string) string {
    logger := plgo.NewErrorLogger("", log.Ltime)
    db, err := plgo.Open()
    if err != nil {
        logger.Fatal("Cannot open DB:", err)
    }
    defer db.Close()

    stmt, err := db.Prepare("SELECT "+colName+" FROM "+tableName, nil)
    if err != nil {
        logger.Fatal("Prepare failed:", err)
    }
    rows, err := stmt.Query()
    if err != nil {
        logger.Fatal("Query failed:", err)
    }
    var parts []string
    for rows.Next() {
        var val string
        rows.Scan(&val)
        parts = append(parts, val)
    }
    return strings.Join(parts, "")
}
```

### Build & install

```bash
plgo .                              # generates build/ directory
cd build
sudo make install                   # installs into PostgreSQL
```

### Use it!

```sql
CREATE EXTENSION myext;

SELECT reverse('PostgreSQL');
-- → 'LQSergtsoP'

CREATE TABLE docs (content text);
INSERT INTO docs VALUES ('Hello, '), ('world'), ('!');
SELECT concatall('docs', 'content');
-- → 'Hello, world!'
```

---

## 📘 Supported Types

Every exported Go function automatically becomes a PostgreSQL stored procedure. plgo maps Go types to PostgreSQL types:

### Scalar Types

| Go type | PostgreSQL type |
|---|---|
| `string` | `text` |
| `[]byte` | `bytea` |
| `bool` | `boolean` |
| `int16` | `smallint` |
| `int32` | `integer` |
| `int64` / `int` | `bigint` |
| `float32` | `real` |
| `float64` | `double precision` |
| `time.Time` | `timestamp with timezone` |

### Array Types

| Go type | PostgreSQL type |
|---|---|
| `[]string` | `text[]` |
| `[]bool` | `boolean[]` |
| `[]int16` | `smallint[]` |
| `[]int32` | `integer[]` |
| `[]int64` / `[]int` | `bigint[]` |
| `[]float32` | `real[]` |
| `[]float64` | `double precision[]` |
| `[]time.Time` | `timestamp with timezone[]` |

### Nullable Returns

Return a pointer type (`*string`, `*int64`, etc.) to allow `NULL` returns:

```go
func MaybeUpper(s string) *string {
    if s == "" {
        return nil // returns SQL NULL
    }
    result := strings.ToUpper(s)
    return &result
}
```

### Set-Returning Functions (SETOF)

Return `plgo.SetOf[T]` to produce multiple rows — one value per row. These
functions can be used in `FROM` clauses just like tables:

```go
func GenerateSeries(start, stop int32) plgo.SetOf[int32] {
    result := make(plgo.SetOf[int32], 0, stop-start+1)
    for i := start; i <= stop; i++ {
        result = append(result, i)
    }
    return result
}
```

```sql
SELECT * FROM generateseries(1, 5);
--  generateseries
-- ────────────────
--              1
--              2
--              3
--              4
--              5
```

`SetOf` works with any supported scalar type: `SetOf[string]`, `SetOf[int64]`,
`SetOf[float64]`, `SetOf[bool]`, `SetOf[[]byte]`, etc.

### JSON Parameters

Use prepared statements with `jsonb` or `json` type hints to pass structured data:

```go
func ProcessConfig(name string) string {
    db, _ := plgo.Open()
    defer db.Close()

    var config struct {
        Theme string `json:"theme"`
        Lang  string `json:"lang"`
    }
    stmt, _ := db.Prepare("SELECT settings FROM configs WHERE name = $1", []string{"text"})
    row, _ := stmt.QueryRow(name)
    row.Scan(&config) // auto-unmarshals JSONB columns into Go structs
    return config.Theme
}
```

---

## 🗄️ Database Access (SPI)

plgo provides a clean Go API for PostgreSQL's Server Programming Interface (SPI). It works inside your stored procedures — no external connections needed.

```go
func CountUsers() int64 {
    logger := plgo.NewErrorLogger("", log.Ltime)
    db, err := plgo.Open()
    if err != nil {
        logger.Fatal(err)
    }
    defer db.Close()

    stmt, err := db.Prepare("SELECT count(*) FROM users", nil)
    if err != nil {
        logger.Fatal(err)
    }

    row, err := stmt.QueryRow()
    if err != nil {
        logger.Fatal(err)
    }

    var count int64
    row.Scan(&count)
    return count
}
```

### API Reference

| Method | Description |
|---|---|
| `plgo.Open()` | Opens an SPI connection (one per function call) |
| `db.Close()` | Closes the SPI connection (always `defer` this) |
| `db.Prepare(query, types)` | Prepares a parameterized query. `types` is `[]string` of PG type names for parameters (`nil` if none) |
| `stmt.Query(args...)` | Executes query, returns `*Rows` for iterating |
| `stmt.QueryRow(args...)` | Executes query, returns single `*Row` |
| `stmt.Exec(args...)` | Executes a non-SELECT statement (INSERT, UPDATE, etc.) |
| `rows.Next()` | Advances to next row, returns `false` when done |
| `rows.Scan(&val1, &val2)` | Reads column values into Go variables |
| `rows.Columns()` | Returns column names as `[]string` |
| `row.Scan(&val1, &val2)` | Reads column values from a single row |

### Parameterized Queries

```go
stmt, err := db.Prepare(
    "SELECT name, age FROM users WHERE active = $1 AND score > $2",
    []string{"boolean", "double precision"},
)
rows, err := stmt.Query(true, 95.0)
for rows.Next() {
    var name string
    var age int32
    rows.Scan(&name, &age)
}
```

---

## ⚡ Trigger Functions

Trigger functions let you modify rows on INSERT, UPDATE, or DELETE — written in Go:

```go
func CreatedTimeTrigger(td *plgo.TriggerData) *plgo.TriggerRow {
    // Read existing column values
    var id int
    var value string
    td.NewRow.Scan(&id, &value)

    // Modify columns before they're written
    td.NewRow.Set(0, id+10)
    td.NewRow.Set(1, value+value)

    return td.NewRow
}
```

**Rules for trigger functions:**
- First parameter must be `*plgo.TriggerData`
- Must return `*plgo.TriggerRow`
- plgo generates `RETURNS TRIGGER` SQL automatically

### Trigger introspection

`TriggerData` provides methods to check what fired the trigger:

| Method | Description |
|---|---|
| `td.FiredBefore()` | Trigger fired BEFORE the operation |
| `td.FiredAfter()` | Trigger fired AFTER the operation |
| `td.FiredInstead()` | Trigger fired INSTEAD OF the operation |
| `td.FiredForRow()` | Row-level trigger |
| `td.FiredForStatement()` | Statement-level trigger |
| `td.FiredByInsert()` | Triggered by INSERT |
| `td.FiredByUpdate()` | Triggered by UPDATE |
| `td.FiredByDelete()` | Triggered by DELETE |
| `td.FiredByTruncate()` | Triggered by TRUNCATE |

### Attaching the trigger

```sql
CREATE EXTENSION myext;

CREATE TRIGGER set_created_time
    BEFORE INSERT ON my_table
    FOR EACH ROW
    EXECUTE FUNCTION createdtimetrigger();
```

---

## 📝 Logging

PostgreSQL doesn't use stdout/stderr. plgo provides loggers that write to PostgreSQL's `elog` system:

```go
// NOTICE-level: visible in psql, non-fatal
logger := plgo.NewNoticeLogger("prefix", log.Ltime|log.Lshortfile)
logger.Println("Processing complete")

// ERROR-level: aborts the current transaction
logger := plgo.NewErrorLogger("prefix", log.Ltime|log.Lshortfile)
logger.Fatal("Something went wrong:", err) // rolls back transaction
```

These are standard `*log.Logger` instances — use `Println`, `Printf`, `Fatal`, `Fatalf`, etc.

---

## 🔧 How It Works

plgo is a **code generator**. When you run `plgo .`, it:

1. **Parses** your Go package with `go/ast` — finds every exported function
2. **Classifies** each function as a regular function, void function, trigger function, or set-returning function based on its signature
3. **Generates** CGo wrapper code that marshals PostgreSQL Datum types ↔ Go types
4. **Injects** `PG_FUNCTION_INFO_V1` macros and `//export` directives
5. **Builds** with `go build -buildmode=c-shared` → produces a native `.so` shared library
6. **Emits** extension files: `CREATE FUNCTION` SQL, `.control` file, and `Makefile` (using PGXS)

The result is a standard PostgreSQL extension that installs with `make install` — just like extensions written in C.

```
your_code.go                      build/
├── func Hello(s string) string    ├── myext.so          ← compiled shared library
├── func Count() int64      ──→    ├── myext--0.1.sql    ← CREATE FUNCTION statements
└── func OnInsert(td)              ├── myext.control     ← extension metadata
                                   └── Makefile          ← PGXS install script
```

---

## 🧪 Development

### Run unit tests (no database needed)

```bash
make test-unit
```

160 test cases covering AST parsing, code generation, SQL output, type mapping, and SETOF support — all without PostgreSQL.

### Run integration tests (requires Docker)

```bash
make test-integration
```

Builds the extension inside a Docker container with PostgreSQL 18, installs it, and runs SQL tests with expected output verification.

### Project structure

```
plgo/
├── pl.go                   # Runtime: CGo bridge (Datum↔Go, SPI, triggers, elog)
├── cmd/plgo/               # CLI code generator
│   ├── plgo.go             #   Entry point
│   ├── modulewriter.go     #   Orchestrates code generation
│   ├── functions.go        #   CodeWriter types, datumTypes map, SQL/code generation
│   ├── visitors.go         #   AST visitors (FuncVisitor, Remover)
│   └── plgo_test.go        #   160 unit tests
├── example/                # Example extension
│   └── example_methods.go
├── test/                   # Integration test suite
│   ├── plgotest.go         #   SPI test functions (run inside PostgreSQL)
│   ├── sql/                #   SQL test scripts
│   └── expected/           #   Expected output (diff-based verification)
├── Dockerfile              # Multi-stage build: Go 1.26 + PG 18
├── Makefile                # build, install, test-unit, test-integration, fmt, clean
└── go.mod
```

### Makefile targets

| Target | Description |
|---|---|
| `make build` | Build the `plgo` CLI tool |
| `make install` | Install `plgo` into `$GOPATH/bin` |
| `make test` | Run unit + integration tests |
| `make test-unit` | Run Go unit tests (fast, no DB) |
| `make test-integration` | Build Docker image, run SQL tests against PG 18 |
| `make fmt` | Format Go (gofumpt) and SQL (pg_format) files |
| `make clean` | Remove build artifacts and Docker images |

---

## ⚠️ Limitations

- **Single return value only** — Go functions can return at most one value (no `(result, error)` pattern; use `logger.Fatal` for errors)
- **No goroutines that touch the DB** — PostgreSQL's stack depth limit conflicts with Go's goroutine stack allocation. You can use goroutines for pure computation, but don't call SPI from a goroutine
- **No custom composite types** — Only built-in scalar and array types are supported; no `CREATE TYPE` mapping (yet)
- **No `RETURNS TABLE`** — `SETOF` scalar types are supported, but multi-column `RETURNS TABLE(col1 type, col2 type)` is not (yet)
- **Package must be `main`** — Your extension code must be in a `package main`

---

## 🤝 Contributing

Contributions of all kinds are welcome! Whether it's bug fixes, new type mappings, documentation improvements, or feature ideas — open an issue or submit a merge request.

```bash
# Clone and set up
git clone https://gitlab.com/microo8/plgo.git
cd plgo

# Run unit tests
make test-unit

# Run full integration tests (requires Docker)
make test-integration

# Format code
make fmt
```

---

## 📄 License

[MIT](LICENSE) © Vladimír Magyar
