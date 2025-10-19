//go:build !mysql
// +build !mysql

package database

import (
	"context"
	"database/sql"
	"time"

	"github.com/sirupsen/logrus"
)

// MySQLConfig holds MySQL configuration fields. When MySQL support is disabled,
// the struct is still available so callers can compile, but InitializeDatabase
// will always return ErrMySQLDisabled.
type MySQLConfig struct {
	Host            string
	Port            int
	Database        string
	Username        string
	Password        string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	SSLMode         string
	Charset         string
	ParseTime       bool
	Loc             string
}

// MySQLDatabase is a stub implementation used when MySQL support is disabled.
type MySQLDatabase struct {
	db     *sql.DB
	config MySQLConfig
	logger *logrus.Logger
}

// NewMySQLDatabase always returns ErrMySQLDisabled in builds without the mysql tag.
func NewMySQLDatabase(config MySQLConfig, logger *logrus.Logger) (*MySQLDatabase, error) {
	if logger != nil {
		logger.Warn("MySQL support disabled at build time; database connection not created")
	}
	return nil, ErrMySQLDisabled
}

// Close is a no-op when MySQL support is disabled.
func (m *MySQLDatabase) Close() error {
	return nil
}

// Health reports that MySQL is unavailable in this build.
func (m *MySQLDatabase) Health() error {
	return ErrMySQLDisabled
}

// Migrate reports that migrations cannot run without MySQL support.
func (m *MySQLDatabase) Migrate() error {
	return ErrMySQLDisabled
}

// getContext returns a cancellable context with a short timeout so callers can compile.
func (m *MySQLDatabase) getContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}
