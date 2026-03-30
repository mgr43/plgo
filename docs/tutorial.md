# 🐘 The plgo Tutorial

### Or: How I Learned to Stop Worrying and Write PostgreSQL Extensions in Go

---

So you want to run Go code _inside_ PostgreSQL. Like, literally inside the
database server process. Not "call an HTTP endpoint from a trigger." Not "use
some ORM." We're talking `SELECT my_go_function('hello')` and having Go execute
at C-extension speed, right there in the query pipeline.

You're in the right place. Buckle up.

---

## Chapter 0: What Are We Even Doing?

PostgreSQL lets you write extensions in C. That gives you native speed and full
access to the database internals. The catch? **You have to write C.** Memory
management, `Datum` types, `PG_FUNCTION_INFO_V1` macros, `SPI_connect`,
`HeapTuple`, `TupleDesc`... it's a whole thing.

**plgo** lets you skip all of that. You write normal Go functions, and plgo
generates all the C glue code, builds a shared library, and emits the SQL and
Makefile you need to install it as a real PostgreSQL extension.

Your Go function:

```go
func Shout(msg string) string {
    return strings.ToUpper(msg) + "!!!"
}
```

What PostgreSQL sees:

```sql
SELECT shout('hello');
--  shout
-- ──────────
--  HELLO!!!
```

Let's build something real.

---

## Chapter 1: Setting Up Shop

### What you need

| Thing                      | Why                                                  |
| -------------------------- | ---------------------------------------------------- |
| **Go 1.22+**               | We build with `cgo` and `-buildmode=c-shared`        |
| **PostgreSQL dev headers** | The C headers plgo's generated code compiles against |
| **`pg_config` in PATH**    | plgo asks it where PostgreSQL lives                  |
| **Docker** _(optional)_    | For running tests without polluting your system      |

Install the prerequisites:

```bash
# Ubuntu/Debian
sudo apt-get install postgresql-server-dev-18   # match your PG version!

# macOS (Homebrew)
brew install postgresql@18

# Verify pg_config works
pg_config --version
# PostgreSQL 18.x
```

### Install plgo

```bash
go install gitlab.com/microo8/plgo/cmd/plgo@latest
```

That's it. You now have a `plgo` binary in your `$GOPATH/bin`.

```bash
plgo
# Usage: plgo [-v] [path/to/package]
```

Let's use it.

---

## Chapter 2: Your First Extension — "pgpet"

We're going to build a PostgreSQL extension called **pgpet** — a virtual pet
that lives in your database. Because why not.

### Create the project

```bash
mkdir pgpet && cd pgpet
go mod init pgpet
go get gitlab.com/microo8/plgo
```

> 💡 **The directory name becomes the extension name.** So `pgpet/` →
> `CREATE EXTENSION pgpet`. Choose wisely (or at least amusingly).

### The rules

Before we write code, here are the rules plgo enforces:

1. **Must be `package main`** — PostgreSQL extensions are shared libraries,
   and Go builds those from `main` packages.

2. **Every exported function becomes a stored procedure** — `func Shout()`
   becomes `SELECT shout()`. Unexported functions (`func helper()`) are
   ignored and stay private.

3. **Supported parameter/return types** — Go builtins only: `string`, `int16`,
   `int32`, `int64`, `float32`, `float64`, `bool`, `[]byte`, and their array
   variants (`[]string`, `[]int64`, etc.). Plus `*plgo.TriggerData` /
   `*plgo.TriggerRow` for triggers.

4. **Single return value** — No `(result, error)`. If something goes wrong,
   use `logger.Fatal()` to report an error to PostgreSQL (which will abort the
   transaction).

5. **Go doc comments become SQL COMMENTs** — Write a doc comment on your
   function and it shows up in `\df+` in psql. Free documentation!

Got it? Let's code.

---

### Create `main.go`

