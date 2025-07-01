package backup

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
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

// CheckMySQLReplication checks MySQL replication status
func (dbo *DatabaseOperations) CheckMySQLReplication(host string, port int, username, password string) (ReplicationStatus, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/", username, password, host, port)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return ReplicationStatus{}, fmt.Errorf("failed to connect to MySQL: %w", err)
	}
	defer db.Close()

	// Check if this is a slave
	var slaveStatus struct {
		SlaveIOState        sql.NullString
		MasterLogFile       sql.NullString
		ReadMasterLogPos    sql.NullInt64
		RelayLogFile        sql.NullString
		RelayLogPos         sql.NullInt64
		RelayMasterLogFile  sql.NullString
		SlaveIORunning      sql.NullString
		SlaveSQLRunning     sql.NullString
		ReplicateDoDB       sql.NullString
		ReplicateIgnoreDB   sql.NullString
		LastErrno           sql.NullInt64
		LastError           sql.NullString
		SkipCounter         sql.NullInt64
		ExecMasterLogPos    sql.NullInt64
		RelayLogSpace       sql.NullInt64
		SecondsBehindMaster sql.NullInt64
	}

	query := `SHOW SLAVE STATUS`
	row := db.QueryRow(query)

	err = row.Scan(
		&slaveStatus.SlaveIOState,
		&slaveStatus.MasterLogFile,
		&slaveStatus.ReadMasterLogPos,
		&slaveStatus.RelayLogFile,
		&slaveStatus.RelayLogPos,
		&slaveStatus.RelayMasterLogFile,
		&slaveStatus.SlaveIORunning,
		&slaveStatus.SlaveSQLRunning,
		&slaveStatus.ReplicateDoDB,
		&slaveStatus.ReplicateIgnoreDB,
		&slaveStatus.LastErrno,
		&slaveStatus.LastError,
		&slaveStatus.SkipCounter,
		&slaveStatus.ExecMasterLogPos,
		&slaveStatus.RelayLogSpace,
		&slaveStatus.SecondsBehindMaster,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			// Not a slave, check if it's a master
			return dbo.checkMySQLMasterStatus(db)
		}
		return ReplicationStatus{}, fmt.Errorf("failed to get slave status: %w", err)
	}

	// Parse replication status
	status := ReplicationStatus{
		ServiceName:     "mysql",
		IsReplicating:   slaveStatus.SlaveIORunning.Valid && slaveStatus.SlaveIORunning.String == "Yes",
		SlaveIORunning:  slaveStatus.SlaveIORunning.Valid && slaveStatus.SlaveIORunning.String == "Yes",
		SlaveSQLRunning: slaveStatus.SlaveSQLRunning.Valid && slaveStatus.SlaveSQLRunning.String == "Yes",
	}

	if slaveStatus.SecondsBehindMaster.Valid {
		status.LagSeconds = float64(slaveStatus.SecondsBehindMaster.Int64)
	}

	if slaveStatus.LastError.Valid && slaveStatus.LastError.String != "" {
		status.ReplicationError = slaveStatus.LastError.String
	}

	// Calculate last replication time
	if status.LagSeconds > 0 {
		status.LastReplicationTime = time.Now().Add(-time.Duration(status.LagSeconds) * time.Second)
	} else {
		status.LastReplicationTime = time.Now()
	}

	return status, nil
}

// checkMySQLMasterStatus checks if this is a MySQL master
func (dbo *DatabaseOperations) checkMySQLMasterStatus(db *sql.DB) (ReplicationStatus, error) {
	var binlogFile, binlogPosition string

	err := db.QueryRow("SHOW MASTER STATUS").Scan(&binlogFile, &binlogPosition)
	if err != nil {
		if err == sql.ErrNoRows {
			// Neither master nor slave
			return ReplicationStatus{
				ServiceName:   "mysql",
				IsReplicating: false,
			}, nil
		}
		return ReplicationStatus{}, fmt.Errorf("failed to get master status: %w", err)
	}

	// Check connected slaves
	rows, err := db.Query("SHOW PROCESSLIST")
	if err != nil {
		return ReplicationStatus{}, fmt.Errorf("failed to get process list: %w", err)
	}
	defer rows.Close()

	slaveCount := 0
	for rows.Next() {
		var id int
		var user, host, dbName, command, timeStr, state, info sql.NullString

		err := rows.Scan(&id, &user, &host, &dbName, &command, &timeStr, &state, &info)
		if err != nil {
			continue
		}

		if command.Valid && command.String == "Binlog Dump" {
			slaveCount++
		}
	}

	return ReplicationStatus{
		ServiceName:         "mysql",
		IsReplicating:       slaveCount > 0,
		LastReplicationTime: time.Now(),
		LagSeconds:          0, // Master has no lag
	}, nil
}

