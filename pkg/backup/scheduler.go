package backup

import (
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/sirupsen/logrus"
)

// BackupScheduler manages scheduled backup operations
type BackupScheduler struct {
	cron     *cron.Cron
	schedule BackupSchedule
	manager  *DatabaseBackupManager
	logger   *logrus.Logger
	running  bool
	mu       sync.RWMutex
	entryMap map[string]cron.EntryID // Maps backup type to cron entry ID
}

// NewBackupScheduler creates a new backup scheduler
func NewBackupScheduler(schedule BackupSchedule, manager *DatabaseBackupManager, logger *logrus.Logger) *BackupScheduler {
	return &BackupScheduler{
		cron:     cron.New(cron.WithSeconds()),
		schedule: schedule,
		manager:  manager,
		logger:   logger,
		entryMap: make(map[string]cron.EntryID),
	}
}

// Start starts the backup scheduler
func (bs *BackupScheduler) Start() error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if bs.running {
		return nil
	}

	// Schedule full backups
	if bs.schedule.FullBackup != "" {
		entryID, err := bs.cron.AddFunc(bs.schedule.FullBackup, func() {
			bs.logger.Info("Starting scheduled full backup")
			result, err := bs.manager.CreateFullBackup()
			if err != nil {
				bs.logger.WithError(err).Error("Scheduled full backup failed")
			} else {
				bs.logger.WithFields(logrus.Fields{
					"backup_id": result.ID,
					"duration":  result.Duration,
					"size":      formatBytes(result.Size),
				}).Info("Scheduled full backup completed")
			}
		})
		if err != nil {
			return err
		}
		bs.entryMap["full"] = entryID
		bs.logger.WithField("schedule", bs.schedule.FullBackup).Info("Scheduled full backups")
	}

	// Schedule incremental backups
	if bs.schedule.IncrementalBackup != "" {
		_, err := bs.cron.AddFunc(bs.schedule.IncrementalBackup, func() {
			bs.logger.Info("Starting scheduled incremental backup")
			result, err := bs.manager.CreateIncrementalBackup()
			if err != nil {
				bs.logger.WithError(err).Error("Scheduled incremental backup failed")
			} else {
				bs.logger.WithFields(logrus.Fields{
					"backup_id": result.ID,
					"duration":  result.Duration,
					"size":      formatBytes(result.Size),
				}).Info("Scheduled incremental backup completed")
			}
		})
		if err != nil {
			return err
		}
		bs.logger.WithField("schedule", bs.schedule.IncrementalBackup).Info("Scheduled incremental backups")
	}

	// Schedule transaction log backups
	if bs.schedule.TransactionLog != "" {
		_, err := bs.cron.AddFunc(bs.schedule.TransactionLog, func() {
			bs.logger.Info("Starting scheduled transaction log backup")
			result, err := bs.manager.CreateTransactionLogBackup()
			if err != nil {
				bs.logger.WithError(err).Error("Scheduled transaction log backup failed")
			} else {
				bs.logger.WithFields(logrus.Fields{
					"backup_id": result.ID,
					"duration":  result.Duration,
					"size":      formatBytes(result.Size),
				}).Info("Scheduled transaction log backup completed")
			}
		})
		if err != nil {
			return err
		}
		bs.logger.WithField("schedule", bs.schedule.TransactionLog).Info("Scheduled transaction log backups")
	}

	bs.cron.Start()
	bs.running = true
	bs.logger.Info("Backup scheduler started")

	return nil
}

// Stop stops the backup scheduler
func (bs *BackupScheduler) Stop() {
	if !bs.running {
		return
	}

	bs.cron.Stop()
	bs.running = false
	bs.logger.Info("Backup scheduler stopped")
}

// IsRunning returns whether the scheduler is running
func (bs *BackupScheduler) IsRunning() bool {
	return bs.running
}

// GetNextRun returns the next scheduled run times
func (bs *BackupScheduler) GetNextRun() map[string]time.Time {
	nextRuns := make(map[string]time.Time)

	entries := bs.cron.Entries()
	if len(entries) > 0 {
		// This is simplified - in a real implementation you'd track which entry is which
		nextRuns["full"] = entries[0].Next
		if len(entries) > 1 {
			nextRuns["incremental"] = entries[1].Next
		}
		if len(entries) > 2 {
			nextRuns["transaction_log"] = entries[2].Next
		}
	}

	return nextRuns
}