```go
package main

import (
	"fmt"
	"log"
	"math/rand"
	"strings"

	"gitlab.com/microo8/plgo"
)

// PetGreet returns a greeting from your virtual pet.
// The pet is very enthusiastic.
func PetGreet(petName string) string {
	greetings := []string{
		fmt.Sprintf("🐾 %s wags tail furiously!", petName),
		fmt.Sprintf("🐱 %s purrs and headbutts your leg!", petName),
		fmt.Sprintf("🦜 %s squawks: HELLO HELLO HELLO!", petName),
		fmt.Sprintf("🐢 %s slowly blinks at you. This means love.", petName),
	}
	return greetings[rand.Intn(len(greetings))]
}

// PetFeed feeds your pet and returns a status message.
// Demonstrates multiple parameters.
func PetFeed(petName string, treats int32) string {
	if treats <= 0 {
		return fmt.Sprintf("😾 %s stares at your empty hands in betrayal.", petName)
	}
	if treats > 10 {
		return fmt.Sprintf("🤢 %s ate %d treats and is now regretting everything.", petName, treats)
	}
	return fmt.Sprintf("😋 %s happily devours %d treat(s)!", petName, treats)
}

// PetRename renames a pet in the database.
// Demonstrates SPI (database access from inside a stored procedure).
func PetRename(oldName, newName string) string {
	logger := plgo.NewErrorLogger("", log.Ltime)
	db, err := plgo.Open()
	if err != nil {
		logger.Fatal("Cannot open DB:", err)
	}
	defer db.Close()

	stmt, err := db.Prepare(
		"UPDATE pets SET name = $1 WHERE name = $2",
		[]string{"text", "text"},
	)
	if err != nil {
		logger.Fatal("Prepare failed:", err)
	}
	err = stmt.Exec(newName, oldName)
	if err != nil {
		logger.Fatal("Exec failed:", err)
	}
	return fmt.Sprintf("✅ %s is now known as %s!", oldName, newName)
}

// PetStats returns all pet names as a comma-separated list.
// Demonstrates SPI queries with row iteration.
func PetStats() string {
	logger := plgo.NewErrorLogger("", log.Ltime)
	db, err := plgo.Open()
	if err != nil {
		logger.Fatal("Cannot open DB:", err)
	}
	defer db.Close()

	stmt, err := db.Prepare("SELECT name FROM pets ORDER BY name", nil)
	if err != nil {
		logger.Fatal("Prepare failed:", err)
	}
	rows, err := stmt.Query()
	if err != nil {
		logger.Fatal("Query failed:", err)
	}

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			logger.Fatal("Scan failed:", err)
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return "🏚️ No pets yet. Adopt one!"
	}
	return fmt.Sprintf("🐾 Your pets: %s", strings.Join(names, ", "))
}

// PetScoreMultiply multiplies an array of scores by a factor.
// Demonstrates array parameters and array return types.
func PetScoreMultiply(scores []int64, factor int64) []int64 {
	result := make([]int64, len(scores))
	for i, s := range scores {
		result[i] = s * factor
	}
	return result
}

// PetMood returns the pet's mood, or NULL if the pet doesn't exist.
// Demonstrates nullable returns with pointer types.
func PetMood(petName string) *string {
	moods := map[string]string{
		"whiskers": "😸 Smug",
		"rex":      "🐕 Excited",
		"polly":    "🦜 Chaotic",
	}
	mood, ok := moods[strings.ToLower(petName)]
	if !ok {
		return nil // returns SQL NULL
	}
	return &mood
}

// PetAutoNamer is a trigger function.
// It fires BEFORE INSERT on the pets table and uppercases the pet name,
// because PETS DESERVE CAPITAL LETTERS.
func PetAutoNamer(td *plgo.TriggerData) *plgo.TriggerRow {
	var id int32
	var name string
	td.NewRow.Scan(&id, &name)

	// EVERY PET NAME SHALL BE UPPERCASE
	td.NewRow.Set(1, strings.ToUpper(name))

	return td.NewRow
}
```

Let's count what we have:

| Function           | Type                 | PG Signature                                    |
| ------------------ | -------------------- | ----------------------------------------------- |
| `PetGreet`         | scalar → scalar      | `petgreet(text) → text`                         |
| `PetFeed`          | multi-param → scalar | `petfeed(text, integer) → text`                 |
| `PetRename`        | SPI + exec           | `petrename(text, text) → text`                  |
| `PetStats`         | SPI + query          | `petstats() → text`                             |
| `PetScoreMultiply` | arrays               | `petscoremultiply(bigint[], bigint) → bigint[]` |
| `PetMood`          | nullable return      | `petmood(text) → text` (can return NULL)        |
| `PetAutoNamer`     | trigger              | `petautonamer() → trigger`                      |

Seven functions. Zero lines of C. Let's build it.

---

## Chapter 3: Building the Extension

```bash
plgo .
```

That's literally it. plgo will:

1. Parse your Go package
2. Find all 7 exported functions
3. Generate CGo wrappers, SQL, Makefile, and `.control` file
4. Compile everything into a shared library

You'll see output like:

```
2026/03/30 12:00:00 /tmp/plgo-build-123456
```

And now you have a `build/` directory:

