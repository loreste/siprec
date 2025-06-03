package backup

import (
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// DatabaseBackupManager manages automated database backups
type DatabaseBackupManager struct {
	config    BackupConfig
	logger    *logrus.Logger
	scheduler *BackupScheduler
	storage   BackupStorage
	mutex     sync.RWMutex
}

// BackupConfig holds backup configuration
type BackupConfig struct {
	Enabled        bool
	BackupPath     string
	Schedule       BackupSchedule
	Retention      RetentionPolicy
	Compression    bool
	Encryption     EncryptionConfig
	Storage        StorageConfig
	Notifications  NotificationConfig
	DatabaseConfig DatabaseConfig
}

// BackupSchedule defines when backups should run
type BackupSchedule struct {
	FullBackup        string // cron expression
	IncrementalBackup string // cron expression
	TransactionLog    string // cron expression
}

// RetentionPolicy defines how long to keep backups
type RetentionPolicy struct {
	DailyBackups   int // days
	WeeklyBackups  int // weeks
	MonthlyBackups int // months
	YearlyBackups  int // years
}

// EncryptionConfig holds encryption settings
type EncryptionConfig struct {
	Enabled    bool
	Algorithm  string // AES-256-GCM
	KeyFile    string
	Passphrase string
}

// StorageConfig defines backup storage options
type StorageConfig struct {
	Local bool
	S3    S3Config
	GCS   GCSConfig
	Azure AzureConfig
}

// S3Config for AWS S3 storage
type S3Config struct {
	Enabled   bool
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
	Prefix    string
}

// GCSConfig for Google Cloud Storage
type GCSConfig struct {
	Enabled           bool
	Bucket            string
	ServiceAccountKey string
	Prefix            string
}

// AzureConfig for Azure Blob Storage
type AzureConfig struct {
	Enabled   bool
	Account   string
	Container string
	AccessKey string
	Prefix    string
}

// NotificationConfig for backup notifications
type NotificationConfig struct {
	Enabled   bool
	OnSuccess bool
	OnFailure bool
	Channels  []string
}

// DatabaseConfig holds database connection info
type DatabaseConfig struct {
	Type     string // mysql, postgresql
	Host     string
	Port     int
	Database string
	Username string
	Password string
}

// BackupResult represents the result of a backup operation
type BackupResult struct {
	ID               string        `json:"id"`
	Type             string        `json:"type"` // full, incremental, transaction_log
	StartTime        time.Time     `json:"start_time"`
	EndTime          time.Time     `json:"end_time"`
	Duration         time.Duration `json:"duration"`
	Size             int64         `json:"size"`
	Path             string        `json:"path"`
	Status           string        `json:"status"` // success, failed, in_progress
	Error            string        `json:"error,omitempty"`
	Checksum         string        `json:"checksum"`
	Compressed       bool          `json:"compressed"`
	Encrypted        bool          `json:"encrypted"`
	StorageLocations []string      `json:"storage_locations"`
}

// NewDatabaseBackupManager creates a new backup manager
func NewDatabaseBackupManager(config BackupConfig, logger *logrus.Logger) (*DatabaseBackupManager, error) {
	if !config.Enabled {
		logger.Info("Database backup is disabled")
		return &DatabaseBackupManager{
			config: config,
			logger: logger,
		}, nil
	}

	// Validate configuration
	if err := validateBackupConfig(config); err != nil {
		return nil, fmt.Errorf("invalid backup configuration: %w", err)
	}

	// Create backup directory
	if err := os.MkdirAll(config.BackupPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Initialize storage
	storage, err := NewBackupStorage(config.Storage, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize backup storage: %w", err)
	}

	manager := &DatabaseBackupManager{
		config:  config,
		logger:  logger,
		storage: storage,
	}

	// Initialize scheduler
	manager.scheduler = NewBackupScheduler(config.Schedule, manager, logger)

	logger.WithFields(logrus.Fields{
		"backup_path": config.BackupPath,
		"retention":   config.Retention,
		"compression": config.Compression,
		"encryption":  config.Encryption.Enabled,
	}).Info("Database backup manager initialized")

	return manager, nil
}

// Start starts the backup manager
func (bm *DatabaseBackupManager) Start() error {
	if !bm.config.Enabled {
		return nil
	}

	if err := bm.scheduler.Start(); err != nil {
		return fmt.Errorf("failed to start backup scheduler: %w", err)
	}

	bm.logger.Info("Database backup manager started")
	return nil
}

// Stop stops the backup manager
func (bm *DatabaseBackupManager) Stop() error {
	if bm.scheduler != nil {
		bm.scheduler.Stop()
	}
	bm.logger.Info("Database backup manager stopped")
	return nil
}

// CreateFullBackup creates a full database backup
func (bm *DatabaseBackupManager) CreateFullBackup() (*BackupResult, error) {
	return bm.createBackup("full")
}

// CreateIncrementalBackup creates an incremental backup
func (bm *DatabaseBackupManager) CreateIncrementalBackup() (*BackupResult, error) {
	return bm.createBackup("incremental")
}

// CreateTransactionLogBackup creates a transaction log backup
func (bm *DatabaseBackupManager) CreateTransactionLogBackup() (*BackupResult, error) {
	return bm.createBackup("transaction_log")
}

// createBackup creates a backup of the specified type
func (bm *DatabaseBackupManager) createBackup(backupType string) (*BackupResult, error) {
	backupID := generateBackupID(backupType)
	startTime := time.Now()

	bm.logger.WithFields(logrus.Fields{
		"backup_id":   backupID,
		"backup_type": backupType,
	}).Info("Starting database backup")

	result := &BackupResult{
		ID:        backupID,
		Type:      backupType,
		StartTime: startTime,
		Status:    "in_progress",
	}

	// Create backup file path
	fileName := fmt.Sprintf("%s_%s_%s.sql",
		bm.config.DatabaseConfig.Database,
		backupType,
		startTime.Format("20060102_150405"))

	if bm.config.Compression {
		fileName += ".gz"
	}

	backupPath := filepath.Join(bm.config.BackupPath, fileName)
	result.Path = backupPath

	// Perform the backup
	size, checksum, err := bm.performDatabaseBackup(backupType, backupPath)
	if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)

		bm.logger.WithError(err).WithField("backup_id", backupID).Error("Database backup failed")
		bm.sendNotification(result)
		return result, err
	}

	result.Size = size
	result.Checksum = checksum
	result.Compressed = bm.config.Compression
	result.Encrypted = bm.config.Encryption.Enabled

	// Encrypt if enabled
	if bm.config.Encryption.Enabled {
		encryptedPath, err := bm.encryptBackup(backupPath)
		if err != nil {
			result.Status = "failed"
			result.Error = fmt.Sprintf("encryption failed: %v", err)
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime)
			return result, err
		}

		// Remove unencrypted file
		os.Remove(backupPath)
		result.Path = encryptedPath
	}

	// Upload to remote storage
	if err := bm.uploadBackup(result); err != nil {
		bm.logger.WithError(err).WithField("backup_id", backupID).Warning("Failed to upload backup to remote storage")
	}

	result.Status = "success"
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	bm.logger.WithFields(logrus.Fields{
		"backup_id": backupID,
		"duration":  result.Duration,
		"size":      formatBytes(result.Size),
		"path":      result.Path,
	}).Info("Database backup completed successfully")

	// Send success notification
	bm.sendNotification(result)

	// Clean up old backups
	go bm.cleanupOldBackups()

	return result, nil
}

