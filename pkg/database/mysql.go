package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/sirupsen/logrus"
)

// MySQLConfig holds MySQL connection configuration
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

// MySQLDatabase represents a MySQL database connection
type MySQLDatabase struct {
	db     *sql.DB
	config MySQLConfig
	logger *logrus.Logger
}

// NewMySQLDatabase creates a new MySQL database connection
func NewMySQLDatabase(config MySQLConfig, logger *logrus.Logger) (*MySQLDatabase, error) {
	// Build DSN (Data Source Name)
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=%t&loc=%s",
		config.Username,
		config.Password,
		config.Host,
		config.Port,
		config.Database,
		config.Charset,
		config.ParseTime,
		config.Loc,
	)

	if config.SSLMode != "" {
		dsn += "&tls=" + config.SSLMode
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(config.MaxOpenConns)
	db.SetMaxIdleConns(config.MaxIdleConns)
	db.SetConnMaxLifetime(config.ConnMaxLifetime)
	db.SetConnMaxIdleTime(config.ConnMaxIdleTime)

	// Test the connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	mysql := &MySQLDatabase{
		db:     db,
		config: config,
		logger: logger,
	}

	logger.WithFields(logrus.Fields{
		"host":     config.Host,
		"port":     config.Port,
		"database": config.Database,
	}).Info("Connected to MySQL database")

	return mysql, nil
}

// Close closes the database connection
func (m *MySQLDatabase) Close() error {
	if m.db != nil {
		return m.db.Close()
	}
	return nil
}

// Health checks database health
func (m *MySQLDatabase) Health() error {
	ctx, cancel := m.getContext()
	defer cancel()

	if err := m.db.PingContext(ctx); err != nil {
		return fmt.Errorf("database health check failed: %w", err)
	}

	return nil
}

// Migrate runs database migrations
func (m *MySQLDatabase) Migrate() error {
	migrations := []string{
		createSessionsTable,
		createParticipantsTable,
		createStreamsTable,
		createCDRTable,
		createEventsTable,
		createTranscriptionsTable,
		createUsersTable,
		createAPIKeysTable,
		createSearchIndexTable,
		createIndexes,
	}

	for i, migration := range migrations {
		m.logger.WithField("migration", i+1).Debug("Running migration")

		if _, err := m.db.Exec(migration); err != nil {
			return fmt.Errorf("migration %d failed: %w", i+1, err)
		}
	}

	m.logger.Info("Database migrations completed successfully")
	return nil
}

// getContext returns a context with timeout
func (m *MySQLDatabase) getContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// Database schema definitions
const createSessionsTable = `
CREATE TABLE IF NOT EXISTS sessions (
    id VARCHAR(36) PRIMARY KEY,
    call_id VARCHAR(255) NOT NULL,
    session_id VARCHAR(255) NOT NULL,
    status ENUM('active', 'completed', 'failed', 'terminated') NOT NULL DEFAULT 'active',
    transport ENUM('udp', 'tcp', 'tls') NOT NULL,
    source_ip VARCHAR(45) NOT NULL,
    source_port INT NOT NULL,
    local_ip VARCHAR(45) NOT NULL,
    local_port INT NOT NULL,
    start_time TIMESTAMP NOT NULL,
    end_time TIMESTAMP NULL,
    duration BIGINT NULL,
    recording_path VARCHAR(512) NOT NULL,
    metadata_xml TEXT NULL,
    sdp TEXT NULL,
    participants INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_call_id (call_id),
    INDEX idx_session_id (session_id),
    INDEX idx_status (status),
    INDEX idx_start_time (start_time),
    INDEX idx_source_ip (source_ip),
    FULLTEXT(metadata_xml)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`

const createParticipantsTable = `
CREATE TABLE IF NOT EXISTS participants (
    id VARCHAR(36) PRIMARY KEY,
    session_id VARCHAR(36) NOT NULL,
    participant_id VARCHAR(255) NOT NULL,
    type ENUM('caller', 'callee', 'observer') NOT NULL,
    name_id VARCHAR(255) NULL,
    display_name VARCHAR(255) NULL,
    aor VARCHAR(255) NULL,
    stream_id VARCHAR(255) NULL,
    join_time TIMESTAMP NOT NULL,
    leave_time TIMESTAMP NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE,
    INDEX idx_session_id (session_id),
    INDEX idx_participant_id (participant_id),
    INDEX idx_type (type),
    FULLTEXT(display_name, aor)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`

const createStreamsTable = `
CREATE TABLE IF NOT EXISTS streams (
    id VARCHAR(36) PRIMARY KEY,
    session_id VARCHAR(36) NOT NULL,
    stream_id VARCHAR(255) NOT NULL,
    label VARCHAR(255) NOT NULL,
    mode ENUM('separate', 'mixed') NOT NULL DEFAULT 'separate',
    direction ENUM('sendonly', 'recvonly', 'sendrecv') NOT NULL DEFAULT 'sendonly',
    codec VARCHAR(50) NULL,
    sample_rate INT NULL,
    channels INT NULL,
    start_time TIMESTAMP NOT NULL,
    end_time TIMESTAMP NULL,
    packet_count BIGINT NULL,
    byte_count BIGINT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE,
    INDEX idx_session_id (session_id),
    INDEX idx_stream_id (stream_id),
    INDEX idx_label (label),
    INDEX idx_codec (codec)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`