```
build/
├── pgpet.so              ← the compiled shared library (your Go code!)
├── pgpet--0.1.sql         ← CREATE FUNCTION statements for all 7 functions
├── pgpet.control          ← extension metadata
└── Makefile               ← PGXS-based install script
```

### What's in that SQL file?

Let's peek:

```sql
-- complain if script is sourced in psql, rather than via CREATE EXTENSION
\echo Use "CREATE EXTENSION pgpet" to load this file. \quit

CREATE OR REPLACE FUNCTION PetGreet(petName text)
RETURNS text AS
'$libdir/pgpet', 'PetGreet'
LANGUAGE c VOLATILE STRICT;
COMMENT ON FUNCTION PetGreet(text) IS 'PetGreet returns a greeting from your virtual pet.
The pet is very enthusiastic.
';

CREATE OR REPLACE FUNCTION PetFeed(petName text, treats integer)
RETURNS text AS
'$libdir/pgpet', 'PetFeed'
LANGUAGE c VOLATILE STRICT;
-- ... and so on for all functions
```

Notice: your Go doc comments became `COMMENT ON FUNCTION` statements. They'll
show up when someone runs `\df+ petgreet` in psql. 🎉

---

## Chapter 4: Installing on a Local PostgreSQL Server

### Install

```bash
cd build
sudo make install
```

This copies `pgpet.so` to PostgreSQL's `$libdir` and the SQL/control files
where PostgreSQL expects them.

### Load the extension

```bash
psql -U postgres
```

```sql
CREATE EXTENSION pgpet;
```

### Play with it!

```sql
-- Greet your pet
SELECT petgreet('Whiskers');
--                petgreet
-- ─────────────────────────────────────────
--  🐱 Whiskers purrs and headbutts your leg!

-- Feed your pet
SELECT petfeed('Rex', 3);
--              petfeed
-- ──────────────────────────────────
--  😋 Rex happily devours 3 treat(s)!

-- Overfeed your pet (don't actually do this)
SELECT petfeed('Rex', 50);
--                    petfeed
-- ──────────────────────────────────────────────────
--  🤢 Rex ate 50 treats and is now regretting everything.

-- Forget to feed your pet
SELECT petfeed('Rex', 0);
--                    petfeed
-- ──────────────────────────────────────────────────
--  😾 Rex stares at your empty hands in betrayal.

-- Check mood (returns NULL for unknown pets)
SELECT petmood('Whiskers');  -- → '😸 Smug'
SELECT petmood('Unknown');   -- → NULL

-- Array operations
SELECT petscoremultiply(ARRAY[10, 20, 30], 5);
--  petscoremultiply
-- ──────────────────
--  {50,100,150}
```

### Try the trigger

```sql
-- Create a pets table
CREATE TABLE pets (id serial, name text);

-- Attach the auto-namer trigger
CREATE TRIGGER auto_name
    BEFORE INSERT ON pets
    FOR EACH ROW
    EXECUTE FUNCTION petautonamer();

-- Insert a pet with a lowercase name
INSERT INTO pets (name) VALUES ('whiskers');

-- The trigger uppercased it!
SELECT * FROM pets;
--  id |   name
-- ────+──────────
--   1 | WHISKERS
```

### Try the SPI functions

```sql
-- Insert some more pets
INSERT INTO pets (name) VALUES ('rex'), ('polly');

-- List all pets (uses SPI internally)
SELECT petstats();
--            petstats
-- ──────────────────────────────────
--  🐾 Your pets: POLLY, REX, WHISKERS

-- Rename a pet (uses SPI UPDATE)
SELECT petrename('REX', 'T-REX');
--          petrename
-- ────────────────────────────
--  ✅ REX is now known as T-REX!

SELECT petstats();
--             petstats
-- ──────────────────────────────────────
--  🐾 Your pets: POLLY, T-REX, WHISKERS
```

---

## Chapter 5: Testing Like a Professional

"But it works on my machine!" won't cut it when your extension runs inside
a database server. Let's write proper tests.

### The testing strategy

plgo's test approach uses **SQL scripts with expected output**. It's the same
pattern PostgreSQL itself uses for regression testing:

1. Write a `.sql` file with queries
2. Write an `.out` file with the exact expected output
3. Run the SQL, capture output, `diff` against expected

Simple, effective, impossible to argue with.

### Create the test files

```bash
mkdir -p test/sql test/expected
```

#### `test/sql/01_setup.sql`

```sql
-- Load the extension
CREATE EXTENSION pgpet;

-- Create the pets table
CREATE TABLE pets (id serial, name text);

-- Attach the trigger
CREATE TRIGGER auto_name
    BEFORE INSERT ON pets
    FOR EACH ROW
    EXECUTE FUNCTION petautonamer();
```

