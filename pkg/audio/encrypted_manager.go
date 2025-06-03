package audio

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"siprec-server/pkg/encryption"

	"github.com/sirupsen/logrus"
)

// EncryptedRecordingManager handles encrypted audio recording operations
type EncryptedRecordingManager struct {
	encryptionManager encryption.EncryptionManager
	logger            *logrus.Logger
	recordingDir      string
	mu                sync.RWMutex

	// Active recording sessions
	sessions map[string]*EncryptedRecordingSession
}

// EncryptedRecordingSession represents an active encrypted recording session
type EncryptedRecordingSession struct {
	SessionID    string                     `json:"session_id"`
	StartTime    time.Time                  `json:"start_time"`
	FilePath     string                     `json:"file_path"`
	MetadataPath string                     `json:"metadata_path"`
	Writer       io.WriteCloser             `json:"-"`
	Encryption   *encryption.EncryptionInfo `json:"encryption"`
	TotalBytes   int64                      `json:"total_bytes"`
	mu           sync.Mutex
}

// RecordingMetadata contains metadata for encrypted recordings
type RecordingMetadata struct {
	SessionID      string                     `json:"session_id"`
	StartTime      time.Time                  `json:"start_time"`
	EndTime        *time.Time                 `json:"end_time,omitempty"`
	Duration       time.Duration              `json:"duration"`
	Participants   []string                   `json:"participants"`
	Codec          string                     `json:"codec"`
	SampleRate     int                        `json:"sample_rate"`
	Channels       int                        `json:"channels"`
	TotalBytes     int64                      `json:"total_bytes"`
	EncryptionInfo *encryption.EncryptionInfo `json:"encryption_info"`
	FileFormat     string                     `json:"file_format"`
	Checksum       string                     `json:"checksum,omitempty"`
}

// NewEncryptedRecordingManager creates a new encrypted recording manager
func NewEncryptedRecordingManager(encMgr encryption.EncryptionManager, recordingDir string, logger *logrus.Logger) (*EncryptedRecordingManager, error) {
	if err := os.MkdirAll(recordingDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create recording directory: %w", err)
	}

	return &EncryptedRecordingManager{
		encryptionManager: encMgr,
		logger:            logger,
		recordingDir:      recordingDir,
		sessions:          make(map[string]*EncryptedRecordingSession),
	}, nil
}

// StartRecording starts an encrypted recording session
func (erm *EncryptedRecordingManager) StartRecording(sessionID string, metadata *RecordingMetadata) (*EncryptedRecordingSession, error) {
	erm.mu.Lock()
	defer erm.mu.Unlock()

	// Check if session already exists
	if _, exists := erm.sessions[sessionID]; exists {
		return nil, fmt.Errorf("recording session %s already exists", sessionID)
	}

	// Create file paths
	timestamp := time.Now().Format("20060102_150405")
	fileName := fmt.Sprintf("%s_%s.siprec", sessionID, timestamp)
	metadataFileName := fmt.Sprintf("%s_%s.metadata", sessionID, timestamp)

	filePath := filepath.Join(erm.recordingDir, fileName)
	metadataPath := filepath.Join(erm.recordingDir, metadataFileName)

	// Create recording file
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to create recording file: %w", err)
	}

	// Get encryption info
	encInfo, err := erm.encryptionManager.GetEncryptionInfo(sessionID)
	if err != nil {
		file.Close()
		os.Remove(filePath)
		return nil, fmt.Errorf("failed to get encryption info: %w", err)
	}

	// Create encrypted writer
	writer := &EncryptedWriter{
		file:              file,
		encryptionManager: erm.encryptionManager,
		sessionID:         sessionID,
		logger:            erm.logger,
	}

	// Write file header if encryption is enabled
	if erm.encryptionManager.IsEncryptionEnabled() {
		if err := writer.WriteHeader(); err != nil {
			file.Close()
			os.Remove(filePath)
			return nil, fmt.Errorf("failed to write encryption header: %w", err)
		}
	}

	session := &EncryptedRecordingSession{
		SessionID:    sessionID,
		StartTime:    time.Now(),
		FilePath:     filePath,
		MetadataPath: metadataPath,
		Writer:       writer,
		Encryption:   encInfo,
		TotalBytes:   0,
	}

	// Store metadata
	if metadata != nil {
		metadata.SessionID = sessionID
		metadata.StartTime = session.StartTime
		metadata.EncryptionInfo = encInfo

		if err := erm.storeMetadata(session, metadata); err != nil {
			session.Writer.Close()
			os.Remove(filePath)
			erm.logger.WithError(err).WithField("session_id", sessionID).Warn("Failed to store metadata")
		}
	}

	erm.sessions[sessionID] = session

	erm.logger.WithFields(logrus.Fields{
		"session_id": sessionID,
		"file_path":  filePath,
		"encrypted":  erm.encryptionManager.IsEncryptionEnabled(),
		"algorithm":  encInfo.Algorithm,
	}).Info("Started encrypted recording session")

	return session, nil
}