// performDatabaseBackup performs the actual database backup
func (bm *DatabaseBackupManager) performDatabaseBackup(backupType, outputPath string) (int64, string, error) {
	switch bm.config.DatabaseConfig.Type {
	case "mysql":
		return bm.performMySQLBackup(backupType, outputPath)
	case "postgresql":
		return bm.performPostgreSQLBackup(backupType, outputPath)
	default:
		return 0, "", fmt.Errorf("unsupported database type: %s", bm.config.DatabaseConfig.Type)
	}
}

// performMySQLBackup performs MySQL backup using mysqldump
func (bm *DatabaseBackupManager) performMySQLBackup(backupType, outputPath string) (int64, string, error) {
	args := []string{
		fmt.Sprintf("--host=%s", bm.config.DatabaseConfig.Host),
		fmt.Sprintf("--port=%d", bm.config.DatabaseConfig.Port),
		fmt.Sprintf("--user=%s", bm.config.DatabaseConfig.Username),
		fmt.Sprintf("--password=%s", bm.config.DatabaseConfig.Password),
		"--single-transaction",
		"--routines",
		"--triggers",
		"--events",
		"--extended-insert",
		"--create-options",
		"--disable-keys",
		"--lock-tables=false",
	}

	// Add backup type specific options
	switch backupType {
	case "full":
		args = append(args, "--all-databases")
	case "incremental":
		args = append(args, "--flush-logs", "--master-data=2")
		args = append(args, bm.config.DatabaseConfig.Database)
	case "transaction_log":
		args = append(args, "--flush-logs", "--single-transaction")
		args = append(args, bm.config.DatabaseConfig.Database)
	default:
		args = append(args, bm.config.DatabaseConfig.Database)
	}

	return bm.executeBackupCommand("mysqldump", args, outputPath)
}