#### `test/sql/02_basic_functions.sql`

```sql
-- Test basic functions
SELECT petfeed('Whiskers', 3);
SELECT petfeed('Whiskers', 0);
SELECT petfeed('Whiskers', 99);

-- Test nullable returns
SELECT petmood('whiskers');
SELECT petmood('nope') IS NULL AS mood_is_null;

-- Test arrays
SELECT petscoremultiply(ARRAY[1, 2, 3]::bigint[], 10::bigint);
```

#### `test/sql/03_trigger_and_spi.sql`

```sql
-- Test trigger (auto-uppercases names)
INSERT INTO pets (name) VALUES ('fluffy');
SELECT name FROM pets WHERE name = 'FLUFFY';

-- Insert more pets for SPI tests
INSERT INTO pets (name) VALUES ('buddy'), ('coco');

-- Test SPI query
SELECT petstats();

-- Test SPI update
SELECT petrename('BUDDY', 'SUPER-BUDDY');
SELECT name FROM pets WHERE name = 'SUPER-BUDDY';

-- Cleanup
DROP TABLE pets CASCADE;
DROP EXTENSION pgpet;
```

### Generate expected output

The first time, you run the SQL manually and capture the output to create the
expected files. After that, the tests verify nothing changed.

> 💡 **Pro tip:** You don't have to write expected files by hand! Run the SQL
> once, review the output, and save it as the expected file. Then future runs
> will `diff` against it.

---

## Chapter 6: Running Tests in Docker

The cleanest way to test is in a Docker container. No messing with your local
PostgreSQL, no permission issues, perfectly reproducible.

### Create `Dockerfile.test`

```dockerfile
FROM golang:1.26-bookworm AS builder

# Install PostgreSQL dev headers
RUN apt-get update && apt-get install -y curl ca-certificates gnupg lsb-release && \
    curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc | gpg --dearmor -o /usr/share/keyrings/pgdg.gpg && \
    echo "deb [signed-by=/usr/share/keyrings/pgdg.gpg] http://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" > /etc/apt/sources.list.d/pgdg.list && \
    apt-get update && \
    apt-get install -y postgresql-server-dev-18 && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Install plgo
RUN go install gitlab.com/microo8/plgo/cmd/plgo@latest

# Copy your extension source
COPY go.mod go.sum ./
RUN go mod download
COPY main.go .

# Build the extension
RUN plgo .

# --- Runtime: PostgreSQL with your extension installed ---
FROM postgres:18

RUN apt-get update && \
    apt-get install -y make postgresql-server-dev-18 && \
    rm -rf /var/lib/apt/lists/*

# Install the extension
COPY --from=builder /src/build/ /tmp/pgpet-ext/
RUN cd /tmp/pgpet-ext && make install && rm -rf /tmp/pgpet-ext

# Copy test files
COPY test/sql/ /test/sql/
COPY test/expected/ /test/expected/
COPY test/run-tests.sh /test/run-tests.sh
RUN chmod +x /test/run-tests.sh

CMD ["/test/run-tests.sh"]
```

### Create `test/run-tests.sh`

```bash
#!/bin/bash
set -e

export PATH="/usr/lib/postgresql/18/bin:$PATH"
export PGDATA=/var/lib/postgresql/data

# Start PostgreSQL
mkdir -p "$PGDATA"
chown postgres:postgres "$PGDATA"
gosu postgres initdb -D "$PGDATA" > /dev/null 2>&1
gosu postgres pg_ctl -D "$PGDATA" -l /tmp/pg.log start > /dev/null 2>&1
sleep 2

echo "🧪 Running pgpet tests..."
echo ""

PASS=0
FAIL=0
ERRORS=""

for sqlfile in /test/sql/*.sql; do
    name=$(basename "$sqlfile" .sql)
    expected="/test/expected/${name}.out"
    actual="/tmp/${name}.actual"

    # Run SQL, capture output
    gosu postgres psql -X -a -f "$sqlfile" postgres 2>&1 > "$actual" || true

    if [ ! -f "$expected" ]; then
        echo "⏭️  SKIP $name (no expected output file)"
        continue
    fi

    if diff -u "$expected" "$actual" > "/tmp/${name}.diff" 2>&1; then
        echo "✅ PASS $name"
        PASS=$((PASS + 1))
    else
        echo "❌ FAIL $name"
        cat "/tmp/${name}.diff"
        FAIL=$((FAIL + 1))
        ERRORS="$ERRORS $name"
    fi
done

echo ""
echo "════════════════════════════════"
echo "Results: $PASS passed, $FAIL failed"
if [ $FAIL -gt 0 ]; then
    echo "Failed:$ERRORS"
    exit 1
fi
echo "🎉 All tests passed!"
```