// WriteAudio writes audio data to the encrypted recording
func (erm *EncryptedRecordingManager) WriteAudio(sessionID string, audioData []byte) error {
	erm.mu.RLock()
	session, exists := erm.sessions[sessionID]
	erm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("recording session %s not found", sessionID)
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	n, err := session.Writer.Write(audioData)
	if err != nil {
		return fmt.Errorf("failed to write audio data: %w", err)
	}

	session.TotalBytes += int64(n)

	erm.logger.WithFields(logrus.Fields{
		"session_id": sessionID,
		"bytes":      n,
		"total":      session.TotalBytes,
	}).Debug("Wrote audio data to encrypted recording")

	return nil
}

// StopRecording stops an encrypted recording session
func (erm *EncryptedRecordingManager) StopRecording(sessionID string) error {
	erm.mu.Lock()
	defer erm.mu.Unlock()

	session, exists := erm.sessions[sessionID]
	if !exists {
		return fmt.Errorf("recording session %s not found", sessionID)
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	// Close the writer
	if err := session.Writer.Close(); err != nil {
		erm.logger.WithError(err).WithField("session_id", sessionID).Error("Failed to close recording file")
	}

	// Update metadata with end time
	endTime := time.Now()
	duration := endTime.Sub(session.StartTime)

	metadata := &RecordingMetadata{
		SessionID:      sessionID,
		StartTime:      session.StartTime,
		EndTime:        &endTime,
		Duration:       duration,
		TotalBytes:     session.TotalBytes,
		EncryptionInfo: session.Encryption,
		FileFormat:     "siprec",
	}

	if err := erm.storeMetadata(session, metadata); err != nil {
		erm.logger.WithError(err).WithField("session_id", sessionID).Warn("Failed to update final metadata")
	}

	delete(erm.sessions, sessionID)

	erm.logger.WithFields(logrus.Fields{
		"session_id": sessionID,
		"duration":   duration,
		"bytes":      session.TotalBytes,
		"file_path":  session.FilePath,
	}).Info("Stopped encrypted recording session")

	return nil
}

// GetRecordingInfo returns information about a recording session
func (erm *EncryptedRecordingManager) GetRecordingInfo(sessionID string) (*EncryptedRecordingSession, error) {
	erm.mu.RLock()
	defer erm.mu.RUnlock()

	session, exists := erm.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("recording session %s not found", sessionID)
	}

	return session, nil
}

// ListActiveSessions returns all active recording sessions
func (erm *EncryptedRecordingManager) ListActiveSessions() []*EncryptedRecordingSession {
	erm.mu.RLock()
	defer erm.mu.RUnlock()

	sessions := make([]*EncryptedRecordingSession, 0, len(erm.sessions))
	for _, session := range erm.sessions {
		sessions = append(sessions, session)
	}

	return sessions
}

// storeMetadata stores recording metadata (potentially encrypted)
func (erm *EncryptedRecordingManager) storeMetadata(session *EncryptedRecordingSession, metadata *RecordingMetadata) error {
	metadataBytes, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Encrypt metadata if enabled
	if erm.encryptionManager.IsEncryptionEnabled() {
		metadataMap := map[string]interface{}{
			"metadata": string(metadataBytes),
		}

		encryptedMetadata, err := erm.encryptionManager.EncryptMetadata(session.SessionID, metadataMap)
		if err != nil {
			return fmt.Errorf("failed to encrypt metadata: %w", err)
		}

		encryptedBytes, err := json.MarshalIndent(encryptedMetadata, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal encrypted metadata: %w", err)
		}

		metadataBytes = encryptedBytes
	}

	if err := os.WriteFile(session.MetadataPath, metadataBytes, 0600); err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	return nil
}

// EncryptedWriter wraps a file writer to provide encryption
type EncryptedWriter struct {
	file              *os.File
	encryptionManager encryption.EncryptionManager
	sessionID         string
	logger            *logrus.Logger
	headerWritten     bool
}

