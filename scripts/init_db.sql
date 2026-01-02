-- Database Initialization Script for SIPREC Server
-- Derived from pkg/database/models.go

-- Sessions Table
CREATE TABLE IF NOT EXISTS sessions (
    id              VARCHAR(64) PRIMARY KEY,
    call_id         VARCHAR(128) NOT NULL,
    session_id      VARCHAR(128) NOT NULL,
    status          VARCHAR(32),
    transport       VARCHAR(32),
    source_ip       VARCHAR(64),
    source_port     INTEGER,
    local_ip        VARCHAR(64),
    local_port      INTEGER,
    start_time      TIMESTAMP NOT NULL,
    end_time        TIMESTAMP,
    duration        BIGINT,
    recording_path  TEXT,
    metadata_xml    TEXT,
    sdp             TEXT,
    participants    INTEGER DEFAULT 0,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_sessions_call_id ON sessions(call_id);
CREATE INDEX IF NOT EXISTS idx_sessions_start_time ON sessions(start_time);

-- Participants Table
CREATE TABLE IF NOT EXISTS participants (
    id              VARCHAR(64) PRIMARY KEY,
    session_id      VARCHAR(64) NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    participant_id  VARCHAR(128),
    type            VARCHAR(32),
    name_id         VARCHAR(255),
    display_name    VARCHAR(255),
    aor             VARCHAR(255),
    stream_id       VARCHAR(128),
    join_time       TIMESTAMP NOT NULL,
    leave_time      TIMESTAMP,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_participants_session_id ON participants(session_id);

-- Streams Table
CREATE TABLE IF NOT EXISTS streams (
    id              VARCHAR(64) PRIMARY KEY,
    session_id      VARCHAR(64) NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    stream_id       VARCHAR(128),
    label           VARCHAR(255),
    mode            VARCHAR(32),
    direction       VARCHAR(32),
    codec           VARCHAR(32),
    sample_rate     INTEGER,
    channels        INTEGER,
    start_time      TIMESTAMP NOT NULL,
    end_time        TIMESTAMP,
    packet_count    BIGINT,
    byte_count      BIGINT,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_streams_session_id ON streams(session_id);

-- Call Data Records (CDRs) Table
CREATE TABLE IF NOT EXISTS cdrs (
    id                VARCHAR(64) PRIMARY KEY,
    session_id        VARCHAR(64) NOT NULL,
    call_id           VARCHAR(128) NOT NULL,
    caller_id         VARCHAR(255),
    callee_id         VARCHAR(255),
    start_time        TIMESTAMP NOT NULL,
    end_time          TIMESTAMP,
    duration          BIGINT,
    recording_path    TEXT,
    recording_size    BIGINT,
    transcription_id  VARCHAR(64),
    quality           DOUBLE PRECISION,
    transport         VARCHAR(32),
    source_ip         VARCHAR(64),
    codec             VARCHAR(32),
    sample_rate       INTEGER,
    participant_count INTEGER,
    stream_count      INTEGER,
    status            VARCHAR(32),
    error_message     TEXT,
    billing_code      VARCHAR(64),
    cost_center       VARCHAR(64),
    created_at        TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at        TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_cdrs_session_id ON cdrs(session_id);
CREATE INDEX IF NOT EXISTS idx_cdrs_start_time ON cdrs(start_time);
CREATE INDEX IF NOT EXISTS idx_cdrs_billing ON cdrs(billing_code);

-- Transcriptions Table
CREATE TABLE IF NOT EXISTS transcriptions (
    id          VARCHAR(64) PRIMARY KEY,
    session_id  VARCHAR(64) NOT NULL,
    stream_id   VARCHAR(128),
    provider    VARCHAR(32),
    language    VARCHAR(12),
    text        TEXT,
    confidence  DOUBLE PRECISION,
    start_time  TIMESTAMP NOT NULL,
    end_time    TIMESTAMP,
    word_count  INTEGER,
    speaker     VARCHAR(64),
    is_final    BOOLEAN DEFAULT FALSE,
    created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_transcriptions_session_id ON transcriptions(session_id);

-- Users Table
CREATE TABLE IF NOT EXISTS users (
    id            VARCHAR(64) PRIMARY KEY,
    username      VARCHAR(64) NOT NULL UNIQUE,
    email         VARCHAR(255) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    role          VARCHAR(32) NOT NULL,
    is_active     BOOLEAN DEFAULT TRUE,
    last_login    TIMESTAMP,
    created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- API Keys Table
CREATE TABLE IF NOT EXISTS api_keys (
    id          VARCHAR(64) PRIMARY KEY,
    user_id     VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        VARCHAR(64),
    key_hash    VARCHAR(255) NOT NULL,
    permissions TEXT, -- JSON array stored as text
    is_active   BOOLEAN DEFAULT TRUE,
    expires_at  TIMESTAMP,
    last_used   TIMESTAMP,
    created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id);