### Run it

```bash
# Build the test image
docker build -f Dockerfile.test -t pgpet-test .

# Run the tests
docker run --rm pgpet-test
```

Expected output:

```
🧪 Running pgpet tests...

✅ PASS 01_setup
✅ PASS 02_basic_functions
✅ PASS 03_trigger_and_spi

════════════════════════════════
Results: 3 passed, 0 failed
🎉 All tests passed!
```

---

## Chapter 7: Testing on a Local PostgreSQL Server

Don't want Docker? You can test directly on your local PG.

### Build and install

```bash
# Build the extension
plgo .

# Install into your local PostgreSQL
cd build
sudo make install
cd ..
```

### Run the test SQL manually

```bash
# Run all test files in order
for f in test/sql/*.sql; do
    echo "=== Running $f ==="
    psql -U postgres -a -f "$f" postgres
done
```

### Or run individual tests

```bash
# Just test the basic functions
psql -U postgres -f test/sql/02_basic_functions.sql postgres
```

### Compare against expected output

```bash
# Capture and diff
psql -U postgres -X -a -f test/sql/02_basic_functions.sql postgres > /tmp/actual.out 2>&1
diff -u test/expected/02_basic_functions.out /tmp/actual.out
```

If `diff` shows nothing, you're golden. 🏆

---

## Chapter 8: What plgo Actually Generated

Let's pop the hood. When you ran `plgo .`, it created a temp directory with
three generated files, compiled them, and put the results in `build/`.

### The generated wrapper (`methods.go`)

For each of your exported functions, plgo generates an `//export` CGo wrapper:

```go
// This is what plgo generated for PetFeed.
// YOU don't write this. plgo does.

//export PetFeed
func PetFeed(fcinfo *funcInfo) Datum {
    var petName string
    var treats int32
    err := fcinfo.Scan(
        &petName,
        &treats,
    )
    if err != nil {
        C.elog_error(C.CString(err.Error()))
    }
    ret := __PetFeed(petName, treats)
    return toDatum(ret)
}
```

What's happening:

1. PostgreSQL calls `PetFeed` with raw C `Datum` arguments
2. `fcinfo.Scan()` converts them to Go types (`string`, `int32`)
3. Your original function (renamed to `__PetFeed`) runs with real Go types
4. `toDatum()` converts the Go return value back to a PostgreSQL `Datum`

For the trigger function, it's slightly different:

```go
//export PetAutoNamer
func PetAutoNamer(fcinfo *funcInfo) Datum {
    ret := __PetAutoNamer(
        fcinfo.TriggerData(),
    )
    return toDatum(ret)
}
```

The trigger gets `TriggerData` from `fcinfo` instead of scanning arguments.

### The `PG_FUNCTION_INFO_V1` macros

In the generated `pl.go`, plgo injects one macro per function:

```c
PG_FUNCTION_INFO_V1(PetGreet);
PG_FUNCTION_INFO_V1(PetFeed);
PG_FUNCTION_INFO_V1(PetRename);
PG_FUNCTION_INFO_V1(PetStats);
PG_FUNCTION_INFO_V1(PetScoreMultiply);
PG_FUNCTION_INFO_V1(PetMood);
PG_FUNCTION_INFO_V1(PetAutoNamer);
```

These tell PostgreSQL "yes, these are real Version 1 calling convention
functions, not some random symbols in the `.so`."

### The `build/` directory

```
build/
├── pgpet.so           ← ~15MB shared library (includes Go runtime)
├── pgpet--0.1.sql     ← CREATE FUNCTION for all 7 functions
├── pgpet.control      ← Extension metadata (name, version)
└── Makefile            ← PGXS install rules
```

The `.so` is large because it includes the entire Go runtime. That's the
tradeoff: you get Go's garbage collector, goroutines, and the entire standard
library available inside PostgreSQL. The file is big, but it only loads once
when `CREATE EXTENSION` runs.

---

## Chapter 9: The Type Cheat Sheet

When you're writing functions, you need to know what Go types map to what
PostgreSQL types. Here's the complete list:

### Scalar types