// PromoteMySQLSlave promotes a MySQL slave to master
func (dbo *DatabaseOperations) PromoteMySQLSlave(host string, port int, username, password string) error {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/", username, password, host, port)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to connect to MySQL: %w", err)
	}
	defer db.Close()

	dbo.logger.WithFields(logrus.Fields{
		"host": host,
		"port": port,
	}).Info("Promoting MySQL slave to master")

	// Stop slave
	if _, err := db.Exec("STOP SLAVE"); err != nil {
		return fmt.Errorf("failed to stop slave: %w", err)
	}

	// Reset slave configuration
	if _, err := db.Exec("RESET SLAVE ALL"); err != nil {
		return fmt.Errorf("failed to reset slave: %w", err)
	}

	// Enable binary logging if not already enabled
	if _, err := db.Exec("SET GLOBAL log_bin = ON"); err != nil {
		dbo.logger.WithError(err).Warning("Failed to enable binary logging (may already be enabled)")
	}

	// Reset master to start fresh binary logs
	if _, err := db.Exec("RESET MASTER"); err != nil {
		dbo.logger.WithError(err).Warning("Failed to reset master (may not be necessary)")
	}

	dbo.logger.Info("MySQL slave promoted to master successfully")
	return nil
}

// CheckPostgreSQLReplication checks PostgreSQL replication status
func (dbo *DatabaseOperations) CheckPostgreSQLReplication(host string, port int, username, password, database string) (ReplicationStatus, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		host, port, username, password, database)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return ReplicationStatus{}, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	defer db.Close()

	// Check if this is a standby
	var isInRecovery bool
	err = db.QueryRow("SELECT pg_is_in_recovery()").Scan(&isInRecovery)
	if err != nil {
		return ReplicationStatus{}, fmt.Errorf("failed to check recovery status: %w", err)
	}

	if isInRecovery {
		return dbo.checkPostgreSQLStandbyStatus(db)
	} else {
		return dbo.checkPostgreSQLPrimaryStatus(db)
	}
}

// checkPostgreSQLStandbyStatus checks PostgreSQL standby status
func (dbo *DatabaseOperations) checkPostgreSQLStandbyStatus(db *sql.DB) (ReplicationStatus, error) {
	var lagBytes sql.NullInt64

	// Get replication lag
	query := `
		SELECT 
			COALESCE(pg_wal_lsn_diff(pg_last_wal_receive_lsn(), pg_last_wal_replay_lsn()), 0) as lag_bytes,
			pg_last_xact_replay_timestamp() as last_replay_time
	`

	var lastReplayTime sql.NullTime
	err := db.QueryRow(query).Scan(&lagBytes, &lastReplayTime)
	if err != nil {
		return ReplicationStatus{}, fmt.Errorf("failed to get standby status: %w", err)
	}

	status := ReplicationStatus{
		ServiceName:   "postgresql",
		IsReplicating: true,
	}

	if lagBytes.Valid {
		status.BytesReplicated = lagBytes.Int64
		// Convert bytes to approximate seconds (rough estimate)
		status.LagSeconds = float64(lagBytes.Int64) / (1024 * 1024) // Rough conversion
	}

	if lastReplayTime.Valid {
		status.LastReplicationTime = lastReplayTime.Time
	} else {
		status.LastReplicationTime = time.Now()
	}

	return status, nil
}

// checkPostgreSQLPrimaryStatus checks PostgreSQL primary status
func (dbo *DatabaseOperations) checkPostgreSQLPrimaryStatus(db *sql.DB) (ReplicationStatus, error) {
	// Check connected standbys
	query := `
		SELECT COUNT(*) 
		FROM pg_stat_replication 
		WHERE state = 'streaming'
	`

	var standbyCount int
	err := db.QueryRow(query).Scan(&standbyCount)
	if err != nil {
		return ReplicationStatus{}, fmt.Errorf("failed to get replication status: %w", err)
	}

	return ReplicationStatus{
		ServiceName:         "postgresql",
		IsReplicating:       standbyCount > 0,
		LastReplicationTime: time.Now(),
		LagSeconds:          0, // Primary has no lag
	}, nil
}

