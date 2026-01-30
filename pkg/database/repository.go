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

// Participant operations

// CreateParticipant stores a participant record for a session.
func (r *Repository) CreateParticipant(participant *Participant) error {
	ctx, cancel := r.db.getContext()
	defer cancel()

	participant.ID = uuid.New().String()
	now := time.Now()
	participant.CreatedAt = now
	participant.UpdatedAt = now

	query := `
		INSERT INTO participants (
			id, session_id, participant_id, type, name_id, display_name,
			aor, stream_id, join_time, leave_time, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.db.ExecContext(ctx, query,
		participant.ID, participant.SessionID, participant.ParticipantID,
		participant.Type, participant.NameID, participant.DisplayName,
		participant.AOR, participant.StreamID, participant.JoinTime,
		participant.LeaveTime, participant.CreatedAt, participant.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create participant: %w", err)
	}

	return nil
}

// UpdateParticipant updates a participant entry.
func (r *Repository) UpdateParticipant(participant *Participant) error {
	ctx, cancel := r.db.getContext()
	defer cancel()

	participant.UpdatedAt = time.Now()

	query := `
		UPDATE participants SET
			type = ?, name_id = ?, display_name = ?, aor = ?, stream_id = ?,
			join_time = ?, leave_time = ?, updated_at = ?
		WHERE id = ?
	`

	_, err := r.db.db.ExecContext(ctx, query,
		participant.Type, participant.NameID, participant.DisplayName,
		participant.AOR, participant.StreamID, participant.JoinTime,
		participant.LeaveTime, participant.UpdatedAt, participant.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update participant: %w", err)
	}

	return nil
}

// GetParticipantsBySession returns participants for a session.
func (r *Repository) GetParticipantsBySession(sessionID string) ([]*Participant, error) {
	ctx, cancel := r.db.getContext()
	defer cancel()

	query := `
		SELECT id, session_id, participant_id, type, name_id, display_name,
			   aor, stream_id, join_time, leave_time, created_at, updated_at
		FROM participants
		WHERE session_id = ?
		ORDER BY join_time ASC
	`

	rows, err := r.db.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query participants: %w", err)
	}
	defer rows.Close()

	var participants []*Participant
	for rows.Next() {
		p := &Participant{}
		var nameID, displayName, aor, streamID sql.NullString
		var leaveTime sql.NullTime

		if err := rows.Scan(
			&p.ID, &p.SessionID, &p.ParticipantID, &p.Type,
			&nameID, &displayName, &aor, &streamID,
			&p.JoinTime, &leaveTime, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan participant row: %w", err)
		}

		if nameID.Valid {
			val := nameID.String
			p.NameID = &val
		}
		if displayName.Valid {
			val := displayName.String
			p.DisplayName = &val
		}
		if aor.Valid {
			val := aor.String
			p.AOR = &val
		}
		if streamID.Valid {
			val := streamID.String
			p.StreamID = &val
		}
		if leaveTime.Valid {
			val := leaveTime.Time
			p.LeaveTime = &val
		}

		participants = append(participants, p)
	}

	return participants, nil
}

// Stream operations

// CreateStream stores a stream entry for a session.
func (r *Repository) CreateStream(stream *Stream) error {
	ctx, cancel := r.db.getContext()
	defer cancel()

	stream.ID = uuid.New().String()
	now := time.Now()
	stream.CreatedAt = now
	stream.UpdatedAt = now

	query := `
		INSERT INTO streams (
			id, session_id, stream_id, label, mode, direction, codec,
			sample_rate, channels, start_time, end_time, packet_count,
			byte_count, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.db.ExecContext(ctx, query,
		stream.ID, stream.SessionID, stream.StreamID, stream.Label,
		stream.Mode, stream.Direction, stream.Codec, stream.SampleRate,
		stream.Channels, stream.StartTime, stream.EndTime, stream.PacketCount,
		stream.ByteCount, stream.CreatedAt, stream.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create stream: %w", err)
	}

	return nil
}

// UpdateStream updates stream metadata.
func (r *Repository) UpdateStream(stream *Stream) error {
	ctx, cancel := r.db.getContext()
	defer cancel()

	stream.UpdatedAt = time.Now()

	query := `
		UPDATE streams SET
			label = ?, mode = ?, direction = ?, codec = ?, sample_rate = ?,
			channels = ?, start_time = ?, end_time = ?, packet_count = ?,
			byte_count = ?, updated_at = ?
		WHERE id = ?
	`

	_, err := r.db.db.ExecContext(ctx, query,
		stream.Label, stream.Mode, stream.Direction, stream.Codec,
		stream.SampleRate, stream.Channels, stream.StartTime, stream.EndTime,
		stream.PacketCount, stream.ByteCount, stream.UpdatedAt, stream.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update stream: %w", err)
	}

	return nil
}

// GetStreamsBySession returns streams for a session.
func (r *Repository) GetStreamsBySession(sessionID string) ([]*Stream, error) {
	ctx, cancel := r.db.getContext()
	defer cancel()

	query := `
		SELECT id, session_id, stream_id, label, mode, direction, codec,
			   sample_rate, channels, start_time, end_time, packet_count,
			   byte_count, created_at, updated_at
		FROM streams
		WHERE session_id = ?
		ORDER BY start_time ASC
	`

	rows, err := r.db.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query streams: %w", err)
	}
	defer rows.Close()

	var streams []*Stream
	for rows.Next() {
		s := &Stream{}
		var codec sql.NullString
		var sampleRate, channels sql.NullInt64
		var endTime sql.NullTime
		var packetCount, byteCount sql.NullInt64

		if err := rows.Scan(
			&s.ID, &s.SessionID, &s.StreamID, &s.Label, &s.Mode, &s.Direction,
			&codec, &sampleRate, &channels, &s.StartTime, &endTime,
			&packetCount, &byteCount, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan stream row: %w", err)
		}

		if codec.Valid {
			val := codec.String
			s.Codec = &val
		}
		if sampleRate.Valid {
			val := int(sampleRate.Int64)
			s.SampleRate = &val
		}
		if channels.Valid {
			val := int(channels.Int64)
			s.Channels = &val
		}
		if endTime.Valid {
			val := endTime.Time
			s.EndTime = &val
		}
		if packetCount.Valid {
			val := packetCount.Int64
			s.PacketCount = &val
		}
		if byteCount.Valid {
			val := byteCount.Int64
			s.ByteCount = &val
		}

		streams = append(streams, s)
	}

	return streams, nil
}

