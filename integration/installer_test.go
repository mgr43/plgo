//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// installerContainer and installerDB are set up once per test run
// by TestInstallerSuite which acts as the entry point for all installer tests.
// We cannot use TestMain (already used for the main container), so we use
// a top-level test with subtests instead.

// setupInstallerContainer starts a clean PG 18 container that has the
// installer scripts baked in but NO extensions pre-installed.
func setupInstallerContainer(ctx context.Context) (testcontainers.Container, *sql.DB, error) {
	root := projectRoot()

	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:    root,
			Dockerfile: "integration/Dockerfile.installer",
		},
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "postgres",
			"POSTGRES_PASSWORD": "postgres",
			"POSTGRES_DB":       "plgo_test",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(3 * time.Minute),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("start installer container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, nil, fmt.Errorf("get host: %w", err)
	}
	mappedPort, err := container.MappedPort(ctx, "5432")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, nil, fmt.Errorf("get mapped port: %w", err)
	}

	dsn := fmt.Sprintf("host=%s port=%s user=postgres password=postgres dbname=plgo_test sslmode=disable",
		host, mappedPort.Port())

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, nil, fmt.Errorf("open database: %w", err)
	}

	for i := 0; i < 30; i++ {
		if err = db.PingContext(ctx); err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		db.Close()
		_ = container.Terminate(ctx)
		return nil, nil, fmt.Errorf("database not ready: %w", err)
	}

	return container, db, nil
}

// execInContainer runs a command inside the container and returns stdout+stderr.
func execInContainer(ctx context.Context, container testcontainers.Container, cmd ...string) (string, error) {
	code, reader, err := container.Exec(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("exec %v: %w", cmd, err)
	}
	buf := new(strings.Builder)
	if reader != nil {
		b := make([]byte, 4096)
		for {
			n, readErr := reader.Read(b)
			if n > 0 {
				buf.Write(b[:n])
			}
			if readErr != nil {
				break
			}
		}
	}
	if code != 0 {
		return buf.String(), fmt.Errorf("command %v exited with code %d: %s", cmd, code, buf.String())
	}
	return buf.String(), nil
}

// TestInstallerSuite spins up a clean PG container (no extensions installed),
// runs the installer scripts, and verifies they work end-to-end.
// It runs in parallel with the extension tests (which use a separate container).
func TestInstallerSuite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	container, db, err := setupInstallerContainer(ctx)
	if err != nil {
		t.Fatalf("setup installer container: %v", err)
	}
	defer func() {
		db.Close()
		_ = container.Terminate(ctx)
	}()

	// --- Verify extensions are NOT installed yet ---
	t.Run("ExtensionsNotPreInstalled", func(t *testing.T) {
		_, err := db.Exec("CREATE EXTENSION example")
		if err == nil {
			t.Fatal("example extension should NOT be installed yet")
		}
	})

	// --- Run the installer scripts (install files only, no CREATE EXTENSION) ---
	t.Run("RunInstallerExample", func(t *testing.T) {
		out, err := execInContainer(ctx, container, "sh", "/installers/install-example.sh")
		if err != nil {
			t.Fatalf("installer failed: %v\noutput: %s", err, out)
		}
		t.Logf("installer output:\n%s", out)
	})

	t.Run("RunInstallerTest", func(t *testing.T) {
		out, err := execInContainer(ctx, container, "sh", "/installers/install-test.sh")
		if err != nil {
			t.Fatalf("installer failed: %v\noutput: %s", err, out)
		}
		t.Logf("installer output:\n%s", out)
	})

	// --- Verify extensions load after installer ---
	t.Run("CreateExtensionExample", func(t *testing.T) {
		_, err := db.Exec("CREATE EXTENSION IF NOT EXISTS example")
		if err != nil {
			t.Fatalf("CREATE EXTENSION example: %v", err)
		}
	})

	t.Run("CreateExtensionTest", func(t *testing.T) {
		_, err := db.Exec("CREATE EXTENSION IF NOT EXISTS test")
		if err != nil {
			t.Fatalf("CREATE EXTENSION test: %v", err)
		}
	})

	// --- Verify a function from each extension works ---
	t.Run("ExampleFunctionWorks", func(t *testing.T) {
		var got string
		err := db.QueryRow("SELECT concatarray(ARRAY['hello', ' ', 'world'])").Scan(&got)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if got != "hello world" {
			t.Errorf("got %q, want %q", got, "hello world")
		}
	})

	t.Run("TestFunctionWorks", func(t *testing.T) {
		var got string
		err := db.QueryRow("SELECT stringarrayreturn()").Scan(&got)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if got != "{a,b,c}" {
			t.Errorf("got %q, want %q", got, "{a,b,c}")
		}
	})

	// --- Test --create-extension flag ---
	// First drop the extensions, then re-install with --create-extension
	t.Run("InstallerCreateExtension", func(t *testing.T) {
		// Drop the extensions we created above
		db.Exec("DROP EXTENSION IF EXISTS example CASCADE")
		db.Exec("DROP EXTENSION IF EXISTS test CASCADE")

		// Re-run installer with --create-extension --db plgo_test
		// Use env to set PGUSER=postgres since container.Exec runs as root
		out, err := execInContainer(ctx, container,
			"sh", "-c", "PGUSER=postgres /installers/install-example.sh --create-extension --db plgo_test")
		if err != nil {
			t.Fatalf("installer --create-extension failed: %v\noutput: %s", err, out)
		}
		t.Logf("installer --create-extension output:\n%s", out)

		// Verify the extension was auto-created — call a function
		var got string
		err = db.QueryRow("SELECT concatarray(ARRAY['a', 'b'])").Scan(&got)
		if err != nil {
			t.Fatalf("function call after --create-extension: %v", err)
		}
		if got != "ab" {
			t.Errorf("got %q, want %q", got, "ab")
		}
	})

	// --- Test --uninstall ---
	t.Run("InstallerUninstall", func(t *testing.T) {
		// Uninstall with --create-extension to also DROP EXTENSION
		out, err := execInContainer(ctx, container,
			"sh", "-c", "PGUSER=postgres /installers/install-example.sh --uninstall --create-extension --db plgo_test")
		if err != nil {
			t.Fatalf("installer --uninstall failed: %v\noutput: %s", err, out)
		}
		t.Logf("uninstall output:\n%s", out)

		// Verify the extension is gone — CREATE EXTENSION should fail
		_, err = db.Exec("CREATE EXTENSION example")
		if err == nil {
			t.Fatal("example extension should be uninstalled but CREATE EXTENSION succeeded")
		}
	})
}