| Go type   | PostgreSQL type    | Example                       |
| --------- | ------------------ | ----------------------------- |
| `string`  | `text`             | `func Fn(s string) string`    |
| `[]byte`  | `bytea`            | `func Fn(data []byte) []byte` |
| `bool`    | `boolean`          | `func Fn(flag bool) bool`     |
| `int16`   | `smallint`         | `func Fn(n int16) int16`      |
| `int32`   | `integer`          | `func Fn(n int32) int32`      |
| `int64`   | `bigint`           | `func Fn(n int64) int64`      |
| `int`     | `bigint`           | `func Fn(n int) int`          |
| `float32` | `real`             | `func Fn(f float32) float32`  |
| `float64` | `double precision` | `func Fn(f float64) float64`  |

### Array types

| Go type     | PostgreSQL type      | Example                            |
| ----------- | -------------------- | ---------------------------------- |
| `[]string`  | `text[]`             | `func Fn(arr []string) []string`   |
| `[]int32`   | `integer[]`          | `func Fn(arr []int32) []int32`     |
| `[]int64`   | `bigint[]`           | `func Fn(arr []int64) []int64`     |
| `[]float64` | `double precision[]` | `func Fn(arr []float64) []float64` |
| `[]bool`    | `boolean[]`          | `func Fn(arr []bool) []bool`       |

### Special types

| Go type             | PostgreSQL type     | Notes                                  |
| ------------------- | ------------------- | -------------------------------------- |
| _(no return)_       | `VOID`              | Function returns nothing               |
| `*string`           | `text` (nullable)   | Return `nil` → SQL `NULL`              |
| `*int64`            | `bigint` (nullable) | Any pointer type → nullable            |
| `plgo.SetOf[T]`     | `SETOF <type>`      | Returns multiple rows, one `T` per row |
| `*plgo.TriggerData` | _(first param)_     | Trigger functions only                 |
| `*plgo.TriggerRow`  | `trigger`           | Trigger return type                    |

### Using SPI with JSON

PostgreSQL JSON columns auto-unmarshal into Go structs:

```go
func GetUserTheme(userID int64) string {
    db, _ := plgo.Open()
    defer db.Close()

    var prefs struct {
        Theme string `json:"theme"`
    }
    stmt, _ := db.Prepare(
        "SELECT preferences FROM users WHERE id = $1",
        []string{"bigint"},
    )
    row, _ := stmt.QueryRow(userID)
    row.Scan(&prefs)  // auto-unmarshals JSONB!
    return prefs.Theme
}
```

---

## Chapter 10: Common Patterns and Gotchas

### ✅ Error handling

Go's `(result, error)` pattern doesn't work here (only single return values).
Use `plgo.NewErrorLogger` + `Fatal`:

```go
func SafeFunction(input string) string {
    logger := plgo.NewErrorLogger("", log.Ltime)

    if input == "" {
        logger.Fatal("input cannot be empty")
        // This aborts the current PostgreSQL transaction.
        // The user sees: ERROR: input cannot be empty
    }

    return "processed: " + input
}
```

### ✅ Notice messages (non-fatal)

```go
func VerboseFunction(input string) string {
    notice := plgo.NewNoticeLogger("", log.Ltime)
    notice.Println("Starting processing...")  // visible in psql, doesn't abort
    result := strings.ToUpper(input)
    notice.Println("Done!")
    return result
}
```

```sql
SELECT verbosefunction('hello');
-- NOTICE:  12:00:00 Starting processing...
-- NOTICE:  12:00:00 Done!
--  verbosefunction
-- ──────────────────
--  HELLO
```

### ✅ SPI parameterized queries

Always use `$1`, `$2`, ... placeholders. Never concatenate user input.

```go
// GOOD
stmt, _ := db.Prepare(
    "SELECT name FROM users WHERE id = $1",
    []string{"integer"},
)
row, _ := stmt.QueryRow(42)

// BAD — SQL injection!
// stmt, _ := db.Prepare("SELECT name FROM users WHERE id = " + id, nil)
```

### ⚠️ Goroutines

Goroutines _technically_ work, but PostgreSQL has a stack depth limit that
conflicts with Go's goroutine stack allocation. The safe rule:

- ✅ Use goroutines for **pure computation** (no DB access)
- ❌ Never call SPI (`db.Prepare`, `stmt.Query`, etc.) from a goroutine

```go
// OK: goroutines for CPU work
func ParallelHash(inputs []string) []string {
    results := make([]string, len(inputs))
    var wg sync.WaitGroup
    for i, input := range inputs {
        wg.Add(1)
        go func(i int, s string) {
            defer wg.Done()
            h := sha256.Sum256([]byte(s))
            results[i] = hex.EncodeToString(h[:])
        }(i, input)
    }
    wg.Wait()
    return results
}
```

### ⚠️ Using Go libraries

You can use _any_ Go package. plgo builds your code with `go build`, so
everything in your `go.mod` is available:

