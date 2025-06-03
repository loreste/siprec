package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// Repository provides database operations
type Repository struct {
	db     *MySQLDatabase
	logger *logrus.Logger
}

// NewRepository creates a new repository
func NewRepository(db *MySQLDatabase, logger *logrus.Logger) *Repository {
	return &Repository{
		db:     db,
		logger: logger,
	}
}

// Session operations

// CreateSession creates a new session record
func (r *Repository) CreateSession(session *Session) error {
	ctx, cancel := r.db.getContext()
	defer cancel()

	session.ID = uuid.New().String()
	session.CreatedAt = time.Now()
	session.UpdatedAt = time.Now()

	query := `
		INSERT INTO sessions (
			id, call_id, session_id, status, transport, source_ip, source_port,
			local_ip, local_port, start_time, recording_path, metadata_xml, sdp,
			participants, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.db.ExecContext(ctx, query,
		session.ID, session.CallID, session.SessionID, session.Status,
		session.Transport, session.SourceIP, session.SourcePort,
		session.LocalIP, session.LocalPort, session.StartTime,
		session.RecordingPath, session.MetadataXML, session.SDP,
		session.Participants, session.CreatedAt, session.UpdatedAt,
	)

	if err != nil {
		r.logger.WithError(err).Error("Failed to create session")
		return fmt.Errorf("failed to create session: %w", err)
	}

	r.logger.WithFields(logrus.Fields{
		"session_id": session.ID,
		"call_id":    session.CallID,
		"transport":  session.Transport,
	}).Info("Session created successfully")

	return nil
}

// UpdateSession updates an existing session
func (r *Repository) UpdateSession(session *Session) error {
	ctx, cancel := r.db.getContext()
	defer cancel()

	session.UpdatedAt = time.Now()

	query := `
		UPDATE sessions SET
			status = ?, end_time = ?, duration = ?, metadata_xml = ?,
			participants = ?, updated_at = ?
		WHERE id = ?
	`

	_, err := r.db.db.ExecContext(ctx, query,
		session.Status, session.EndTime, session.Duration,
		session.MetadataXML, session.Participants, session.UpdatedAt,
		session.ID,
	)

	if err != nil {
		r.logger.WithError(err).WithField("session_id", session.ID).Error("Failed to update session")
		return fmt.Errorf("failed to update session: %w", err)
	}

	return nil
}

// GetSession retrieves a session by ID
func (r *Repository) GetSession(id string) (*Session, error) {
	ctx, cancel := r.db.getContext()
	defer cancel()

	query := `
		SELECT id, call_id, session_id, status, transport, source_ip, source_port,
			   local_ip, local_port, start_time, end_time, duration, recording_path,
			   metadata_xml, sdp, participants, created_at, updated_at
		FROM sessions WHERE id = ?
	`

	session := &Session{}
	err := r.db.db.QueryRowContext(ctx, query, id).Scan(
		&session.ID, &session.CallID, &session.SessionID, &session.Status,
		&session.Transport, &session.SourceIP, &session.SourcePort,
		&session.LocalIP, &session.LocalPort, &session.StartTime,
		&session.EndTime, &session.Duration, &session.RecordingPath,
		&session.MetadataXML, &session.SDP, &session.Participants,
		&session.CreatedAt, &session.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("session not found: %s", id)
		}
		r.logger.WithError(err).WithField("session_id", id).Error("Failed to get session")
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	return session, nil
}

// SearchSessions searches sessions with filters
func (r *Repository) SearchSessions(filters SessionFilters) ([]*Session, int, error) {
	ctx, cancel := r.db.getContext()
	defer cancel()

	query, args := r.buildSessionQuery(filters)

	// Get total count
	countQuery := strings.Replace(query, "SELECT id, call_id, session_id, status, transport, source_ip, source_port, local_ip, local_port, start_time, end_time, duration, recording_path, metadata_xml, sdp, participants, created_at, updated_at", "SELECT COUNT(*)", 1)

	var total int
	err := r.db.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count sessions: %w", err)
	}

	// Add pagination
	if filters.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filters.Limit)
	}
	if filters.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filters.Offset)
	}

	rows, err := r.db.db.QueryContext(ctx, query, args...)
	if err != nil {
		r.logger.WithError(err).Error("Failed to search sessions")
		return nil, 0, fmt.Errorf("failed to search sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		session := &Session{}
		err := rows.Scan(
			&session.ID, &session.CallID, &session.SessionID, &session.Status,
			&session.Transport, &session.SourceIP, &session.SourcePort,
			&session.LocalIP, &session.LocalPort, &session.StartTime,
			&session.EndTime, &session.Duration, &session.RecordingPath,
			&session.MetadataXML, &session.SDP, &session.Participants,
			&session.CreatedAt, &session.UpdatedAt,
		)
		if err != nil {
			r.logger.WithError(err).Error("Failed to scan session row")
			continue
		}
		sessions = append(sessions, session)
	}

	return sessions, total, nil
}

// CDR operations

// CreateCDR creates a new CDR record
func (r *Repository) CreateCDR(cdr *CDR) error {
	ctx, cancel := r.db.getContext()
	defer cancel()

	cdr.ID = uuid.New().String()
	cdr.CreatedAt = time.Now()
	cdr.UpdatedAt = time.Now()

	query := `
		INSERT INTO cdr (
			id, session_id, call_id, caller_id, callee_id, start_time, end_time,
			duration, recording_path, recording_size, transcription_id, quality,
			transport, source_ip, codec, sample_rate, participant_count,
			stream_count, status, error_message, billing_code, cost_center,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.db.ExecContext(ctx, query,
		cdr.ID, cdr.SessionID, cdr.CallID, cdr.CallerID, cdr.CalleeID,
		cdr.StartTime, cdr.EndTime, cdr.Duration, cdr.RecordingPath,
		cdr.RecordingSize, cdr.TranscriptionID, cdr.Quality, cdr.Transport,
		cdr.SourceIP, cdr.Codec, cdr.SampleRate, cdr.ParticipantCount,
		cdr.StreamCount, cdr.Status, cdr.ErrorMessage, cdr.BillingCode,
		cdr.CostCenter, cdr.CreatedAt, cdr.UpdatedAt,
	)

	if err != nil {
		r.logger.WithError(err).Error("Failed to create CDR")
		return fmt.Errorf("failed to create CDR: %w", err)
	}

	r.logger.WithFields(logrus.Fields{
		"cdr_id":     cdr.ID,
		"session_id": cdr.SessionID,
		"call_id":    cdr.CallID,
		"duration":   cdr.Duration,
	}).Info("CDR created successfully")

	return nil
}

// Event operations

// CreateEvent creates a new event record
func (r *Repository) CreateEvent(event *Event) error {
	ctx, cancel := r.db.getContext()
	defer cancel()

	event.ID = uuid.New().String()
	event.CreatedAt = time.Now()

	var metadataJSON []byte
	var err error
	if event.Metadata != nil {
		metadataJSON, err = json.Marshal(event.Metadata)
		if err != nil {
			r.logger.WithError(err).Error("Failed to marshal event metadata")
			return fmt.Errorf("failed to marshal event metadata: %w", err)
		}
	}

	query := `
		INSERT INTO events (
			id, session_id, type, level, message, source, source_ip,
			user_agent, metadata, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = r.db.db.ExecContext(ctx, query,
		event.ID, event.SessionID, event.Type, event.Level,
		event.Message, event.Source, event.SourceIP, event.UserAgent,
		metadataJSON, event.CreatedAt,
	)

	if err != nil {
		r.logger.WithError(err).Error("Failed to create event")
		return fmt.Errorf("failed to create event: %w", err)
	}

	return nil
}

// Search operations

// FullTextSearch performs full-text search across sessions, CDRs, and transcriptions
func (r *Repository) FullTextSearch(query string, entityTypes []string, limit, offset int) ([]*SearchResult, int, error) {
	ctx, cancel := r.db.getContext()
	defer cancel()

	// Build search query based on entity types
	var unionQueries []string
	var args []interface{}

	if len(entityTypes) == 0 || contains(entityTypes, "session") {
		unionQueries = append(unionQueries, `
			SELECT 'session' as type, id as entity_id, call_id as title, 
				   CONCAT(call_id, ' ', COALESCE(metadata_xml, '')) as content,
				   start_time as created_at,
				   MATCH(metadata_xml) AGAINST(? IN NATURAL LANGUAGE MODE) as relevance
			FROM sessions 
			WHERE MATCH(metadata_xml) AGAINST(? IN NATURAL LANGUAGE MODE)
		`)
		args = append(args, query, query)
	}

	if len(entityTypes) == 0 || contains(entityTypes, "cdr") {
		unionQueries = append(unionQueries, `
			SELECT 'cdr' as type, id as entity_id, call_id as title,
				   CONCAT(call_id, ' ', caller_id, ' ', callee_id, ' ', COALESCE(error_message, '')) as content,
				   start_time as created_at,
				   MATCH(error_message) AGAINST(? IN NATURAL LANGUAGE MODE) as relevance
			FROM cdr 
			WHERE MATCH(error_message) AGAINST(? IN NATURAL LANGUAGE MODE)
		`)
		args = append(args, query, query)
	}

	if len(entityTypes) == 0 || contains(entityTypes, "transcription") {
		unionQueries = append(unionQueries, `
			SELECT 'transcription' as type, id as entity_id, 
				   CONCAT('Transcription ', provider) as title,
				   text as content, start_time as created_at,
				   MATCH(text) AGAINST(? IN NATURAL LANGUAGE MODE) as relevance
			FROM transcriptions 
			WHERE MATCH(text) AGAINST(? IN NATURAL LANGUAGE MODE)
		`)
		args = append(args, query, query)
	}

	if len(unionQueries) == 0 {
		return []*SearchResult{}, 0, nil
	}

	// Combine queries
	fullQuery := strings.Join(unionQueries, " UNION ALL ")
	fullQuery += " ORDER BY relevance DESC"

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM (%s) as search_results", fullQuery)
	var total int
	err := r.db.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count search results: %w", err)
	}

	// Add pagination
	if limit > 0 {
		fullQuery += " LIMIT ?"
		args = append(args, limit)
	}
	if offset > 0 {
		fullQuery += " OFFSET ?"
		args = append(args, offset)
	}

	rows, err := r.db.db.QueryContext(ctx, fullQuery, args...)
	if err != nil {
		r.logger.WithError(err).Error("Failed to execute search query")
		return nil, 0, fmt.Errorf("failed to execute search: %w", err)
	}
	defer rows.Close()

	var results []*SearchResult
	for rows.Next() {
		result := &SearchResult{}
		err := rows.Scan(
			&result.Type, &result.EntityID, &result.Title,
			&result.Content, &result.CreatedAt, &result.Relevance,
		)
		if err != nil {
			r.logger.WithError(err).Error("Failed to scan search result")
			continue
		}
		results = append(results, result)
	}

	return results, total, nil
}

// Helper functions

func (r *Repository) buildSessionQuery(filters SessionFilters) (string, []interface{}) {
	query := `
		SELECT id, call_id, session_id, status, transport, source_ip, source_port,
			   local_ip, local_port, start_time, end_time, duration, recording_path,
			   metadata_xml, sdp, participants, created_at, updated_at
		FROM sessions
		WHERE 1=1
	`
	var args []interface{}

	if filters.CallID != "" {
		query += " AND call_id = ?"
		args = append(args, filters.CallID)
	}

	if filters.Status != "" {
		query += " AND status = ?"
		args = append(args, filters.Status)
	}

	if filters.Transport != "" {
		query += " AND transport = ?"
		args = append(args, filters.Transport)
	}

	if filters.SourceIP != "" {
		query += " AND source_ip = ?"
		args = append(args, filters.SourceIP)
	}

	if !filters.StartTime.IsZero() {
		query += " AND start_time >= ?"
		args = append(args, filters.StartTime)
	}

	if !filters.EndTime.IsZero() {
		query += " AND start_time <= ?"
		args = append(args, filters.EndTime)
	}

	if filters.MinDuration > 0 {
		query += " AND duration >= ?"
		args = append(args, filters.MinDuration)
	}

	if filters.MaxDuration > 0 {
		query += " AND duration <= ?"
		args = append(args, filters.MaxDuration)
	}

	// Add ordering
	query += " ORDER BY start_time DESC"

	return query, args
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// Filter and result types

type SessionFilters struct {
	CallID      string
	Status      string
	Transport   string
	SourceIP    string
	StartTime   time.Time
	EndTime     time.Time
	MinDuration int64
	MaxDuration int64
	Limit       int
	Offset      int
}

type SearchResult struct {
	Type      string    `json:"type"`
	EntityID  string    `json:"entity_id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	Relevance float64   `json:"relevance"`
}
