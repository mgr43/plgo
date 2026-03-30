//go:build integration

// Package integration contains integration tests for plgo extensions.
// These tests use testcontainers-go to spin up a PostgreSQL 18 container
// with pre-built plgo extensions and run SQL assertions against them.
//
// Run with: go test -tags integration -v -timeout 5m ./integration/
package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// testDB is the shared database connection, set up once in TestMain.
var testDB *sql.DB

// projectRoot returns the absolute path to the plgo project root.
func projectRoot() string {
	// This file is at <root>/integration/integration_test.go
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(filename))
}

// setupContainer starts a PostgreSQL 18 container with plgo extensions
// pre-built and installed. It returns the container, a connected *sql.DB,
// and a cleanup function.
func setupContainer(ctx context.Context) (testcontainers.Container, *sql.DB, error) {
	root := projectRoot()

	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:    root,
			Dockerfile: "integration/Dockerfile",
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
		return nil, nil, fmt.Errorf("start container: %w", err)
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

	// Wait for the database to be ready
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

// TestMain starts the PostgreSQL container once, creates extensions, runs all tests, then cleans up.
func TestMain(m *testing.M) {
	ctx := context.Background()

	container, db, err := setupContainer(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "setup failed: %v\n", err)
		os.Exit(1)
	}

	// Pre-create extensions so parallel tests don't race on CREATE EXTENSION.
	for _, ext := range []string{"example", "test"} {
		if _, err := db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS "+ext); err != nil {
			fmt.Fprintf(os.Stderr, "CREATE EXTENSION %s: %v\n", ext, err)
			_ = container.Terminate(ctx)
			os.Exit(1)
		}
	}

	testDB = db

	code := m.Run()

	testDB.Close()
	_ = container.Terminate(ctx)

	os.Exit(code)
}