// Transcription operations

// CreateTranscription stores a transcription entry.
func (r *Repository) CreateTranscription(t *Transcription) error {
	ctx, cancel := r.db.getContext()
	defer cancel()

	t.ID = uuid.New().String()
	now := time.Now()
	t.CreatedAt = now
	t.UpdatedAt = now

	query := `
		INSERT INTO transcriptions (
			id, session_id, stream_id, provider, language, text, confidence,
			start_time, end_time, word_count, speaker, is_final, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.db.ExecContext(ctx, query,
		t.ID, t.SessionID, t.StreamID, t.Provider, t.Language, t.Text,
		t.Confidence, t.StartTime, t.EndTime, t.WordCount, t.Speaker,
		t.IsFinal, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create transcription: %w", err)
	}

	return nil
}

// UpdateTranscription updates transcription content and metadata.
func (r *Repository) UpdateTranscription(t *Transcription) error {
	ctx, cancel := r.db.getContext()
	defer cancel()

	t.UpdatedAt = time.Now()

	query := `
		UPDATE transcriptions SET
			text = ?, confidence = ?, end_time = ?, word_count = ?, speaker = ?,
			is_final = ?, updated_at = ?
		WHERE id = ?
	`

	_, err := r.db.db.ExecContext(ctx, query,
		t.Text, t.Confidence, t.EndTime, t.WordCount, t.Speaker,
		t.IsFinal, t.UpdatedAt, t.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update transcription: %w", err)
	}

	return nil
}

// GetTranscriptionsBySession returns transcriptions for a session.
func (r *Repository) GetTranscriptionsBySession(sessionID string) ([]*Transcription, error) {
	ctx, cancel := r.db.getContext()
	defer cancel()

	query := `
		SELECT id, session_id, stream_id, provider, language, text, confidence,
			   start_time, end_time, word_count, speaker, is_final, created_at, updated_at
		FROM transcriptions
		WHERE session_id = ?
		ORDER BY start_time ASC
	`

	rows, err := r.db.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query transcriptions: %w", err)
	}
	defer rows.Close()

	var transcriptions []*Transcription
	for rows.Next() {
		tr := &Transcription{}
		var streamID, speaker sql.NullString
		var confidence sql.NullFloat64
		var endTime sql.NullTime

		if err := rows.Scan(
			&tr.ID, &tr.SessionID, &streamID, &tr.Provider, &tr.Language,
			&tr.Text, &confidence, &tr.StartTime, &endTime, &tr.WordCount,
			&speaker, &tr.IsFinal, &tr.CreatedAt, &tr.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan transcription row: %w", err)
		}

		if streamID.Valid {
			val := streamID.String
			tr.StreamID = &val
		}
		if speaker.Valid {
			val := speaker.String
			tr.Speaker = &val
		}
		if confidence.Valid {
			val := confidence.Float64
			tr.Confidence = &val
		}
		if endTime.Valid {
			val := endTime.Time
			tr.EndTime = &val
		}

		transcriptions = append(transcriptions, tr)
	}

	return transcriptions, nil
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
			vendor_type, ucid, oracle_ucid, conversation_id, cisco_session_id,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.db.ExecContext(ctx, query,
		cdr.ID, cdr.SessionID, cdr.CallID, cdr.CallerID, cdr.CalleeID,
		cdr.StartTime, cdr.EndTime, cdr.Duration, cdr.RecordingPath,
		cdr.RecordingSize, cdr.TranscriptionID, cdr.Quality, cdr.Transport,
		cdr.SourceIP, cdr.Codec, cdr.SampleRate, cdr.ParticipantCount,
		cdr.StreamCount, cdr.Status, cdr.ErrorMessage, cdr.BillingCode,
		cdr.CostCenter, cdr.VendorType, cdr.UCID, cdr.OracleUCID,
		cdr.ConversationID, cdr.CiscoSessionID, cdr.CreatedAt, cdr.UpdatedAt,
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

// User operations

// CreateUser inserts a user record.
func (r *Repository) CreateUser(user *User) error {
	ctx, cancel := r.db.getContext()
	defer cancel()

	user.ID = uuid.New().String()
	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now

	query := `
		INSERT INTO users (
			id, username, email, password_hash, role, is_active,
			last_login, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.db.ExecContext(ctx, query,
		user.ID, user.Username, user.Email, user.PasswordHash,
		user.Role, user.IsActive, user.LastLogin, user.CreatedAt, user.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	return nil
}

// GetUserByID retrieves a user by ID.
func (r *Repository) GetUserByID(id string) (*User, error) {
	ctx, cancel := r.db.getContext()
	defer cancel()

	query := `
		SELECT id, username, email, password_hash, role, is_active,
		       last_login, created_at, updated_at
		FROM users WHERE id = ?
	`

	user := &User{}
	var lastLogin sql.NullTime

	err := r.db.db.QueryRowContext(ctx, query, id).Scan(
		&user.ID, &user.Username, &user.Email, &user.PasswordHash,
		&user.Role, &user.IsActive, &lastLogin, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found: %s", id)
		}
		return nil, fmt.Errorf("failed to query user: %w", err)
	}

	if lastLogin.Valid {
		val := lastLogin.Time
		user.LastLogin = &val
	}

	return user, nil
}

// GetUserByUsername retrieves a user by username.
func (r *Repository) GetUserByUsername(username string) (*User, error) {
	ctx, cancel := r.db.getContext()
	defer cancel()

	query := `
		SELECT id, username, email, password_hash, role, is_active,
		       last_login, created_at, updated_at
		FROM users WHERE username = ?
	`

	user := &User{}
	var lastLogin sql.NullTime

	err := r.db.db.QueryRowContext(ctx, query, username).Scan(
		&user.ID, &user.Username, &user.Email, &user.PasswordHash,
		&user.Role, &user.IsActive, &lastLogin, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found: %s", username)
		}
		return nil, fmt.Errorf("failed to query user: %w", err)
	}

	if lastLogin.Valid {
		val := lastLogin.Time
		user.LastLogin = &val
	}

	return user, nil
}

