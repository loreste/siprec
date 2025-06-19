package cdr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"siprec-server/pkg/database"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// CDRService handles Call Data Record generation and management
type CDRService struct {
	repo         *database.Repository
	logger       *logrus.Logger
	activeCDRs   map[string]*database.CDR
	mutex        sync.RWMutex
	exportPath   string
	exportFormat string
	autoExport   bool
	batchSize    int
}

// CDRConfig holds CDR service configuration
type CDRConfig struct {
	ExportPath     string
	ExportFormat   string // json, csv, xml
	AutoExport     bool
	BatchSize      int
	ExportInterval time.Duration
}

// NewCDRService creates a new CDR service
func NewCDRService(repo *database.Repository, config CDRConfig, logger *logrus.Logger) *CDRService {
	service := &CDRService{
		repo:         repo,
		logger:       logger,
		activeCDRs:   make(map[string]*database.CDR),
		exportPath:   config.ExportPath,
		exportFormat: config.ExportFormat,
		autoExport:   config.AutoExport,
		batchSize:    config.BatchSize,
	}

	// Start auto-export if enabled
	if config.AutoExport && config.ExportInterval > 0 {
		go service.startAutoExport(config.ExportInterval)
	}

	logger.WithFields(logrus.Fields{
		"export_path":   config.ExportPath,
		"export_format": config.ExportFormat,
		"auto_export":   config.AutoExport,
		"batch_size":    config.BatchSize,
	}).Info("CDR service initialized")

	return service
}

// StartSession initiates a new CDR for a session
func (c *CDRService) StartSession(sessionID, callID, sourceIP, transport string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	cdr := &database.CDR{
		ID:               uuid.New().String(),
		SessionID:        sessionID,
		CallID:           callID,
		StartTime:        time.Now(),
		Transport:        transport,
		SourceIP:         sourceIP,
		ParticipantCount: 0,
		StreamCount:      0,
		Status:           "active",
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	c.activeCDRs[sessionID] = cdr

	c.logger.WithFields(logrus.Fields{
		"session_id": sessionID,
		"call_id":    callID,
		"cdr_id":     cdr.ID,
	}).Info("CDR session started")

	return nil
}

// UpdateSession updates an active CDR with session information
func (c *CDRService) UpdateSession(sessionID string, updates CDRUpdate) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	cdr, exists := c.activeCDRs[sessionID]
	if !exists {
		return fmt.Errorf("CDR not found for session: %s", sessionID)
	}

	// Apply updates
	if updates.CallerID != nil {
		cdr.CallerID = updates.CallerID
	}
	if updates.CalleeID != nil {
		cdr.CalleeID = updates.CalleeID
	}
	if updates.RecordingPath != nil {
		cdr.RecordingPath = *updates.RecordingPath
	}
	if updates.Codec != nil {
		cdr.Codec = updates.Codec
	}
	if updates.SampleRate != nil {
		cdr.SampleRate = updates.SampleRate
	}
	if updates.ParticipantCount != nil {
		cdr.ParticipantCount = *updates.ParticipantCount
	}
	if updates.StreamCount != nil {
		cdr.StreamCount = *updates.StreamCount
	}
	if updates.Quality != nil {
		cdr.Quality = updates.Quality
	}
	if updates.TranscriptionID != nil {
		cdr.TranscriptionID = updates.TranscriptionID
	}
	if updates.BillingCode != nil {
		cdr.BillingCode = updates.BillingCode
	}
	if updates.CostCenter != nil {
		cdr.CostCenter = updates.CostCenter
	}

	cdr.UpdatedAt = time.Now()

	c.logger.WithFields(logrus.Fields{
		"session_id": sessionID,
		"cdr_id":     cdr.ID,
	}).Debug("CDR updated")

	return nil
}

// EndSession finalizes a CDR and stores it in the database
func (c *CDRService) EndSession(sessionID string, status string, errorMessage *string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	cdr, exists := c.activeCDRs[sessionID]
	if !exists {
		return fmt.Errorf("CDR not found for session: %s", sessionID)
	}

	// Finalize CDR
	now := time.Now()
	cdr.EndTime = &now
	duration := int64(now.Sub(cdr.StartTime).Seconds())
	cdr.Duration = &duration
	cdr.Status = status
	cdr.ErrorMessage = errorMessage
	cdr.UpdatedAt = now

	// Calculate recording size if file exists
	if cdr.RecordingPath != "" {
		if info, err := os.Stat(cdr.RecordingPath); err == nil {
			size := info.Size()
			cdr.RecordingSize = &size
		}
	}

	// Store in database
	if err := c.repo.CreateCDR(cdr); err != nil {
		c.logger.WithError(err).WithField("session_id", sessionID).Error("Failed to store CDR")
		return fmt.Errorf("failed to store CDR: %w", err)
	}

	// Remove from active CDRs
	delete(c.activeCDRs, sessionID)

	c.logger.WithFields(logrus.Fields{
		"session_id":     sessionID,
		"cdr_id":         cdr.ID,
		"duration":       duration,
		"status":         status,
		"recording_size": cdr.RecordingSize,
	}).Info("CDR session ended and stored")

	return nil
}

