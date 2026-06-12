package backup

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/sirupsen/logrus"
)

// DatabaseOperations handles real database-specific operations
type DatabaseOperations struct {
	logger *logrus.Logger
}

// NewDatabaseOperations creates a new database operations instance
func NewDatabaseOperations(logger *logrus.Logger) *DatabaseOperations {
	return &DatabaseOperations{
		logger: logger,
	}
}

// RestoreDatabase restores a database from backup
func (dbo *DatabaseOperations) RestoreDatabase(dbType, backupPath, host string, port int, username, password, database string) error {
	switch dbType {
	case "mysql":
		return dbo.restoreMySQL(backupPath, host, port, username, password, database)
	case "postgresql":
		return dbo.restorePostgreSQL(backupPath, host, port, username, password, database)
	default:
		return fmt.Errorf("unsupported database type: %s", dbType)
	}
}

// restoreMySQL restores a MySQL database from backup
func (dbo *DatabaseOperations) restoreMySQL(backupPath, host string, port int, username, password, database string) error {
	dbo.logger.WithFields(logrus.Fields{
		"backup_path": backupPath,
		"host":        host,
		"port":        port,
		"database":    database,
	}).Info("Restoring MySQL database")

	// Connect to MySQL
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", username, password, host, port, database)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to connect to MySQL: %w", err)
	}
	defer db.Close()

	// Read backup file
	backupData, err := ReadBackupFile(backupPath)
	if err != nil {
		return fmt.Errorf("failed to read backup file: %w", err)
	}

	// Execute restoration
	statements := strings.Split(string(backupData), ";")
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}

		if _, err := db.Exec(stmt); err != nil {
			dbo.logger.WithError(err).WithField("statement", stmt[:min(100, len(stmt))]).Warning("Failed to execute statement")
			// Continue with other statements
		}
	}

	dbo.logger.Info("MySQL database restored successfully")
	return nil
}

// postgresRestoreTimeout bounds how long a PostgreSQL restore may run.
const postgresRestoreTimeout = 30 * time.Minute

// pgCustomDumpMagic is the magic prefix of PostgreSQL custom-format dumps
// (produced by pg_dump --format=custom, which is how this package creates
// PostgreSQL backups; see performPostgreSQLBackup).
var pgCustomDumpMagic = []byte("PGDMP")

// isPGCustomDump reports whether the (decompressed) header bytes identify a
// PostgreSQL custom-format dump.
func isPGCustomDump(header []byte) bool {
	return bytes.HasPrefix(header, pgCustomDumpMagic)
}

// isGzipData reports whether the header bytes identify gzip-compressed data.
func isGzipData(header []byte) bool {
	return len(header) >= 2 && header[0] == 0x1f && header[1] == 0x8b
}

// backupStream is an io.ReadCloser over a (possibly encrypted and/or
// gzip-compressed) backup file.
type backupStream struct {
	io.Reader
	closers []io.Closer
}

