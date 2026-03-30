package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBase64Encode(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{"empty", nil},
		{"short", []byte("hello")},
		{"exactly 76 chars decoded", bytes.Repeat([]byte("A"), 57)}, // 57 bytes → 76 base64 chars
		{"long binary", bytes.Repeat([]byte{0xff, 0x00, 0xab}, 100)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := base64Encode(tt.input)
			// Every line should be ≤ 76 chars
			for i, line := range strings.Split(encoded, "\n") {
				if len(line) > 76 {
					t.Errorf("line %d has %d chars (max 76): %q", i, len(line), line[:40]+"...")
				}
			}
			// Round-trip: decode must match original
			clean := strings.ReplaceAll(encoded, "\n", "")
			decoded, err := base64.StdEncoding.DecodeString(clean)
			if err != nil {
				t.Fatalf("decode error: %v", err)
			}
			if !bytes.Equal(decoded, tt.input) {
				t.Error("round-trip mismatch")
			}
		})
	}
}

func TestHumanSize(t *testing.T) {
	tests := []struct {
		bytes int
		want  string
	}{
		{0, "0 bytes"},
		{512, "512 bytes"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1572864, "1.5 MB"},
	}
	for _, tt := range tests {
		got := humanSize(tt.bytes)
		if got != tt.want {
			t.Errorf("humanSize(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestInstallerShellTemplate(t *testing.T) {
	var buf bytes.Buffer
	err := installerShellTemplate.Execute(&buf, installerData{
		ExtensionName: "myext",
		Version:       "0.1",
	})
	if err != nil {
		t.Fatalf("template execute: %v", err)
	}

	script := buf.String()

	// Must start with shebang
	if !strings.HasPrefix(script, "#!/bin/sh\n") {
		t.Error("script must start with #!/bin/sh")
	}

	// Must contain the extension name
	if !strings.Contains(script, `EXTENSION="myext"`) {
		t.Error("script must set EXTENSION variable")
	}

	// Must contain the version
	if !strings.Contains(script, `VERSION="0.1"`) {
		t.Error("script must set VERSION variable")
	}

	// Must contain the payload marker
	if !strings.Contains(script, "__PAYLOAD_BELOW__") {
		t.Error("script must end with __PAYLOAD_BELOW__ marker")
	}

	// Must end with the payload marker as the last line
	if !strings.HasSuffix(strings.TrimSpace(script), "__PAYLOAD_BELOW__") {
		t.Error("__PAYLOAD_BELOW__ must be the last line of the template")
	}

	// Must contain key features
	checks := []string{
		"pg_config",
		"--create-extension",
		"--uninstall",
		"--dry-run",
		"--help",
		"install -m 644",
		"install -m 755",
		"CREATE EXTENSION",
		"base64 -d",
		"__CONTROL__",
		"__SQL__",
		"__SO__",
		"set -e",
		"trap",
	}
	for _, check := range checks {
		if !strings.Contains(script, check) {
			t.Errorf("script missing expected content: %q", check)
		}
	}

	// Verify the generated script is valid shell syntax
	tmpFile := filepath.Join(t.TempDir(), "test-installer.sh")
	if err := os.WriteFile(tmpFile, buf.Bytes(), 0o755); err != nil {
		t.Fatalf("write temp script: %v", err)
	}
	if out, err := exec.Command("sh", "-n", tmpFile).CombinedOutput(); err != nil {
		t.Errorf("shell syntax check failed: %v\n%s", err, out)
	}
}

func TestWriteInstaller(t *testing.T) {
	// Create fake build artifacts
	buildDir := t.TempDir()
	extName := "testpkg"

	soContent := []byte{0x7f, 'E', 'L', 'F', 0x00, 0x01, 0x02, 0x03} // fake ELF header
	sqlContent := []byte(`\echo Use "CREATE EXTENSION testpkg" to load this file. \quit
CREATE OR REPLACE FUNCTION hello(name text)
RETURNS text AS
'$libdir/testpkg', 'hello'
LANGUAGE c VOLATILE STRICT;
`)
	controlContent := []byte(`# testpkg extension
comment = 'testpkg extension'
default_version = '0.1'
relocatable = true`)

	os.WriteFile(filepath.Join(buildDir, extName+".so"), soContent, 0o755)
	os.WriteFile(filepath.Join(buildDir, extName+"--0.1.sql"), sqlContent, 0o644)
	os.WriteFile(filepath.Join(buildDir, extName+".control"), controlContent, 0o644)

	// Create a ModuleWriter with just the package name
	mw := &ModuleWriter{PackageName: extName}

	// Write the installer
	outPath := filepath.Join(t.TempDir(), "install-testpkg.sh")
	err := mw.WriteInstaller(buildDir, outPath)
	if err != nil {
		t.Fatalf("WriteInstaller: %v", err)
	}

	// Read the output
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read installer: %v", err)
	}
	script := string(data)

	// Verify it's executable
	info, _ := os.Stat(outPath)
	if info.Mode()&0o111 == 0 {
		t.Error("installer should be executable")
	}

	// Verify structure: header, then marker, then payload sections
	parts := strings.SplitN(script, "__PAYLOAD_BELOW__\n", 2)
	if len(parts) != 2 {
		t.Fatal("installer must have header + payload separated by __PAYLOAD_BELOW__")
	}

	header := parts[0]
	payload := parts[1]

	// Header checks
	if !strings.HasPrefix(header, "#!/bin/sh\n") {
		t.Error("header must start with shebang")
	}
	if !strings.Contains(header, `EXTENSION="testpkg"`) {
		t.Error("header must contain extension name")
	}

	// Payload must have all three sections
	if !strings.Contains(payload, "__CONTROL__") {
		t.Error("payload missing __CONTROL__ section")
	}
	if !strings.Contains(payload, "__SQL__") {
		t.Error("payload missing __SQL__ section")
	}
	if !strings.Contains(payload, "__SO__") {
		t.Error("payload missing __SO__ section")
	}

	// Verify we can decode the .so back
	soSection := extractSection(payload, "__SO__")
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(soSection, "\n", ""))
	if err != nil {
		t.Fatalf("decode .so section: %v", err)
	}
	if !bytes.Equal(decoded, soContent) {
		t.Error(".so round-trip mismatch")
	}

	// Verify we can decode the .control back
	controlSection := extractSection(payload, "__CONTROL__")
	decoded, err = base64.StdEncoding.DecodeString(strings.ReplaceAll(controlSection, "\n", ""))
	if err != nil {
		t.Fatalf("decode .control section: %v", err)
	}
	if !bytes.Equal(decoded, controlContent) {
		t.Error(".control round-trip mismatch")
	}

	// Verify we can decode the .sql back
	sqlSection := extractSection(payload, "__SQL__")
	decoded, err = base64.StdEncoding.DecodeString(strings.ReplaceAll(sqlSection, "\n", ""))
	if err != nil {
		t.Fatalf("decode .sql section: %v", err)
	}
	if !bytes.Equal(decoded, sqlContent) {
		t.Error(".sql round-trip mismatch")
	}
}

// extractSection extracts the base64 content between a section marker and the next marker (or EOF).
func extractSection(payload, marker string) string {
	// Find the marker line
	idx := strings.Index(payload, marker+"\n")
	if idx < 0 {
		return ""
	}
	rest := payload[idx+len(marker)+1:]

	// Find the next marker or EOF
	end := len(rest)
	for _, m := range []string{"__CONTROL__", "__SQL__", "__SO__"} {
		if m == marker {
			continue
		}
		if i := strings.Index(rest, "\n"+m+"\n"); i >= 0 && i < end {
			end = i
		}
	}
	return strings.TrimRight(rest[:end], "\n")
}