const createCDRTable = `
CREATE TABLE IF NOT EXISTS cdr (
    id VARCHAR(36) PRIMARY KEY,
    session_id VARCHAR(36) NOT NULL,
    call_id VARCHAR(255) NOT NULL,
    caller_id VARCHAR(255) NULL,
    callee_id VARCHAR(255) NULL,
    start_time TIMESTAMP NOT NULL,
    end_time TIMESTAMP NULL,
    duration BIGINT NULL,
    recording_path VARCHAR(512) NOT NULL,
    recording_size BIGINT NULL,
    transcription_id VARCHAR(36) NULL,
    quality DECIMAL(3,2) NULL,
    transport VARCHAR(10) NOT NULL,
    source_ip VARCHAR(45) NOT NULL,
    codec VARCHAR(50) NULL,
    sample_rate INT NULL,
    participant_count INT NOT NULL DEFAULT 0,
    stream_count INT NOT NULL DEFAULT 0,
    status ENUM('completed', 'failed', 'partial') NOT NULL DEFAULT 'completed',
    error_message TEXT NULL,
    billing_code VARCHAR(100) NULL,
    cost_center VARCHAR(100) NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE,
    INDEX idx_session_id (session_id),
    INDEX idx_call_id (call_id),
    INDEX idx_caller_id (caller_id),
    INDEX idx_callee_id (callee_id),
    INDEX idx_start_time (start_time),
    INDEX idx_duration (duration),
    INDEX idx_status (status),
    INDEX idx_billing_code (billing_code),
    INDEX idx_cost_center (cost_center),
    FULLTEXT(error_message)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`

const createEventsTable = `
CREATE TABLE IF NOT EXISTS events (
    id VARCHAR(36) PRIMARY KEY,
    session_id VARCHAR(36) NULL,
    type VARCHAR(100) NOT NULL,
    level ENUM('info', 'warning', 'error', 'critical') NOT NULL DEFAULT 'info',
    message TEXT NOT NULL,
    source VARCHAR(100) NOT NULL,
    source_ip VARCHAR(45) NULL,
    user_agent VARCHAR(500) NULL,
    metadata JSON NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE SET NULL,
    INDEX idx_session_id (session_id),
    INDEX idx_type (type),
    INDEX idx_level (level),
    INDEX idx_source (source),
    INDEX idx_created_at (created_at),
    FULLTEXT(message)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`

const createTranscriptionsTable = `
CREATE TABLE IF NOT EXISTS transcriptions (
    id VARCHAR(36) PRIMARY KEY,
    session_id VARCHAR(36) NOT NULL,
    stream_id VARCHAR(255) NULL,
    provider VARCHAR(50) NOT NULL,
    language VARCHAR(10) NOT NULL DEFAULT 'en-US',
    text TEXT NOT NULL,
    confidence DECIMAL(3,2) NULL,
    start_time TIMESTAMP NOT NULL,
    end_time TIMESTAMP NULL,
    word_count INT NOT NULL DEFAULT 0,
    speaker VARCHAR(255) NULL,
    is_final BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE,
    INDEX idx_session_id (session_id),
    INDEX idx_stream_id (stream_id),
    INDEX idx_provider (provider),
    INDEX idx_language (language),
    INDEX idx_start_time (start_time),
    INDEX idx_is_final (is_final),
    FULLTEXT(text)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`

const createUsersTable = `
CREATE TABLE IF NOT EXISTS users (
    id VARCHAR(36) PRIMARY KEY,
    username VARCHAR(100) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    role ENUM('admin', 'operator', 'viewer') NOT NULL DEFAULT 'viewer',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    last_login TIMESTAMP NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_username (username),
    INDEX idx_email (email),
    INDEX idx_role (role),
    INDEX idx_is_active (is_active)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`

const createAPIKeysTable = `
CREATE TABLE IF NOT EXISTS api_keys (
    id VARCHAR(36) PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL,
    name VARCHAR(255) NOT NULL,
    key_hash VARCHAR(255) NOT NULL,
    permissions JSON NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    expires_at TIMESTAMP NULL,
    last_used TIMESTAMP NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    INDEX idx_user_id (user_id),
    INDEX idx_key_hash (key_hash),
    INDEX idx_is_active (is_active),
    INDEX idx_expires_at (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`

const createSearchIndexTable = `
CREATE TABLE IF NOT EXISTS search_index (
    id VARCHAR(36) PRIMARY KEY,
    type VARCHAR(50) NOT NULL,
    entity_id VARCHAR(36) NOT NULL,
    content TEXT NOT NULL,
    metadata JSON NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_type (type),
    INDEX idx_entity_id (entity_id),
    FULLTEXT(content)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`

const createIndexes = `
-- Additional performance indexes
CREATE INDEX IF NOT EXISTS idx_sessions_composite ON sessions(status, start_time, transport);
CREATE INDEX IF NOT EXISTS idx_cdr_composite ON cdr(status, start_time, participant_count);
CREATE INDEX IF NOT EXISTS idx_events_composite ON events(level, created_at, type);
CREATE INDEX IF NOT EXISTS idx_transcriptions_composite ON transcriptions(session_id, is_final, start_time);
`