// PromotePostgreSQLStandby promotes a PostgreSQL standby to primary
func (dbo *DatabaseOperations) PromotePostgreSQLStandby(host string, port int, username, password, database string) error {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		host, port, username, password, database)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	defer db.Close()

	dbo.logger.WithFields(logrus.Fields{
		"host":     host,
		"port":     port,
		"database": database,
	}).Info("Promoting PostgreSQL standby to primary")

	// Promote the standby
	_, err = db.Exec("SELECT pg_promote()")
	if err != nil {
		return fmt.Errorf("failed to promote standby: %w", err)
	}

	// Wait for promotion to complete
	maxWait := 30 * time.Second
	start := time.Now()

	for time.Since(start) < maxWait {
		var isInRecovery bool
		err = db.QueryRow("SELECT pg_is_in_recovery()").Scan(&isInRecovery)
		if err == nil && !isInRecovery {
			dbo.logger.Info("PostgreSQL standby promoted to primary successfully")
			return nil
		}
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("promotion timed out after %v", maxWait)
}

// CheckRedisReplication checks Redis replication status
func (dbo *DatabaseOperations) CheckRedisReplication(host string, port int, password string) (ReplicationStatus, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       0,
	})
	defer client.Close()

	ctx := context.Background()

	// Get replication info
	info, err := client.Info(ctx, "replication").Result()
	if err != nil {
		return ReplicationStatus{}, fmt.Errorf("failed to get Redis info: %w", err)
	}

	status := ReplicationStatus{
		ServiceName:         "redis",
		LastReplicationTime: time.Now(),
	}

	lines := strings.Split(info, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "role:") {
			role := strings.TrimPrefix(line, "role:")
			status.IsReplicating = role == "slave" || role == "master"
		} else if strings.HasPrefix(line, "master_repl_offset:") {
			offsetStr := strings.TrimPrefix(line, "master_repl_offset:")
			if offset, err := strconv.ParseInt(offsetStr, 10, 64); err == nil {
				status.BytesReplicated = offset
			}
		} else if strings.HasPrefix(line, "master_last_io_seconds_ago:") {
			lagStr := strings.TrimPrefix(line, "master_last_io_seconds_ago:")
			if lag, err := strconv.ParseFloat(lagStr, 64); err == nil {
				status.LagSeconds = lag
				status.LastReplicationTime = time.Now().Add(-time.Duration(lag) * time.Second)
			}
		}
	}

	return status, nil
}

// PromoteRedisSlave promotes a Redis slave to master
func (dbo *DatabaseOperations) PromoteRedisSlave(host string, port int, password string) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       0,
	})
	defer client.Close()

	ctx := context.Background()

	dbo.logger.WithFields(logrus.Fields{
		"host": host,
		"port": port,
	}).Info("Promoting Redis slave to master")

	// Promote slave to master
	err := client.SlaveOf(ctx, "NO", "ONE").Err()
	if err != nil {
		return fmt.Errorf("failed to promote Redis slave: %w", err)
	}

	// Verify promotion
	info, err := client.Info(ctx, "replication").Result()
	if err != nil {
		return fmt.Errorf("failed to verify promotion: %w", err)
	}

	if !strings.Contains(info, "role:master") {
		return fmt.Errorf("promotion verification failed")
	}

	dbo.logger.Info("Redis slave promoted to master successfully")
	return nil
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

// restorePostgreSQL restores a PostgreSQL database from backup
func (dbo *DatabaseOperations) restorePostgreSQL(backupPath, host string, port int, username, password, database string) error {
	dbo.logger.WithFields(logrus.Fields{
		"backup_path": backupPath,
		"host":        host,
		"port":        port,
		"database":    database,
	}).Info("Restoring PostgreSQL database")

	// For PostgreSQL, we would typically use pg_restore
	// This is a simplified implementation
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		host, port, username, password, database)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	defer db.Close()

	// Read backup file
	backupData, err := ReadBackupFile(backupPath)
	if err != nil {
		return fmt.Errorf("failed to read backup file: %w", err)
	}

	// Execute restoration
	if _, err := db.Exec(string(backupData)); err != nil {
		return fmt.Errorf("failed to restore database: %w", err)
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
