package cdr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"siprec-server/pkg/database"
	"siprec-server/pkg/pii"

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
	piiDetector  *pii.PIIDetector
	piiRedactCDR bool
}

// CDRConfig holds CDR service configuration
type CDRConfig struct {
	ExportPath     string
	ExportFormat   string // json, csv, xml
	AutoExport     bool
	BatchSize      int
	ExportInterval time.Duration
	PIIDetector    *pii.PIIDetector // Optional PII detector for redacting CDR fields
	PIIRedactCDR   bool             // Enable PII redaction for CallerID/CalleeID
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
		piiDetector:  config.PIIDetector,
		piiRedactCDR: config.PIIRedactCDR,
	}

	// Start auto-export if enabled
	if config.AutoExport && config.ExportInterval > 0 {
		go service.startAutoExport(config.ExportInterval)
	}

	logger.WithFields(logrus.Fields{
		"export_path":    config.ExportPath,
		"export_format":  config.ExportFormat,
		"auto_export":    config.AutoExport,
		"batch_size":     config.BatchSize,
		"pii_redact_cdr": config.PIIRedactCDR,
	}).Info("CDR service initialized")

	return service
}

// redactPII applies PII redaction to a string if PII redaction is enabled
func (c *CDRService) redactPII(text string) string {
	if !c.piiRedactCDR || c.piiDetector == nil || text == "" {
		return text
	}

	result := c.piiDetector.DetectAndRedact(text)
	if result.HasPII {
		c.logger.WithFields(logrus.Fields{
			"original_length": len(text),
			"redacted_length": len(result.RedactedText),
			"pii_matches":     len(result.Matches),
		}).Debug("PII redacted from CDR field")
	}
	return result.RedactedText
}