func (s *backupStream) Close() error {
	var firstErr error
	for _, closer := range s.closers {
		if err := closer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// openBackupStream opens a backup file for streaming, transparently handling
// encryption (.enc suffix, matching ReadBackupFile) and gzip compression
// (detected by magic bytes rather than extension).
func openBackupStream(backupPath string) (io.ReadCloser, error) {
	cleaned := filepath.Clean(backupPath)
	if !filepath.IsAbs(cleaned) {
		return nil, fmt.Errorf("backup path must be absolute: %s", backupPath)
	}
	if strings.Contains(cleaned, "..") {
		return nil, fmt.Errorf("backup path must not contain traversal segments: %s", backupPath)
	}

	info, err := os.Stat(cleaned)
	if err != nil {
		return nil, fmt.Errorf("failed to stat backup file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("backup path is not a regular file: %s", backupPath)
	}

	file, err := os.Open(cleaned) // #nosec G304 -- path is operator-supplied restore configuration, validated above
	if err != nil {
		return nil, fmt.Errorf("failed to open backup file: %w", err)
	}

	var reader io.Reader = file
	if strings.HasSuffix(cleaned, ".enc") {
		decrypted, err := createDecryptionReader(file)
		if err != nil {
			if closeErr := file.Close(); closeErr != nil {
				err = fmt.Errorf("%w (also failed to close backup file: %v)", err, closeErr)
			}
			return nil, fmt.Errorf("failed to decrypt backup file: %w", err)
		}
		reader = decrypted
	}

	buffered := bufio.NewReader(reader)
	header, err := buffered.Peek(2)
	if err == nil && isGzipData(header) {
		gzipReader, err := gzip.NewReader(buffered)
		if err != nil {
			if closeErr := file.Close(); closeErr != nil {
				err = fmt.Errorf("%w (also failed to close backup file: %v)", err, closeErr)
			}
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		return &backupStream{Reader: gzipReader, closers: []io.Closer{gzipReader, file}}, nil
	}

	return &backupStream{Reader: buffered, closers: []io.Closer{file}}, nil
}

// truncateString shortens a string for error reporting.
func truncateString(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "... (truncated)"
}

// restorePostgreSQL restores a PostgreSQL database from backup.
//
// Backups in this package are created with pg_dump (see
// performPostgreSQLBackup), so restores use the matching client tools:
// pg_restore for custom-format dumps (detected via the PGDMP magic bytes)
// and psql for plain SQL dumps. Both read the backup from stdin so that
// compressed and encrypted backups can be streamed without temp files.
func (dbo *DatabaseOperations) restorePostgreSQL(backupPath, host string, port int, username, password, database string) error {
	dbo.logger.WithFields(logrus.Fields{
		"backup_path": backupPath,
		"host":        host,
		"port":        port,
		"database":    database,
	}).Info("Restoring PostgreSQL database")

	// Validate the backup file exists, is a regular file, and is non-empty.
	if err := ValidateBackupFile(backupPath); err != nil {
		return fmt.Errorf("invalid backup file: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), postgresRestoreTimeout)
	defer cancel()

	stream, err := openBackupStream(backupPath)
	if err != nil {
		return err
	}
	defer stream.Close()

	// Peek the decompressed header to detect the dump format.
	buffered := bufio.NewReaderSize(stream, 64*1024)
	header, err := buffered.Peek(len(pgCustomDumpMagic))
	if err != nil && err != io.EOF {
		return fmt.Errorf("failed to read backup header: %w", err)
	}

	connArgs := []string{
		"--host", host,
		"--port", strconv.Itoa(port),
		"--username", username,
		"--dbname", database,
		"--no-password",
	}

	var cmd *exec.Cmd
	if isPGCustomDump(header) {
		dbo.logger.Info("Detected PostgreSQL custom-format dump, using pg_restore")
		args := append([]string{
			"--clean",
			"--if-exists",
			"--no-owner",
			"--exit-on-error",
		}, connArgs...)
		// #nosec G204 -- binary name is hardcoded; args are structured connection parameters
		cmd = exec.CommandContext(ctx, "pg_restore", args...)
	} else {
		dbo.logger.Info("Detected plain SQL dump, using psql")
		args := append([]string{
			"--set", "ON_ERROR_STOP=1",
			"--single-transaction",
			"--file", "-",
		}, connArgs...)
		// #nosec G204 -- binary name is hardcoded; args are structured connection parameters
		cmd = exec.CommandContext(ctx, "psql", args...)
	}

	cmd.Stdin = buffered
	cmd.Env = append(os.Environ(), "PGPASSWORD="+password)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("PostgreSQL restore timed out after %v: %w", postgresRestoreTimeout, err)
		}
		return fmt.Errorf("PostgreSQL restore failed: %w; stderr: %s", err, truncateString(stderr.String(), 2000))
	}

	if stderr.Len() > 0 {
		dbo.logger.WithField("stderr", truncateString(stderr.String(), 500)).Debug("PostgreSQL restore completed with warnings")
	}

	dbo.logger.Info("PostgreSQL database restored successfully")
	return nil
}

// Helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
