package backup

import (
	"bufio"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestIsPGCustomDump(t *testing.T) {
	tests := []struct {
		name     string
		header   []byte
		expected bool
	}{
		{"custom format dump", []byte("PGDMP\x01\x0e\x00"), true},
		{"exact magic", []byte("PGDMP"), true},
		{"plain SQL dump", []byte("-- PostgreSQL database dump"), false},
		{"empty", nil, false},
		{"short header", []byte("PG"), false},
		{"gzip data", []byte{0x1f, 0x8b, 0x08}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPGCustomDump(tt.header); got != tt.expected {
				t.Errorf("isPGCustomDump(%q) = %v, want %v", tt.header, got, tt.expected)
			}
		})
	}
}

func TestIsGzipData(t *testing.T) {
	tests := []struct {
		name     string
		header   []byte
		expected bool
	}{
		{"gzip magic", []byte{0x1f, 0x8b, 0x08, 0x00}, true},
		{"plain text", []byte("-- SQL"), false},
		{"empty", nil, false},
		{"one byte", []byte{0x1f}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGzipData(tt.header); got != tt.expected {
				t.Errorf("isGzipData(%v) = %v, want %v", tt.header, got, tt.expected)
			}
		})
	}
}

func TestOpenBackupStreamPlain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backup.sql")
	content := "-- PostgreSQL database dump\nCREATE TABLE test (id int);\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	stream, err := openBackupStream(path)
	if err != nil {
		t.Fatalf("openBackupStream: %v", err)
	}
	defer stream.Close()

	data, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != content {
		t.Errorf("content mismatch: got %q", data)
	}
}

func TestOpenBackupStreamGzip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backup.sql.gz")

	// Simulate a gzip-compressed custom-format dump (as created by
	// performPostgreSQLBackup with compression enabled).
	payload := append([]byte("PGDMP"), []byte("\x01\x0e\x00binary-dump-data")...)

	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	if _, err := gzipWriter.Write(payload); err != nil {
		t.Fatal(err)
	}
	gzipWriter.Close()
	file.Close()

	stream, err := openBackupStream(path)
	if err != nil {
		t.Fatalf("openBackupStream: %v", err)
	}
	defer stream.Close()

	// The decompressed stream should expose the PGDMP magic for detection.
	buffered := bufio.NewReader(stream)
	header, err := buffered.Peek(len(pgCustomDumpMagic))
	if err != nil {
		t.Fatalf("peek: %v", err)
	}
	if !isPGCustomDump(header) {
		t.Errorf("expected decompressed header to be detected as custom dump, got %q", header)
	}

	data, err := io.ReadAll(buffered)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != string(payload) {
		t.Errorf("decompressed content mismatch: got %q, want %q", data, payload)
	}
}

func TestRestorePostgreSQLMissingFile(t *testing.T) {
	dbo := NewDatabaseOperations(logrus.New())

	err := dbo.restorePostgreSQL("/nonexistent/backup.sql", "localhost", 5432, "user", "pass", "db")
	if err == nil {
		t.Fatal("expected error for missing backup file")
	}
	if !strings.Contains(err.Error(), "invalid backup file") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRestorePostgreSQLEmptyFile(t *testing.T) {
	dbo := NewDatabaseOperations(logrus.New())

	dir := t.TempDir()
	path := filepath.Join(dir, "empty.sql")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}

	err := dbo.restorePostgreSQL(path, "localhost", 5432, "user", "pass", "db")
	if err == nil {
		t.Fatal("expected error for empty backup file")
	}
	if !strings.Contains(err.Error(), "invalid backup file") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRestoreDatabaseUnsupportedType(t *testing.T) {
	dbo := NewDatabaseOperations(logrus.New())

	err := dbo.RestoreDatabase("oracle", "/tmp/backup.dmp", "localhost", 1521, "user", "pass", "db")
	if err == nil || !strings.Contains(err.Error(), "unsupported database type") {
		t.Errorf("expected unsupported database type error, got: %v", err)
	}
}

func TestTruncateString(t *testing.T) {
	if got := truncateString("  hello  ", 10); got != "hello" {
		t.Errorf("expected trimmed string, got %q", got)
	}
	if got := truncateString("abcdefghij", 4); got != "abcd... (truncated)" {
		t.Errorf("unexpected truncation: %q", got)
	}
}