// SetPIIDetector sets the PII detector for CDR field redaction
func (c *CDRService) SetPIIDetector(detector *pii.PIIDetector, enabled bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.piiDetector = detector
	c.piiRedactCDR = enabled
	if enabled && detector != nil {
		c.logger.Info("PII redaction enabled for CDR fields")
	}
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

	// Apply updates with PII redaction for caller/callee IDs
	if updates.CallerID != nil {
		redacted := c.redactPII(*updates.CallerID)
		cdr.CallerID = &redacted
	}
	if updates.CalleeID != nil {
		redacted := c.redactPII(*updates.CalleeID)
		cdr.CalleeID = &redacted
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
	if updates.VendorType != nil {
		cdr.VendorType = updates.VendorType
	}
	if updates.UCID != nil {
		cdr.UCID = updates.UCID
	}
	if updates.OracleUCID != nil {
		cdr.OracleUCID = updates.OracleUCID
	}
	if updates.ConversationID != nil {
		cdr.ConversationID = updates.ConversationID
	}
	if updates.CiscoSessionID != nil {
		cdr.CiscoSessionID = updates.CiscoSessionID
	}
	// Genesys-specific fields
	if updates.GenesysInteractionID != nil {
		cdr.GenesysInteractionID = updates.GenesysInteractionID
	}
	if updates.GenesysConversationID != nil {
		cdr.GenesysConversationID = updates.GenesysConversationID
	}
	if updates.GenesysQueueName != nil {
		cdr.GenesysQueueName = updates.GenesysQueueName
	}
	if updates.GenesysAgentID != nil {
		cdr.GenesysAgentID = updates.GenesysAgentID
	}
	if updates.GenesysCampaignID != nil {
		cdr.GenesysCampaignID = updates.GenesysCampaignID
	}
	// Asterisk-specific fields
	if updates.AsteriskUniqueID != nil {
		cdr.AsteriskUniqueID = updates.AsteriskUniqueID
	}
	if updates.AsteriskLinkedID != nil {
		cdr.AsteriskLinkedID = updates.AsteriskLinkedID
	}
	if updates.AsteriskChannelID != nil {
		cdr.AsteriskChannelID = updates.AsteriskChannelID
	}
	if updates.AsteriskAccountCode != nil {
		cdr.AsteriskAccountCode = updates.AsteriskAccountCode
	}
	if updates.AsteriskContext != nil {
		cdr.AsteriskContext = updates.AsteriskContext
	}
	// FreeSWITCH-specific fields
	if updates.FreeSWITCHUUID != nil {
		cdr.FreeSWITCHUUID = updates.FreeSWITCHUUID
	}
	if updates.FreeSWITCHCoreUUID != nil {
		cdr.FreeSWITCHCoreUUID = updates.FreeSWITCHCoreUUID
	}
	if updates.FreeSWITCHChannelName != nil {
		cdr.FreeSWITCHChannelName = updates.FreeSWITCHChannelName
	}
	if updates.FreeSWITCHProfileName != nil {
		cdr.FreeSWITCHProfileName = updates.FreeSWITCHProfileName
	}
	if updates.FreeSWITCHAccountCode != nil {
		cdr.FreeSWITCHAccountCode = updates.FreeSWITCHAccountCode
	}
	// OpenSIPS-specific fields
	if updates.OpenSIPSDialogID != nil {
		cdr.OpenSIPSDialogID = updates.OpenSIPSDialogID
	}
	if updates.OpenSIPSTransactionID != nil {
		cdr.OpenSIPSTransactionID = updates.OpenSIPSTransactionID
	}
	if updates.OpenSIPSCallID != nil {
		cdr.OpenSIPSCallID = updates.OpenSIPSCallID
	}
	// NICE-specific fields
	if updates.NICEInteractionID != nil {
		cdr.NICEInteractionID = updates.NICEInteractionID
	}
	if updates.NICESessionID != nil {
		cdr.NICESessionID = updates.NICESessionID
	}
	if updates.NICERecordingID != nil {
		cdr.NICERecordingID = updates.NICERecordingID
	}
	if updates.NICEContactID != nil {
		cdr.NICEContactID = updates.NICEContactID
	}
	if updates.NICEAgentID != nil {
		cdr.NICEAgentID = updates.NICEAgentID
	}
	if updates.NICECallID != nil {
		cdr.NICECallID = updates.NICECallID
	}
	// Avaya-specific fields
	if updates.AvayaUCID != nil {
		cdr.AvayaUCID = updates.AvayaUCID
	}
	if updates.AvayaConfID != nil {
		cdr.AvayaConfID = updates.AvayaConfID
	}
	if updates.AvayaStationID != nil {
		cdr.AvayaStationID = updates.AvayaStationID
	}
	if updates.AvayaAgentID != nil {
		cdr.AvayaAgentID = updates.AvayaAgentID
	}
	if updates.AvayaVDN != nil {
		cdr.AvayaVDN = updates.AvayaVDN
	}
	if updates.AvayaSkillGroup != nil {
		cdr.AvayaSkillGroup = updates.AvayaSkillGroup
	}
	// AudioCodes-specific fields
	if updates.AudioCodesSessionID != nil {
		cdr.AudioCodesSessionID = updates.AudioCodesSessionID
	}
	if updates.AudioCodesCallID != nil {
		cdr.AudioCodesCallID = updates.AudioCodesCallID
	}
	// Ribbon-specific fields
	if updates.RibbonSessionID != nil {
		cdr.RibbonSessionID = updates.RibbonSessionID
	}
	if updates.RibbonCallID != nil {
		cdr.RibbonCallID = updates.RibbonCallID
	}
	if updates.RibbonGWID != nil {
		cdr.RibbonGWID = updates.RibbonGWID
	}
	// Sansay-specific fields
	if updates.SansaySessionID != nil {
		cdr.SansaySessionID = updates.SansaySessionID
	}
	if updates.SansayCallID != nil {
		cdr.SansayCallID = updates.SansayCallID
	}
	if updates.SansayTrunkID != nil {
		cdr.SansayTrunkID = updates.SansayTrunkID
	}
	// Huawei-specific fields
	if updates.HuaweiSessionID != nil {
		cdr.HuaweiSessionID = updates.HuaweiSessionID
	}
	if updates.HuaweiCallID != nil {
		cdr.HuaweiCallID = updates.HuaweiCallID
	}
	if updates.HuaweiTrunkID != nil {
		cdr.HuaweiTrunkID = updates.HuaweiTrunkID
	}
	// Microsoft-specific fields
	if updates.MSConversationID != nil {
		cdr.MSConversationID = updates.MSConversationID
	}
	if updates.MSCallID != nil {
		cdr.MSCallID = updates.MSCallID
	}
	if updates.MSCorrelationID != nil {
		cdr.MSCorrelationID = updates.MSCorrelationID
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

	// Store in database (if configured)
	if c.repo != nil {
		if err := c.repo.CreateCDR(cdr); err != nil {
			c.logger.WithError(err).WithField("session_id", sessionID).Error("Failed to store CDR")
			return fmt.Errorf("failed to store CDR: %w", err)
		}
	} else {
		c.logger.WithField("session_id", sessionID).Debug("Database not configured, CDR not persisted")
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
	VendorType       *string // avaya, cisco, oracle, genesys, audiocodes, ribbon, sansay, huawei, microsoft, asterisk, freeswitch, opensips, generic
	UCID             *string // Universal Call ID
	OracleUCID       *string // Oracle SBC specific UCID
	ConversationID   *string // Oracle Conversation ID
	CiscoSessionID   *string // Cisco Session-ID
	// Genesys-specific fields
	GenesysInteractionID  *string // Genesys Interaction ID
	GenesysConversationID *string // Genesys Conversation ID
	GenesysQueueName      *string // Genesys Queue Name
	GenesysAgentID        *string // Genesys Agent ID
	GenesysCampaignID     *string // Genesys Campaign ID
	// Asterisk-specific fields
	AsteriskUniqueID    *string // Asterisk Unique ID
	AsteriskLinkedID    *string // Asterisk Linked ID
	AsteriskChannelID   *string // Asterisk Channel Name
	AsteriskAccountCode *string // Asterisk Account Code
	AsteriskContext     *string // Asterisk Context
	// FreeSWITCH-specific fields
	FreeSWITCHUUID        *string // FreeSWITCH Call UUID
	FreeSWITCHCoreUUID    *string // FreeSWITCH Core UUID
	FreeSWITCHChannelName *string // FreeSWITCH Channel Name
	FreeSWITCHProfileName *string // FreeSWITCH Sofia Profile
	FreeSWITCHAccountCode *string // FreeSWITCH Account Code
	// OpenSIPS-specific fields
	OpenSIPSDialogID      *string // OpenSIPS Dialog ID
	OpenSIPSTransactionID *string // OpenSIPS Transaction ID
	OpenSIPSCallID        *string // OpenSIPS Call-ID
	// NICE-specific fields
	NICEInteractionID *string // NICE Interaction ID
	NICESessionID     *string // NICE Session ID
	NICERecordingID   *string // NICE Recording ID
	NICEContactID     *string // NICE Contact ID (CXone/inContact)
	NICEAgentID       *string // NICE Agent ID
	NICECallID        *string // NICE Call ID
	// Avaya-specific fields
	AvayaUCID       *string // Avaya Universal Call ID
	AvayaConfID     *string // Avaya Conference ID
	AvayaStationID  *string // Avaya Station ID
	AvayaAgentID    *string // Avaya Agent ID
	AvayaVDN        *string // Avaya Vector Directory Number
	AvayaSkillGroup *string // Avaya Skill Group
	// AudioCodes-specific fields
	AudioCodesSessionID *string // AudioCodes Session ID
	AudioCodesCallID    *string // AudioCodes Call ID
	// Ribbon-specific fields (formerly Sonus/GENBAND)
	RibbonSessionID *string // Ribbon Session ID
	RibbonCallID    *string // Ribbon Call ID
	RibbonGWID      *string // Ribbon Gateway ID
	// Sansay-specific fields
	SansaySessionID *string // Sansay Session ID
	SansayCallID    *string // Sansay Call ID
	SansayTrunkID   *string // Sansay Trunk ID
	// Huawei-specific fields
	HuaweiSessionID *string // Huawei Session ID
	HuaweiCallID    *string // Huawei Call ID
	HuaweiTrunkID   *string // Huawei Trunk ID
	// Microsoft Teams/Skype for Business/Lync-specific fields
	MSConversationID *string // Microsoft Conversation ID
	MSCallID         *string // Microsoft Call ID
	MSCorrelationID  *string // Microsoft Correlation ID
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