// performPostgreSQLBackup performs PostgreSQL backup using pg_dump
func (bm *DatabaseBackupManager) performPostgreSQLBackup(backupType, outputPath string) (int64, string, error) {
	// Set environment variables for PostgreSQL
	env := os.Environ()
	env = append(env, fmt.Sprintf("PGHOST=%s", bm.config.DatabaseConfig.Host))
	env = append(env, fmt.Sprintf("PGPORT=%d", bm.config.DatabaseConfig.Port))
	env = append(env, fmt.Sprintf("PGUSER=%s", bm.config.DatabaseConfig.Username))
	env = append(env, fmt.Sprintf("PGPASSWORD=%s", bm.config.DatabaseConfig.Password))

	args := []string{
		"--verbose",
		"--clean",
		"--create",
		"--if-exists",
		"--format=custom",
		"--compress=9",
	}

	// Add backup type specific options
	switch backupType {
	case "full":
		args = append(args, "--all")
	default:
		args = append(args, bm.config.DatabaseConfig.Database)
	}

	return bm.executeBackupCommandWithEnv("pg_dump", args, outputPath, env)
}

// executeBackupCommand executes a backup command and handles compression
func (bm *DatabaseBackupManager) executeBackupCommand(command string, args []string, outputPath string) (int64, string, error) {
	return bm.executeBackupCommandWithEnv(command, args, outputPath, nil)
}

// executeBackupCommandWithEnv executes a backup command with environment variables
func (bm *DatabaseBackupManager) executeBackupCommandWithEnv(command string, args []string, outputPath string, env []string) (int64, string, error) {
	cmd := exec.Command(command, args...)
	if env != nil {
		cmd.Env = env
	}

	// Create output file
	var outputFile *os.File
	var writer io.WriteCloser
	var err error

	if bm.config.Compression {
		outputFile, err = os.Create(outputPath)
		if err != nil {
			return 0, "", fmt.Errorf("failed to create output file: %w", err)
		}
		defer outputFile.Close()

		gzipWriter := gzip.NewWriter(outputFile)
		defer gzipWriter.Close()
		writer = gzipWriter
	} else {
		outputFile, err = os.Create(outputPath)
		if err != nil {
			return 0, "", fmt.Errorf("failed to create output file: %w", err)
		}
		defer outputFile.Close()
		writer = outputFile
	}

	cmd.Stdout = writer

	// Execute command
	if err := cmd.Run(); err != nil {
		return 0, "", fmt.Errorf("backup command failed: %w", err)
	}

	// Close writer to flush any buffered data
	if bm.config.Compression {
		if gw, ok := writer.(*gzip.Writer); ok {
			gw.Close()
		}
	}

	// Get file size and calculate checksum
	fileInfo, err := os.Stat(outputPath)
	if err != nil {
		return 0, "", fmt.Errorf("failed to stat backup file: %w", err)
	}

	checksum, err := calculateFileChecksum(outputPath)
	if err != nil {
		return 0, "", fmt.Errorf("failed to calculate checksum: %w", err)
	}

	return fileInfo.Size(), checksum, nil
}

