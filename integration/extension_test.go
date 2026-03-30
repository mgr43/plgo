//go:build integration

package integration

import (
	"database/sql"
	"encoding/hex"
	"testing"
)

// ---------------------------------------------------------------------------
// Extension loading
// ---------------------------------------------------------------------------

func TestCreateExtensionExample(t *testing.T) {
	_, err := testDB.Exec("CREATE EXTENSION IF NOT EXISTS example")
	if err != nil {
		t.Fatalf("CREATE EXTENSION example: %v", err)
	}
}

func TestCreateExtensionTest(t *testing.T) {
	_, err := testDB.Exec("CREATE EXTENSION IF NOT EXISTS test")
	if err != nil {
		t.Fatalf("CREATE EXTENSION test: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Example extension functions
// ---------------------------------------------------------------------------

func TestConcatArray(t *testing.T) {
	mustExec(t, "CREATE EXTENSION IF NOT EXISTS example")

	tests := []struct {
		name  string
		query string
		want  string
	}{
		{"words", "SELECT concatarray(ARRAY['hello', ' ', 'world'])", "hello world"},
		{"letters", "SELECT concatarray(ARRAY['a', 'b', 'c'])", "abc"},
		{"empty", "SELECT concatarray(ARRAY[]::text[])", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			if err := testDB.QueryRow(tt.query).Scan(&got); err != nil {
				t.Fatalf("query %q: %v", tt.query, err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGzipCompress(t *testing.T) {
	mustExec(t, "CREATE EXTENSION IF NOT EXISTS example")

	var works bool
	err := testDB.QueryRow("SELECT length(gzipcompress('hello world'::bytea)) > 0").Scan(&works)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !works {
		t.Error("gzipcompress returned empty or zero-length result")
	}
}

func TestTrigger(t *testing.T) {
	mustExec(t, "CREATE EXTENSION IF NOT EXISTS example")
	mustExec(t, "DROP TABLE IF EXISTS test_trigger")
	mustExec(t, "CREATE TABLE test_trigger (id integer, value text)")
	mustExec(t, `CREATE TRIGGER test_created_time_trigger
		BEFORE INSERT ON test_trigger
		FOR EACH ROW EXECUTE FUNCTION createdtimetrigger()`)
	mustExec(t, "INSERT INTO test_trigger VALUES (1, 'abc')")

	var id int
	var value string
	err := testDB.QueryRow("SELECT id, value FROM test_trigger").Scan(&id, &value)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	// The trigger adds 10 to id and doubles the value string
	if id != 11 {
		t.Errorf("id = %d, want 11", id)
	}
	if value != "abcabc" {
		t.Errorf("value = %q, want %q", value, "abcabc")
	}

	mustExec(t, "DROP TABLE test_trigger")
}

// ---------------------------------------------------------------------------
// Test extension functions
// ---------------------------------------------------------------------------

func TestPLGoTest(t *testing.T) {
	mustExec(t, "CREATE EXTENSION IF NOT EXISTS test")

	// plgotest() exercises all SPI type conversions internally.
	// It returns void (NULL). If any internal check fails, it calls log.Fatal
	// which raises an ERROR inside PG and the query will fail.
	var result sql.NullString
	err := testDB.QueryRow("SELECT plgotest()").Scan(&result)
	if err != nil {
		t.Fatalf("plgotest() failed: %v", err)
	}
}

func TestReverseBytea(t *testing.T) {
	mustExec(t, "CREATE EXTENSION IF NOT EXISTS test")

	var got []byte
	err := testDB.QueryRow("SELECT reversebytea('hello'::bytea)").Scan(&got)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	want := "olleh"
	if string(got) != want {
		t.Errorf("got %s (hex: %s), want %q", string(got), hex.EncodeToString(got), want)
	}
}

func TestStringArrayReturn(t *testing.T) {
	mustExec(t, "CREATE EXTENSION IF NOT EXISTS test")

	var got string
	err := testDB.QueryRow("SELECT stringarrayreturn()").Scan(&got)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	want := "{a,b,c}"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// SETOF (set-returning) functions — test extension
// ---------------------------------------------------------------------------

func TestGenerateInts(t *testing.T) {
	mustExec(t, "CREATE EXTENSION IF NOT EXISTS test")

	rows, err := testDB.Query("SELECT * FROM generateints(5)")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var got []int32
	for rows.Next() {
		var v int32
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows iteration: %v", err)
	}

	want := []int32{1, 2, 3, 4, 5}
	if !int32SliceEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestGenerateIntsEmpty(t *testing.T) {
	mustExec(t, "CREATE EXTENSION IF NOT EXISTS test")

	rows, err := testDB.Query("SELECT * FROM generateints(0)")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows iteration: %v", err)
	}
	if count != 0 {
		t.Errorf("got %d rows, want 0", count)
	}
}

func TestGenerateWords(t *testing.T) {
	mustExec(t, "CREATE EXTENSION IF NOT EXISTS test")

	rows, err := testDB.Query("SELECT * FROM generatewords()")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows iteration: %v", err)
	}

	want := []string{"hello", "world", "from", "plgo"}
	if !stringSliceEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// SETOF (set-returning) functions — example extension
// ---------------------------------------------------------------------------

func TestGenerateSeries(t *testing.T) {
	mustExec(t, "CREATE EXTENSION IF NOT EXISTS example")

	rows, err := testDB.Query("SELECT * FROM generateseries(3, 7)")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var got []int32
	for rows.Next() {
		var v int32
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows iteration: %v", err)
	}

	want := []int32{3, 4, 5, 6, 7}
	if !int32SliceEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRepeatString(t *testing.T) {
	mustExec(t, "CREATE EXTENSION IF NOT EXISTS example")

	rows, err := testDB.Query("SELECT * FROM repeatstring('go', 3)")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows iteration: %v", err)
	}

	want := []string{"go", "go", "go"}
	if !stringSliceEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// mustExec executes a SQL statement and fails the test on error.
func mustExec(t *testing.T, query string) {
	t.Helper()
	if _, err := testDB.Exec(query); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func int32SliceEqual(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