```go
import (
    "encoding/json"          // ✅ standard library
    "compress/gzip"          // ✅
    "crypto/sha256"          // ✅
    "github.com/google/uuid" // ✅ third-party packages work too!
)
```

Just remember: the `.so` will include the entire dependency tree, so keep it
reasonable.

---

## Chapter 11: Real-World Example — Gzip in the Database

Here's a practical example: a function that compresses and decompresses data
using gzip. Useful for storing compressed blobs.

```go
package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"log"

	"gitlab.com/microo8/plgo"
)

// GzipCompress compresses bytea data using gzip.
func GzipCompress(data []byte) []byte {
	logger := plgo.NewErrorLogger("", log.Ltime)
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		logger.Fatal("compress write:", err)
	}
	if err := w.Close(); err != nil {
		logger.Fatal("compress close:", err)
	}
	return buf.Bytes()
}

// GzipDecompress decompresses gzip bytea data.
func GzipDecompress(data []byte) []byte {
	logger := plgo.NewErrorLogger("", log.Ltime)
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		logger.Fatal("decompress open:", err)
	}
	defer r.Close()
	result, err := io.ReadAll(r)
	if err != nil {
		logger.Fatal("decompress read:", err)
	}
	return result
}
```

Usage:

```sql
-- Compress
SELECT length(gzipcompress('hello world hello world hello world'::bytea));
-- 35 bytes of input → ~31 bytes compressed

-- Round-trip
SELECT convert_from(
    gzipdecompress(gzipcompress('hello world'::bytea)),
    'UTF8'
);
-- → 'hello world'
```

You just added gzip support to PostgreSQL. In 30 lines of Go. Try doing that
in PL/pgSQL.

---

## Chapter 13: Set-Returning Functions (SETOF)

So far, every function we've written returns a **single value**. But what if you
want to return **multiple rows** — like a table-valued function you can use in
`FROM` clauses?

That's what `SETOF` is for. And plgo makes it dead simple with `plgo.SetOf[T]`.

### The basics

```go
package main

import "gitlab.com/microo8/plgo"

// GenerateSeries returns integers from start to stop, one per row.
func GenerateSeries(start, stop int32) plgo.SetOf[int32] {
    result := make(plgo.SetOf[int32], 0, stop-start+1)
    for i := start; i <= stop; i++ {
        result = append(result, i)
    }
    return result
}
```

That's it. `plgo.SetOf[int32]` is just `[]int32` under the hood, but the
distinct type tells plgo to generate `RETURNS SETOF integer` instead of
`RETURNS integer[]`.

The difference matters:

| Return type         | SQL                     | Result                                         |
| ------------------- | ----------------------- | ---------------------------------------------- |
| `[]int32`           | `RETURNS integer[]`     | **One row** containing an array: `{1,2,3,4,5}` |
| `plgo.SetOf[int32]` | `RETURNS SETOF integer` | **Five rows**, each with one integer           |

### Using it in SQL

```sql
-- Like a table!
SELECT * FROM generateseries(1, 5);
--  generateseries
-- ────────────────
--              1
--              2
--              3
--              4
--              5

-- In a JOIN
SELECT u.name, s.n
FROM users u
JOIN generateseries(1, 3) AS s(n) ON true;

-- In a WHERE EXISTS
SELECT name FROM products
WHERE EXISTS (SELECT 1 FROM generateseries(1, 10) s(n) WHERE s.n = products.id);
```

### Supported types

`SetOf` works with every scalar type plgo supports:

```go
func TextRows() plgo.SetOf[string]   { ... }  // SETOF text
func IntRows()  plgo.SetOf[int64]    { ... }  // SETOF bigint
func NumRows()  plgo.SetOf[float64]  { ... }  // SETOF double precision
func FlagRows() plgo.SetOf[bool]     { ... }  // SETOF boolean
func BlobRows() plgo.SetOf[[]byte]   { ... }  // SETOF bytea
```

### Empty results

Returning an empty slice produces zero rows — perfectly valid:

```go
func MaybeResults(want bool) plgo.SetOf[string] {
    if !want {
        return nil // zero rows
    }
    return plgo.SetOf[string]{"yes", "we", "have", "results"}
}
```

```sql
SELECT * FROM mayberesults(false);
-- (0 rows)

SELECT * FROM mayberesults(true);
--  mayberesults
-- ──────────────
--  yes
--  we
--  have
--  results
```

### How it works under the hood

PostgreSQL's SRF protocol is... involved. The C function gets called **once per
output row**, not once total. It uses `FuncCallContext` to persist state across
calls, with macros like `SRF_IS_FIRSTCALL()`, `SRF_RETURN_NEXT()`, and
`SRF_RETURN_DONE()`.