// encryptBackup encrypts a backup file using AES-256-GCM
func (bm *DatabaseBackupManager) encryptBackup(filePath string) (string, error) {
	encryptedPath := filePath + ".enc"

	// Read the source file
	plaintext, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read backup file: %w", err)
	}

	// Generate or load encryption key
	key, err := bm.getEncryptionKey()
	if err != nil {
		return "", fmt.Errorf("failed to get encryption key: %w", err)
	}

	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt the data
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)

	// Write encrypted file
	if err := os.WriteFile(encryptedPath, ciphertext, 0600); err != nil {
		return "", fmt.Errorf("failed to write encrypted file: %w", err)
	}

	// Remove original file
	if err := os.Remove(filePath); err != nil {
		bm.logger.WithError(err).Warning("Failed to remove original backup file")
	}

	return encryptedPath, nil
}

// uploadBackup uploads a backup to configured remote storage
func (bm *DatabaseBackupManager) uploadBackup(result *BackupResult) error {
	if bm.storage == nil {
		return nil
	}

	locations, err := bm.storage.Upload(result.Path, result.ID)
	if err != nil {
		return err
	}

	result.StorageLocations = locations
	return nil
}

// cleanupOldBackups removes old backups based on retention policy
func (bm *DatabaseBackupManager) cleanupOldBackups() {
	bm.mutex.Lock()
	defer bm.mutex.Unlock()

	bm.logger.Info("Starting backup cleanup")

	// Get all backup files
	backupFiles, err := bm.getBackupFiles()
	if err != nil {
		bm.logger.WithError(err).Error("Failed to get backup files for cleanup")
		return
	}

	// Group by backup type and sort by date
	groupedBackups := make(map[string][]*BackupFile)
	for _, backup := range backupFiles {
		backupType := backup.Type
		groupedBackups[backupType] = append(groupedBackups[backupType], backup)
	}

	// Apply retention policy for each type
	for backupType, backups := range groupedBackups {
		sort.Slice(backups, func(i, j int) bool {
			return backups[i].Date.After(backups[j].Date)
		})

		toDelete := bm.getBackupsToDelete(backups, backupType)
		for _, backup := range toDelete {
			if err := os.Remove(backup.Path); err != nil {
				bm.logger.WithError(err).WithField("path", backup.Path).Warning("Failed to delete old backup")
			} else {
				bm.logger.WithField("path", backup.Path).Info("Deleted old backup")
			}
		}
	}
}

// getBackupsToDelete determines which backups to delete based on retention policy
func (bm *DatabaseBackupManager) getBackupsToDelete(backups []*BackupFile, backupType string) []*BackupFile {
	var toDelete []*BackupFile
	policy := bm.config.Retention

	now := time.Now()

	// Keep daily backups for specified days
	dailyKeep := now.AddDate(0, 0, -policy.DailyBackups)

	// Keep weekly backups for specified weeks
	weeklyKeep := now.AddDate(0, 0, -policy.WeeklyBackups*7)

	// Keep monthly backups for specified months
	monthlyKeep := now.AddDate(0, -policy.MonthlyBackups, 0)

	// Keep yearly backups for specified years
	yearlyKeep := now.AddDate(-policy.YearlyBackups, 0, 0)

	for i, backup := range backups {
		age := now.Sub(backup.Date)

		// Skip if within daily retention
		if backup.Date.After(dailyKeep) {
			continue
		}

		// Keep if it's a weekly backup within weekly retention
		if backup.Date.After(weeklyKeep) && backup.Date.Weekday() == time.Sunday {
			continue
		}

		// Keep if it's a monthly backup within monthly retention
		if backup.Date.After(monthlyKeep) && backup.Date.Day() == 1 {
			continue
		}

		// Keep if it's a yearly backup within yearly retention
		if backup.Date.After(yearlyKeep) && backup.Date.YearDay() == 1 {
			continue
		}

		// Delete if older than yearly retention
		if age > time.Duration(policy.YearlyBackups)*365*24*time.Hour {
			toDelete = append(toDelete, backup)
		}

		// Keep at least the most recent backup of each type
		if i < len(backups)-1 {
			toDelete = append(toDelete, backup)
		}
	}

	return toDelete
}