// GetActiveCDRs returns all currently active CDRs
func (c *CDRService) GetActiveCDRs() map[string]*database.CDR {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	// Create a copy to avoid race conditions
	active := make(map[string]*database.CDR)
	for k, v := range c.activeCDRs {
		active[k] = v
	}

	return active
}

// ExportCDRs exports CDR records to specified format
func (c *CDRService) ExportCDRs(filters CDRFilters, format string) (string, error) {
	cdrs, err := c.getCDRsWithFilters(filters)
	if err != nil {
		return "", fmt.Errorf("failed to get CDRs: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("cdr_export_%s.%s", timestamp, format)
	filepath := filepath.Join(c.exportPath, filename)

	// Ensure export directory exists
	if err := os.MkdirAll(c.exportPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create export directory: %w", err)
	}

	switch format {
	case "json":
		err = c.exportJSON(cdrs, filepath)
	case "csv":
		err = c.exportCSV(cdrs, filepath)
	case "xml":
		err = c.exportXML(cdrs, filepath)
	default:
		return "", fmt.Errorf("unsupported export format: %s", format)
	}

	if err != nil {
		return "", fmt.Errorf("failed to export CDRs: %w", err)
	}

	c.logger.WithFields(logrus.Fields{
		"format":    format,
		"file":      filepath,
		"cdr_count": len(cdrs),
	}).Info("CDRs exported successfully")

	return filepath, nil
}

// Statistics and reporting

// GetCDRStats returns CDR statistics for a time period
func (c *CDRService) GetCDRStats(startTime, endTime time.Time) (*CDRStats, error) {
	filters := CDRFilters{
		StartTime: &startTime,
		EndTime:   &endTime,
	}

	cdrs, err := c.getCDRsWithFilters(filters)
	if err != nil {
		return nil, fmt.Errorf("failed to get CDRs for stats: %w", err)
	}

	stats := &CDRStats{
		TotalCalls:         len(cdrs),
		CompletedCalls:     0,
		FailedCalls:        0,
		TotalDuration:      0,
		AverageDuration:    0,
		TotalRecordingSize: 0,
		TransportStats:     make(map[string]int),
		CodecStats:         make(map[string]int),
		QualityStats:       &QualityStats{},
	}

	var totalQuality float64
	var qualityCount int
	var durations []float64

	for _, cdr := range cdrs {
		switch cdr.Status {
		case "completed":
			stats.CompletedCalls++
		case "failed":
			stats.FailedCalls++
		}

		if cdr.Duration != nil {
			duration := float64(*cdr.Duration)
			stats.TotalDuration += duration
			durations = append(durations, duration)
		}

		if cdr.RecordingSize != nil {
			stats.TotalRecordingSize += *cdr.RecordingSize
		}

		// Transport statistics
		stats.TransportStats[cdr.Transport]++

		// Codec statistics
		if cdr.Codec != nil {
			stats.CodecStats[*cdr.Codec]++
		}

		// Quality statistics
		if cdr.Quality != nil {
			totalQuality += *cdr.Quality
			qualityCount++
		}
	}

	// Calculate averages
	if len(durations) > 0 {
		stats.AverageDuration = stats.TotalDuration / float64(len(durations))
	}

	if qualityCount > 0 {
		stats.QualityStats.Average = totalQuality / float64(qualityCount)
		stats.QualityStats.Count = qualityCount
	}

	return stats, nil
}

// Private methods

func (c *CDRService) startAutoExport(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		c.performAutoExport()
	}
}

func (c *CDRService) performAutoExport() {
	// Export CDRs from the last interval
	endTime := time.Now()
	startTime := endTime.Add(-time.Hour) // Last hour by default

	filters := CDRFilters{
		StartTime: &startTime,
		EndTime:   &endTime,
		Status:    "completed",
	}

	_, err := c.ExportCDRs(filters, c.exportFormat)
	if err != nil {
		c.logger.WithError(err).Error("Auto-export failed")
	}
}

func (c *CDRService) getCDRsWithFilters(filters CDRFilters) ([]*database.CDR, error) {
	// This would use the database repository to fetch CDRs with filters
	// For now, return empty slice - this needs to be implemented in the repository
	return []*database.CDR{}, nil
}

func (c *CDRService) exportJSON(cdrs []*database.CDR, filepath string) error {
	file, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(cdrs)
}

func (c *CDRService) exportCSV(cdrs []*database.CDR, filepath string) error {
	file, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write CSV header
	header := "id,session_id,call_id,caller_id,callee_id,start_time,end_time,duration,transport,source_ip,codec,participant_count,status\n"
	if _, err := file.WriteString(header); err != nil {
		return err
	}

	// Write CDR records
	for _, cdr := range cdrs {
		line := fmt.Sprintf("%s,%s,%s,%s,%s,%s,%s,%.2f,%s,%s,%s,%d,%s\n",
			cdr.ID, cdr.SessionID, cdr.CallID,
			stringOrEmpty(cdr.CallerID), stringOrEmpty(cdr.CalleeID),
			cdr.StartTime.Format(time.RFC3339),
			timeOrEmpty(cdr.EndTime),
			floatOrZero(cdr.Duration),
			cdr.Transport, cdr.SourceIP,
			stringOrEmpty(cdr.Codec),
			cdr.ParticipantCount, cdr.Status,
		)
		if _, err := file.WriteString(line); err != nil {
			return err
		}
	}

	return nil
}

func (c *CDRService) exportXML(cdrs []*database.CDR, filepath string) error {
	file, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write XML header
	if _, err := file.WriteString(`<?xml version="1.0" encoding="UTF-8"?>\n<cdrs>\n`); err != nil {
		return err
	}

	// Write CDR records
	for _, cdr := range cdrs {
		xml := fmt.Sprintf(`  <cdr>
    <id>%s</id>
    <session_id>%s</session_id>
    <call_id>%s</call_id>
    <caller_id>%s</caller_id>
    <callee_id>%s</callee_id>
    <start_time>%s</start_time>
    <end_time>%s</end_time>
    <duration>%.2f</duration>
    <transport>%s</transport>
    <source_ip>%s</source_ip>
    <codec>%s</codec>
    <participant_count>%d</participant_count>
    <status>%s</status>
  </cdr>
`,
			cdr.ID, cdr.SessionID, cdr.CallID,
			stringOrEmpty(cdr.CallerID), stringOrEmpty(cdr.CalleeID),
			cdr.StartTime.Format(time.RFC3339),
			timeOrEmpty(cdr.EndTime),
			floatOrZero(cdr.Duration),
			cdr.Transport, cdr.SourceIP,
			stringOrEmpty(cdr.Codec),
			cdr.ParticipantCount, cdr.Status,
		)
		if _, err := file.WriteString(xml); err != nil {
			return err
		}
	}

	// Write XML footer
	if _, err := file.WriteString("</cdrs>\n"); err != nil {
		return err
	}

	return nil
}

// Helper functions

func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func timeOrEmpty(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

func floatOrZero(f *int64) float64 {
	if f == nil {
		return 0.0
	}
	return float64(*f)
}

// Types

type CDRUpdate struct {
	CallerID         *string
	CalleeID         *string
	RecordingPath    *string
	Codec            *string
	SampleRate       *int
	ParticipantCount *int
	StreamCount      *int
	Quality          *float64
	TranscriptionID  *string
	BillingCode      *string
	CostCenter       *string
}

type CDRFilters struct {
	StartTime   *time.Time
	EndTime     *time.Time
	Status      string
	Transport   string
	CallerID    string
	CalleeID    string
	BillingCode string
	CostCenter  string
	MinDuration *float64
	MaxDuration *float64
}

type CDRStats struct {
	TotalCalls         int            `json:"total_calls"`
	CompletedCalls     int            `json:"completed_calls"`
	FailedCalls        int            `json:"failed_calls"`
	TotalDuration      float64        `json:"total_duration"`
	AverageDuration    float64        `json:"average_duration"`
	TotalRecordingSize int64          `json:"total_recording_size"`
	TransportStats     map[string]int `json:"transport_stats"`
	CodecStats         map[string]int `json:"codec_stats"`
	QualityStats       *QualityStats  `json:"quality_stats"`
}

type QualityStats struct {
	Average float64 `json:"average"`
	Count   int     `json:"count"`
}