// WriteHeader writes the encryption header to the file
func (ew *EncryptedWriter) WriteHeader() error {
	if !ew.encryptionManager.IsEncryptionEnabled() {
		return nil
	}

	encInfo, err := ew.encryptionManager.GetEncryptionInfo(ew.sessionID)
	if err != nil {
		return fmt.Errorf("failed to get encryption info: %w", err)
	}

	header := &encryption.FileHeader{
		Magic:      [8]byte{'S', 'I', 'P', 'R', 'E', 'C', '0', '1'},
		Version:    1,
		Algorithm:  encInfo.Algorithm,
		KeyID:      encInfo.KeyID,
		KeyVersion: encInfo.KeyVersion,
		NonceSize:  12, // Default for GCM
		TagSize:    16, // Default for GCM
	}

	headerBytes, err := json.Marshal(header)
	if err != nil {
		return fmt.Errorf("failed to marshal header: %w", err)
	}

	// Write header length followed by header
	headerLen := uint32(len(headerBytes))
	if err := writeUint32(ew.file, headerLen); err != nil {
		return fmt.Errorf("failed to write header length: %w", err)
	}

	if _, err := ew.file.Write(headerBytes); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	ew.headerWritten = true
	return nil
}

// Write encrypts and writes data to the file
func (ew *EncryptedWriter) Write(data []byte) (int, error) {
	if !ew.headerWritten && ew.encryptionManager.IsEncryptionEnabled() {
		if err := ew.WriteHeader(); err != nil {
			return 0, fmt.Errorf("failed to write header: %w", err)
		}
	}

	if !ew.encryptionManager.IsEncryptionEnabled() {
		// Write unencrypted
		return ew.file.Write(data)
	}

	// Encrypt the data
	encryptedData, err := ew.encryptionManager.EncryptRecording(ew.sessionID, data)
	if err != nil {
		return 0, fmt.Errorf("failed to encrypt data: %w", err)
	}

	// Serialize encrypted data
	encBytes, err := json.Marshal(encryptedData)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal encrypted data: %w", err)
	}

	// Write chunk length followed by encrypted data
	chunkLen := uint32(len(encBytes))
	if err := writeUint32(ew.file, chunkLen); err != nil {
		return 0, fmt.Errorf("failed to write chunk length: %w", err)
	}

	if _, err := ew.file.Write(encBytes); err != nil {
		return 0, fmt.Errorf("failed to write encrypted data: %w", err)
	}

	return len(data), nil // Return original data length
}

// Close closes the writer
func (ew *EncryptedWriter) Close() error {
	return ew.file.Close()
}

// Helper function to write uint32 in little-endian
func writeUint32(w io.Writer, val uint32) error {
	bytes := []byte{
		byte(val),
		byte(val >> 8),
		byte(val >> 16),
		byte(val >> 24),
	}
	_, err := w.Write(bytes)
	return err
}

// DecryptRecording decrypts a recording file
func DecryptRecording(encMgr encryption.EncryptionManager, filePath string, outputPath string, logger *logrus.Logger) error {
	// Open encrypted file
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open encrypted file: %w", err)
	}
	defer file.Close()

	// Read header length
	var headerLen uint32
	if err := readUint32(file, &headerLen); err != nil {
		return fmt.Errorf("failed to read header length: %w", err)
	}

	// Read header
	headerBytes := make([]byte, headerLen)
	if _, err := io.ReadFull(file, headerBytes); err != nil {
		return fmt.Errorf("failed to read header: %w", err)
	}

	var header encryption.FileHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return fmt.Errorf("failed to unmarshal header: %w", err)
	}

	// Create output file
	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	// Process chunks
	for {
		var chunkLen uint32
		if err := readUint32(file, &chunkLen); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to read chunk length: %w", err)
		}

		// Read encrypted chunk
		encBytes := make([]byte, chunkLen)
		if _, err := io.ReadFull(file, encBytes); err != nil {
			return fmt.Errorf("failed to read encrypted chunk: %w", err)
		}

		// Unmarshal encrypted data
		var encData encryption.EncryptedData
		if err := json.Unmarshal(encBytes, &encData); err != nil {
			return fmt.Errorf("failed to unmarshal encrypted data: %w", err)
		}

		// Decrypt chunk
		plaintext, err := encMgr.DecryptRecording("", &encData)
		if err != nil {
			return fmt.Errorf("failed to decrypt chunk: %w", err)
		}

		// Write decrypted data
		if _, err := outFile.Write(plaintext); err != nil {
			return fmt.Errorf("failed to write decrypted data: %w", err)
		}
	}

	logger.WithFields(logrus.Fields{
		"input_file":  filePath,
		"output_file": outputPath,
		"algorithm":   header.Algorithm,
		"key_id":      header.KeyID,
	}).Info("Successfully decrypted recording")

	return nil
}

// Helper function to read uint32 in little-endian
func readUint32(r io.Reader, val *uint32) error {
	bytes := make([]byte, 4)
	if _, err := io.ReadFull(r, bytes); err != nil {
		return err
	}
	*val = uint32(bytes[0]) | uint32(bytes[1])<<8 | uint32(bytes[2])<<16 | uint32(bytes[3])<<24
	return nil
}