// Helper functions and types

type BackupFile struct {
	Path string
	Type string
	Date time.Time
	Size int64
}

func (bm *DatabaseBackupManager) getBackupFiles() ([]*BackupFile, error) {
	var backupFiles []*BackupFile

	files, err := os.ReadDir(bm.config.BackupPath)
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		// Parse backup file name to extract metadata
		name := file.Name()
		if !strings.HasSuffix(name, ".sql") && !strings.HasSuffix(name, ".sql.gz") && !strings.HasSuffix(name, ".sql.gz.enc") {
			continue
		}

		// Extract backup type and date from filename
		// Expected format: database_type_YYYYMMDD_HHMMSS.sql[.gz][.enc]
		parts := strings.Split(name, "_")
		if len(parts) < 4 {
			continue
		}

		backupType := parts[1]
		dateStr := parts[2] + "_" + strings.Split(parts[3], ".")[0]

		date, err := time.Parse("20060102_150405", dateStr)
		if err != nil {
			bm.logger.WithError(err).WithField("filename", name).Warning("Failed to parse backup date")
			continue
		}

		info, err := file.Info()
		if err != nil {
			continue
		}

		backupFiles = append(backupFiles, &BackupFile{
			Path: filepath.Join(bm.config.BackupPath, name),
			Type: backupType,
			Date: date,
			Size: info.Size(),
		})
	}

	return backupFiles, nil
}

func (bm *DatabaseBackupManager) sendNotification(result *BackupResult) {
	if !bm.config.Notifications.Enabled {
		return
	}

	shouldNotify := (result.Status == "success" && bm.config.Notifications.OnSuccess) ||
		(result.Status == "failed" && bm.config.Notifications.OnFailure)

	if shouldNotify {
		// Send notification through configured channels
		for _, channel := range bm.config.Notifications.Channels {
			message := fmt.Sprintf("Backup %s: %s (Duration: %v, Size: %s)",
				result.Status, result.ID, result.Duration, formatBytes(result.Size))
			if result.Error != "" {
				message += fmt.Sprintf(" - Error: %s", result.Error)
			}

			if err := bm.sendChannelNotification(channel, message, result); err != nil {
				bm.logger.WithError(err).WithField("channel", channel).Warning("Failed to send backup notification")
			}
		}
	}
}

// Utility functions

func generateBackupID(backupType string) string {
	return fmt.Sprintf("%s_%d", backupType, time.Now().Unix())
}

func calculateFileChecksum(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("failed to calculate hash: %w", err)
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func validateBackupConfig(config BackupConfig) error {
	if config.BackupPath == "" {
		return fmt.Errorf("backup path is required")
	}

	if config.DatabaseConfig.Type == "" {
		return fmt.Errorf("database type is required")
	}

	if config.DatabaseConfig.Host == "" {
		return fmt.Errorf("database host is required")
	}

	if config.DatabaseConfig.Database == "" {
		return fmt.Errorf("database name is required")
	}

	return nil
}

// getEncryptionKey retrieves or generates the encryption key
func (bm *DatabaseBackupManager) getEncryptionKey() ([]byte, error) {
	if bm.config.Encryption.KeyFile != "" {
		// Read key from file
		key, err := os.ReadFile(bm.config.Encryption.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read key file: %w", err)
		}
		if len(key) != 32 {
			return nil, fmt.Errorf("encryption key must be 32 bytes for AES-256")
		}
		return key, nil
	}

	if bm.config.Encryption.Passphrase != "" {
		// Derive key from passphrase using SHA-256
		hash := sha256.Sum256([]byte(bm.config.Encryption.Passphrase))
		return hash[:], nil
	}

	return nil, fmt.Errorf("no encryption key or passphrase configured")
}

// sendChannelNotification sends notification to a specific channel
func (bm *DatabaseBackupManager) sendChannelNotification(channel, message string, result *BackupResult) error {
	// This would integrate with the alerting system
	bm.logger.WithFields(logrus.Fields{
		"channel":   channel,
		"message":   message,
		"backup_id": result.ID,
	}).Info("Backup notification sent")
	return nil
}