plgo handles all of this for you. When you return `plgo.SetOf[int32]`, the
generated wrapper:

1. **First call**: runs your Go function, materializes the entire slice, converts
   each element to a PostgreSQL `Datum`, stores them in PG's multi-call memory
   context
2. **Each subsequent call**: returns the next `Datum` via `SRF_RETURN_NEXT`
3. **Final call**: signals `SRF_RETURN_DONE`

You just return a slice. plgo does the rest. 🎉

---

## Chapter 12: Deploying to Production

### Build on a matching system

The `.so` must be compiled on a system with the same:

- OS and architecture as the target server
- PostgreSQL major version (the C ABI can change between majors)

The safest approach: build in Docker using the same base image as your server.

### Option A: Self-contained installer script (recommended)

The easiest way to deploy. Build with `--installer`:

```bash
plgo --installer .
```

This produces `install-pgpet.sh` — a single file that bundles the compiled
`.so`, SQL definitions, and control file. Copy it to the server and run:

```bash
scp install-pgpet.sh prod-server:
ssh prod-server

# Install files + create the extension in one shot:
sudo ./install-pgpet.sh --create-extension --db myapp

# Or install files only (CREATE EXTENSION yourself later):
sudo ./install-pgpet.sh
```

No Go, no make, no build tools needed on the server. The script auto-detects
`pg_config` and installs everything to the right directories.

**Useful flags:**

| Flag | What it does |
|------|-------------|
| `--create-extension` | Also runs `CREATE EXTENSION` via `psql` |
| `--db myapp` | Which database to create the extension in |
| `--pg-config /path/...` | Override `pg_config` location |
| `--dry-run` | Show what would happen without doing it |
| `--uninstall` | Remove the extension (with `--create-extension`: also `DROP EXTENSION`) |

**Upgrade workflow:**

1. Rebuild: `plgo --installer .`
2. Copy new `install-pgpet.sh` to server
3. `sudo ./install-pgpet.sh --create-extension --db myapp`
4. If the extension was already loaded: `DROP EXTENSION pgpet; CREATE EXTENSION pgpet;`

### Option B: Copy build/ directory

The traditional approach — copy the `build/` directory and use `make install`:

```bash
# Copy the build/ directory to the server
scp -r build/ prod-server:/tmp/pgpet/

# SSH in and install
ssh prod-server
cd /tmp/pgpet
sudo make install
```

### Enable in your database

```sql
-- As a superuser
CREATE EXTENSION pgpet;
```

### Upgrade workflow

When you change your Go code:

1. Rebuild: `plgo .`
2. Copy new `build/` to server
3. `sudo make install`
4. In psql: `DROP EXTENSION pgpet; CREATE EXTENSION pgpet;`

(PostgreSQL extension versioning is a deeper topic — for now, the
drop-and-recreate approach works fine.)

---

## Epilogue: The Full Picture

```
                    You write this:
                    ┌─────────────────────────┐
                    │  package main            │
                    │                          │
                    │  func Greet(s string)    │
                    │      string {            │
                    │    return "Hi " + s      │
                    │  }                       │
                    └───────────┬──────────────┘
                                │
                          plgo .│
                                │
                    ┌───────────▼──────────────┐
                    │  ┌── parse (go/ast)       │
                    │  ├── classify functions   │
                    │  ├── generate CGo wrapper │
                    │  ├── inject PG macros     │
                    │  ├── go build -c-shared   │
                    │  └── emit SQL/Makefile    │
                    └───────────┬──────────────┘
                                │
                    ┌───────────▼──────────────┐
                    │  build/                   │
                    │  ├── myext.so             │
                    │  ├── myext--0.1.sql       │
                    │  ├── myext.control        │
                    │  └── Makefile             │
                    └───────────┬──────────────┘
                                │
                    --installer flag also produces:
                    install-myext.sh  (self-contained)
                                │
                    sudo make install
                      ── or ──
                    sudo ./install-myext.sh
                                │
                    ┌───────────▼──────────────┐
                    │  PostgreSQL               │
                    │                           │
                    │  CREATE EXTENSION myext;  │
                    │  SELECT greet('world');   │
                    │  → 'Hi world'             │
                    └───────────────────────────┘
```

That's the whole pipeline. Go in, PostgreSQL extension out.

Now go build something awesome. Your database is waiting. 🐘

---

_This tutorial is part of [plgo](https://gitlab.com/microo8/plgo) — Write
PostgreSQL extensions in Go._