// UpdateUser updates user profile details.
func (r *Repository) UpdateUser(user *User) error {
	ctx, cancel := r.db.getContext()
	defer cancel()

	user.UpdatedAt = time.Now()

	query := `
		UPDATE users SET
			email = ?, password_hash = ?, role = ?, is_active = ?,
			last_login = ?, updated_at = ?
		WHERE id = ?
	`

	_, err := r.db.db.ExecContext(ctx, query,
		user.Email, user.PasswordHash, user.Role, user.IsActive,
		user.LastLogin, user.UpdatedAt, user.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	return nil
}

// DeactivateUser disables a user account.
func (r *Repository) DeactivateUser(id string) error {
	ctx, cancel := r.db.getContext()
	defer cancel()

	query := `
		UPDATE users SET is_active = FALSE, updated_at = ?
		WHERE id = ?
	`

	_, err := r.db.db.ExecContext(ctx, query, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to deactivate user: %w", err)
	}

	return nil
}

// API key operations

// CreateAPIKey inserts a new API key record.
func (r *Repository) CreateAPIKey(key *APIKey) error {
	ctx, cancel := r.db.getContext()
	defer cancel()

	key.ID = uuid.New().String()
	now := time.Now()
	key.CreatedAt = now
	key.UpdatedAt = now

	permissionsJSON, err := json.Marshal(key.Permissions)
	if err != nil {
		return fmt.Errorf("failed to marshal permissions: %w", err)
	}

	query := `
		INSERT INTO api_keys (
			id, user_id, name, key_hash, permissions, is_active,
			expires_at, last_used, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = r.db.db.ExecContext(ctx, query,
		key.ID, key.UserID, key.Name, key.KeyHash, permissionsJSON,
		key.IsActive, key.ExpiresAt, key.LastUsed, key.CreatedAt, key.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create API key: %w", err)
	}

	return nil
}

// GetAPIKeyByID retrieves an API key by ID.
func (r *Repository) GetAPIKeyByID(id string) (*APIKey, error) {
	ctx, cancel := r.db.getContext()
	defer cancel()

	query := `
		SELECT id, user_id, name, key_hash, permissions, is_active,
		       expires_at, last_used, created_at, updated_at
		FROM api_keys
		WHERE id = ?
	`

	key := &APIKey{}
	var permissionsJSON []byte
	var expiresAt, lastUsed sql.NullTime

	err := r.db.db.QueryRowContext(ctx, query, id).Scan(
		&key.ID, &key.UserID, &key.Name, &key.KeyHash, &permissionsJSON,
		&key.IsActive, &expiresAt, &lastUsed, &key.CreatedAt, &key.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("api key not found: %s", id)
		}
		return nil, fmt.Errorf("failed to query api key: %w", err)
	}

	if err := json.Unmarshal(permissionsJSON, &key.Permissions); err != nil {
		return nil, fmt.Errorf("failed to unmarshal permissions: %w", err)
	}
	if expiresAt.Valid {
		val := expiresAt.Time
		key.ExpiresAt = &val
	}
	if lastUsed.Valid {
		val := lastUsed.Time
		key.LastUsed = &val
	}

	return key, nil
}

// GetAPIKeysByUser returns API keys for a user.
func (r *Repository) GetAPIKeysByUser(userID string) ([]*APIKey, error) {
	ctx, cancel := r.db.getContext()
	defer cancel()

	query := `
		SELECT id, user_id, name, key_hash, permissions, is_active,
		       expires_at, last_used, created_at, updated_at
		FROM api_keys
		WHERE user_id = ?
		ORDER BY created_at DESC
	`

	rows, err := r.db.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query api keys: %w", err)
	}
	defer rows.Close()

	var keys []*APIKey
	for rows.Next() {
		key := &APIKey{}
		var permissionsJSON []byte
		var expiresAt, lastUsed sql.NullTime

		if err := rows.Scan(
			&key.ID, &key.UserID, &key.Name, &key.KeyHash, &permissionsJSON,
			&key.IsActive, &expiresAt, &lastUsed, &key.CreatedAt, &key.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan api key row: %w", err)
		}

		if err := json.Unmarshal(permissionsJSON, &key.Permissions); err != nil {
			return nil, fmt.Errorf("failed to unmarshal permissions: %w", err)
		}
		if expiresAt.Valid {
			val := expiresAt.Time
			key.ExpiresAt = &val
		}
		if lastUsed.Valid {
			val := lastUsed.Time
			key.LastUsed = &val
		}

		keys = append(keys, key)
	}

	return keys, nil
}

// DeactivateAPIKey marks an API key inactive.
func (r *Repository) DeactivateAPIKey(id string) error {
	ctx, cancel := r.db.getContext()
	defer cancel()

	query := `
		UPDATE api_keys SET is_active = FALSE, updated_at = ?, last_used = ?
		WHERE id = ?
	`

	now := time.Now()
	_, err := r.db.db.ExecContext(ctx, query, now, now, id)
	if err != nil {
		return fmt.Errorf("failed to deactivate api key: %w", err)
	}

	return nil
}

// DeleteAPIKey removes an API key permanently.
func (r *Repository) DeleteAPIKey(id string) error {
	ctx, cancel := r.db.getContext()
	defer cancel()

	_, err := r.db.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete api key: %w", err)
	}

	return nil
}

// Search index maintenance

// UpsertSearchIndex creates or updates a search index entry.
func (r *Repository) UpsertSearchIndex(entry *SearchIndex) error {
	ctx, cancel := r.db.getContext()
	defer cancel()

	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	entry.UpdatedAt = time.Now()

	query := `
		INSERT INTO search_index (
			id, type, entity_id, content, metadata, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			content = VALUES(content),
			metadata = VALUES(metadata),
			updated_at = VALUES(updated_at)
	`

	_, err := r.db.db.ExecContext(ctx, query,
		entry.ID, entry.Type, entry.EntityID,
		entry.Content, entry.Metadata, entry.CreatedAt, entry.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert search index: %w", err)
	}

	return nil
}

// DeleteSearchIndexByEntity removes search index entries for an entity.
func (r *Repository) DeleteSearchIndexByEntity(entityID string) error {
	ctx, cancel := r.db.getContext()
	defer cancel()

	_, err := r.db.db.ExecContext(ctx, `DELETE FROM search_index WHERE entity_id = ?`, entityID)
	if err != nil {
		return fmt.Errorf("failed to delete search index entries: %w", err)
	}

	return nil
}

// GetSessionByCallID retrieves the latest session associated with a call ID.
func (r *Repository) GetSessionByCallID(callID string) (*Session, error) {
	ctx, cancel := r.db.getContext()
	defer cancel()

	query := `SELECT id, call_id, session_id, status, transport, source_ip, source_port,
	local_ip, local_port, start_time, end_time, duration, recording_path, metadata_xml,
	sdp, participants, created_at, updated_at FROM sessions WHERE call_id = ? ORDER BY start_time DESC LIMIT 1`

	row := r.db.db.QueryRowContext(ctx, query, callID)
	session := &Session{}
	var (
		endTime       sql.NullTime
		duration      sql.NullInt64
		recordingPath sql.NullString
		metadataXML   sql.NullString
		sdp           sql.NullString
	)

	if err := row.Scan(
		&session.ID,
		&session.CallID,
		&session.SessionID,
		&session.Status,
		&session.Transport,
		&session.SourceIP,
		&session.SourcePort,
		&session.LocalIP,
		&session.LocalPort,
		&session.StartTime,
		&endTime,
		&duration,
		&recordingPath,
		&metadataXML,
		&sdp,
		&session.Participants,
		&session.CreatedAt,
		&session.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("session not found for call %s", callID)
		}
		return nil, fmt.Errorf("failed to fetch session: %w", err)
	}

	if endTime.Valid {
		session.EndTime = &endTime.Time
	}
	if duration.Valid {
		value := duration.Int64
		session.Duration = &value
	}
	if recordingPath.Valid {
		session.RecordingPath = recordingPath.String
	}
	if metadataXML.Valid {
		value := metadataXML.String
		session.MetadataXML = &value
	}
	if sdp.Valid {
		value := sdp.String
		session.SDP = &value
	}

	return session, nil
}

// GetCDRByCallID fetches a CDR entry for the specified call ID.
func (r *Repository) GetCDRByCallID(callID string) (*CDR, error) {
	ctx, cancel := r.db.getContext()
	defer cancel()

	query := `SELECT id, session_id, call_id, caller_id, callee_id, start_time, end_time, duration,
	recording_path, recording_size, transcription_id, quality, transport, source_ip, codec, sample_rate,
	participant_count, stream_count, status, error_message, billing_code, cost_center, created_at, updated_at
	FROM cdr WHERE call_id = ? LIMIT 1`

	row := r.db.db.QueryRowContext(ctx, query, callID)
	cdr := &CDR{}
	var (
		callerID      sql.NullString
		calleeID      sql.NullString
		endTime       sql.NullTime
		duration      sql.NullInt64
		recordingPath sql.NullString
		recordingSize sql.NullInt64
		transcription sql.NullString
		quality       sql.NullFloat64
		codec         sql.NullString
		sampleRate    sql.NullInt64
		errorMessage  sql.NullString
		billingCode   sql.NullString
		costCenter    sql.NullString
	)

	if err := row.Scan(
		&cdr.ID,
		&cdr.SessionID,
		&cdr.CallID,
		&callerID,
		&calleeID,
		&cdr.StartTime,
		&endTime,
		&duration,
		&recordingPath,
		&recordingSize,
		&transcription,
		&quality,
		&cdr.Transport,
		&cdr.SourceIP,
		&codec,
		&sampleRate,
		&cdr.ParticipantCount,
		&cdr.StreamCount,
		&cdr.Status,
		&errorMessage,
		&billingCode,
		&costCenter,
		&cdr.CreatedAt,
		&cdr.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("cdr not found for call %s", callID)
		}
		return nil, fmt.Errorf("failed to fetch cdr: %w", err)
	}

	if callerID.Valid {
		cdr.CallerID = &callerID.String
	}
	if calleeID.Valid {
		cdr.CalleeID = &calleeID.String
	}
	if endTime.Valid {
		cdr.EndTime = &endTime.Time
	}
	if duration.Valid {
		value := duration.Int64
		cdr.Duration = &value
	}
	if recordingPath.Valid {
		cdr.RecordingPath = recordingPath.String
	}
	if recordingSize.Valid {
		value := recordingSize.Int64
		cdr.RecordingSize = &value
	}
	if transcription.Valid {
		cdr.TranscriptionID = &transcription.String
	}
	if quality.Valid {
		value := quality.Float64
		cdr.Quality = &value
	}
	if codec.Valid {
		cdr.Codec = &codec.String
	}
	if sampleRate.Valid {
		value := int(sampleRate.Int64)
		cdr.SampleRate = &value
	}
	if errorMessage.Valid {
		cdr.ErrorMessage = &errorMessage.String
	}
	if billingCode.Valid {
		cdr.BillingCode = &billingCode.String
	}
	if costCenter.Valid {
		cdr.CostCenter = &costCenter.String
	}

	return cdr, nil
}

// DeleteCallData removes all persisted information for a call.
func (r *Repository) DeleteCallData(callID string) error {
	ctx, cancel := r.db.getContext()
	defer cancel()

	tx, err := r.db.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	var sessionIDs []string
	rows, err := tx.QueryContext(ctx, `SELECT id FROM sessions WHERE call_id = ?`, callID)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to query sessions: %w", err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			sessionIDs = append(sessionIDs, id)
		}
	}
	rows.Close()

	for _, sessionID := range sessionIDs {
		if _, err := tx.ExecContext(ctx, `DELETE FROM transcriptions WHERE session_id = ?`, sessionID); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to delete transcriptions: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM participants WHERE session_id = ?`, sessionID); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to delete participants: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM streams WHERE session_id = ?`, sessionID); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to delete streams: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE session_id = ?`, sessionID); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to delete events: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM search_index WHERE entity_id = ?`, sessionID); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to delete search index entries: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM cdr WHERE call_id = ?`, callID); err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete cdr: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE call_id = ?`, callID); err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM search_index WHERE entity_id = ?`, callID); err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete call search index entries: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit deletion: %w", err)
	}

	return nil
}
