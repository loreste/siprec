package encryption

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// RotationService handles automatic key rotation
type RotationService struct {
	encryptionManager EncryptionManager
	config            *EncryptionConfig
	logger            *logrus.Logger

	ticker  *time.Ticker
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool
	mu      sync.Mutex
}

// NewRotationService creates a new key rotation service
func NewRotationService(encMgr EncryptionManager, config *EncryptionConfig, logger *logrus.Logger) *RotationService {
	return &RotationService{
		encryptionManager: encMgr,
		config:            config,
		logger:            logger,
	}
}

// Start starts the key rotation service
func (rs *RotationService) Start() error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if rs.running {
		return nil
	}

	rs.ctx, rs.cancel = context.WithCancel(context.Background())
	rs.ticker = time.NewTicker(rs.config.KeyRotationInterval)
	rs.running = true

	rs.wg.Add(1)
	go rs.rotationLoop()

	rs.logger.WithField("interval", rs.config.KeyRotationInterval).Info("Started key rotation service")
	return nil
}

// Stop stops the key rotation service
func (rs *RotationService) Stop() error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if !rs.running {
		return nil
	}

	rs.cancel()
	rs.ticker.Stop()
	rs.running = false

	rs.wg.Wait()

	rs.logger.Info("Stopped key rotation service")
	return nil
}

// rotationLoop runs the periodic key rotation
func (rs *RotationService) rotationLoop() {
	defer rs.wg.Done()

	for {
		select {
		case <-rs.ctx.Done():
			return
		case <-rs.ticker.C:
			if err := rs.performRotation(); err != nil {
				rs.logger.WithError(err).Error("Failed to perform scheduled key rotation")
			}
		}
	}
}

// performRotation performs the actual key rotation
func (rs *RotationService) performRotation() error {
	start := time.Now()

	rs.logger.Info("Starting key rotation")

	// Rotate keys
	if err := rs.encryptionManager.RotateKeys(); err != nil {
		return err
	}

	// Backup keys if enabled
	if rs.config.KeyBackupEnabled {
		if err := rs.encryptionManager.BackupKeys(); err != nil {
			rs.logger.WithError(err).Error("Failed to backup keys after rotation")
		}
	}

	duration := time.Since(start)
	rs.logger.WithField("duration", duration).Info("Completed key rotation")

	return nil
}
