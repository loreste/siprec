package config

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"siprec-server/pkg/errors"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
)

// Config represents the complete application configuration
type Config struct {
	Network         NetworkConfig          `json:"network"`
	HTTP            HTTPConfig             `json:"http"`
	Recording       RecordingConfig        `json:"recording"`
	STT             STTConfig              `json:"stt"`
	Resources       ResourceConfig         `json:"resources"`
	Logging         LoggingConfig          `json:"logging"`
	Messaging       MessagingConfig        `json:"messaging"`
	Redundancy      RedundancyConfig       `json:"redundancy"`
	Encryption      EncryptionConfig       `json:"encryption"`
	AsyncSTT        AsyncSTTConfig         `json:"async_stt"`
	HotReload       HotReloadConfig        `json:"hot_reload"`
	Performance     PerformanceConfig      `json:"performance"`
	CircuitBreaker  CircuitBreakerConfig   `json:"circuit_breaker"`
	PauseResume     PauseResumeConfig      `json:"pause_resume"`
	PII             PIIConfig              `json:"pii"`
	Tracing         TracingConfig          `json:"tracing"`
	Analytics       AnalyticsConfig        `json:"analytics"`
	Database        DatabaseConfig         `json:"database"`
	Compliance      ComplianceConfig       `json:"compliance"`
	Auth            AuthConfig             `json:"auth"`
	Alerting        AlertingConfig         `json:"alerting"`
	Cluster         ClusterConfig          `json:"cluster"`
	AudioProcessing AudioEnhancementConfig `json:"audio_processing"`
	RateLimit       RateLimitConfig        `json:"rate_limit"`
}

// ClusterConfig holds cluster management configuration
type ClusterConfig struct {
	Enabled           bool          `json:"enabled" env:"CLUSTER_ENABLED" default:"false"`
	NodeID            string        `json:"node_id" env:"CLUSTER_NODE_ID" default:"node-1"`
	HeartbeatInterval time.Duration `json:"heartbeat_interval" env:"CLUSTER_HEARTBEAT_INTERVAL" default:"5s"`
	NodeTTL           time.Duration `json:"node_ttl" env:"CLUSTER_NODE_TTL" default:"15s"`

	// Leader election configuration
	LeaderElectionEnabled bool          `json:"leader_election_enabled" env:"CLUSTER_LEADER_ELECTION_ENABLED" default:"true"`
	LeaderLockTTL         time.Duration `json:"leader_lock_ttl" env:"CLUSTER_LEADER_LOCK_TTL" default:"10s"`
	LeaderRetryInterval   time.Duration `json:"leader_retry_interval" env:"CLUSTER_LEADER_RETRY_INTERVAL" default:"3s"`

	// Redis configuration
	Redis RedisClusterConfig `json:"redis"`

	// RTP State Replication
	RTPStateReplication bool `json:"rtp_state_replication" env:"CLUSTER_RTP_STATE_REPLICATION" default:"true"`

	// Distributed Rate Limiting
	DistributedRateLimiting bool `json:"distributed_rate_limiting" env:"CLUSTER_DISTRIBUTED_RATE_LIMITING" default:"true"`

	// Distributed Tracing
	DistributedTracing bool `json:"distributed_tracing" env:"CLUSTER_DISTRIBUTED_TRACING" default:"true"`

	// Stream Migration
	StreamMigration bool `json:"stream_migration" env:"CLUSTER_STREAM_MIGRATION" default:"true"`

	// Split-brain detection
	SplitBrainDetection SplitBrainConfig `json:"split_brain_detection"`
}

// RedisClusterConfig holds Redis cluster/sentinel configuration
type RedisClusterConfig struct {
	// Mode: standalone, sentinel, cluster
	Mode string `json:"mode" env:"REDIS_MODE" default:"standalone"`

	// Standalone configuration
	Address  string `json:"address" env:"REDIS_ADDRESS" default:"localhost:6379"`
	Password string `json:"password" env:"REDIS_PASSWORD"`
	Database int    `json:"database" env:"REDIS_DATABASE" default:"0"`

	// Sentinel configuration
	SentinelAddresses  []string `json:"sentinel_addresses" env:"REDIS_SENTINEL_ADDRESSES"`
	SentinelMasterName string   `json:"sentinel_master_name" env:"REDIS_SENTINEL_MASTER" default:"mymaster"`
	SentinelPassword   string   `json:"sentinel_password" env:"REDIS_SENTINEL_PASSWORD"`

	// Cluster configuration
	ClusterAddresses []string `json:"cluster_addresses" env:"REDIS_CLUSTER_ADDRESSES"`

	// Connection pool
	PoolSize     int           `json:"pool_size" env:"REDIS_POOL_SIZE" default:"20"`
	MinIdleConns int           `json:"min_idle_conns" env:"REDIS_MIN_IDLE_CONNS" default:"5"`
	DialTimeout  time.Duration `json:"dial_timeout" env:"REDIS_DIAL_TIMEOUT" default:"5s"`
	ReadTimeout  time.Duration `json:"read_timeout" env:"REDIS_READ_TIMEOUT" default:"3s"`
	WriteTimeout time.Duration `json:"write_timeout" env:"REDIS_WRITE_TIMEOUT" default:"3s"`
	PoolTimeout  time.Duration `json:"pool_timeout" env:"REDIS_POOL_TIMEOUT" default:"4s"`

	// Retry configuration
	MaxRetries      int           `json:"max_retries" env:"REDIS_MAX_RETRIES" default:"3"`
	MinRetryBackoff time.Duration `json:"min_retry_backoff" env:"REDIS_MIN_RETRY_BACKOFF" default:"8ms"`
	MaxRetryBackoff time.Duration `json:"max_retry_backoff" env:"REDIS_MAX_RETRY_BACKOFF" default:"512ms"`

	// TLS configuration
	TLSEnabled            bool   `json:"tls_enabled" env:"REDIS_TLS_ENABLED" default:"false"`
	TLSCertFile           string `json:"tls_cert_file" env:"REDIS_TLS_CERT_FILE"`
	TLSKeyFile            string `json:"tls_key_file" env:"REDIS_TLS_KEY_FILE"`
	TLSCAFile             string `json:"tls_ca_file" env:"REDIS_TLS_CA_FILE"`
	TLSInsecureSkipVerify bool   `json:"tls_insecure_skip_verify" env:"REDIS_TLS_INSECURE_SKIP_VERIFY" default:"false"`

	// Routing
	RouteByLatency bool `json:"route_by_latency" env:"REDIS_ROUTE_BY_LATENCY" default:"true"`
	RouteRandomly  bool `json:"route_randomly" env:"REDIS_ROUTE_RANDOMLY" default:"false"`
}

// SplitBrainConfig holds split-brain detection configuration
type SplitBrainConfig struct {
	Enabled         bool          `json:"enabled" env:"CLUSTER_SPLIT_BRAIN_ENABLED" default:"true"`
	MinQuorum       int           `json:"min_quorum" env:"CLUSTER_MIN_QUORUM" default:"2"`
	CheckInterval   time.Duration `json:"check_interval" env:"CLUSTER_SPLIT_BRAIN_CHECK_INTERVAL" default:"5s"`
	NodeTimeout     time.Duration `json:"node_timeout" env:"CLUSTER_NODE_TIMEOUT" default:"3s"`
	GracePeriod     time.Duration `json:"grace_period" env:"CLUSTER_SPLIT_BRAIN_GRACE_PERIOD" default:"15s"`
	PartitionAction string        `json:"partition_action" env:"CLUSTER_PARTITION_ACTION" default:"readonly"` // readonly, shutdown, continue
	EnableFencing   bool          `json:"enable_fencing" env:"CLUSTER_ENABLE_FENCING" default:"true"`
}

// DatabaseConfig controls database persistence
type DatabaseConfig struct {
	Enabled bool `json:"enabled" env:"DATABASE_ENABLED" default:"false"`
}

// ComplianceConfig groups compliance-related settings.
type ComplianceConfig struct {
	PCI struct {
		Enabled bool `json:"enabled" env:"COMPLIANCE_PCI_ENABLED" default:"false"`
	} `json:"pci"`

	GDPR struct {
		Enabled   bool   `json:"enabled" env:"COMPLIANCE_GDPR_ENABLED" default:"false"`
		ExportDir string `json:"export_dir" env:"COMPLIANCE_GDPR_EXPORT_DIR" default:"./exports"`
	} `json:"gdpr"`

	Audit struct {
		TamperProof bool   `json:"tamper_proof" env:"COMPLIANCE_AUDIT_TAMPER_PROOF" default:"false"`
		LogPath     string `json:"log_path" env:"COMPLIANCE_AUDIT_LOG_PATH" default:"./logs/audit-chain.log"`
	} `json:"audit"`
}

// NetworkConfig holds network-related configurations
type NetworkConfig struct {
	// External IP address for SIP/RTP (auto = auto-detect)
	ExternalIP string `json:"external_ip" env:"EXTERNAL_IP" default:"auto"`

	// Internal IP address for binding (auto = auto-detect)
	InternalIP string `json:"internal_ip" env:"INTERNAL_IP" default:"auto"`

	// SIP host address to bind to (0.0.0.0 = all interfaces)
	Host string `json:"host" env:"SIP_HOST" default:"0.0.0.0"`

	// SIP ports to listen on (both UDP and TCP)
	Ports []int `json:"ports" env:"PORTS" default:"5060,5061"`

	// UDP-specific SIP ports (overrides Ports for UDP if set)
	UDPPorts []int `json:"udp_ports" env:"UDP_PORTS"`

	// TCP-specific SIP ports (overrides Ports for TCP if set)
	TCPPorts []int `json:"tcp_ports" env:"TCP_PORTS"`

	// Whether SRTP is enabled
	EnableSRTP bool `json:"enable_srtp" env:"ENABLE_SRTP" default:"false"`

	// RTP port range minimum
	RTPPortMin int `json:"rtp_port_min" env:"RTP_PORT_MIN" default:"10000"`

	// RTP port range maximum
	RTPPortMax int `json:"rtp_port_max" env:"RTP_PORT_MAX" default:"20000"`

	// RTP inactivity timeout before a forwarder is closed
	RTPTimeout time.Duration `json:"rtp_timeout" env:"RTP_TIMEOUT" default:"30s"`

	// RTP bind IP address (empty = 0.0.0.0 - all interfaces)
	RTPBindIP string `json:"rtp_bind_ip" env:"RTP_BIND_IP" default:""`

	// TLS certificate file
	TLSCertFile string `json:"tls_cert_file" env:"TLS_CERT_PATH"`

	// TLS key file
	TLSKeyFile string `json:"tls_key_file" env:"TLS_KEY_PATH"`

	// TLS port
	TLSPort int `json:"tls_port" env:"TLS_PORT" default:"5062"`

	// Whether TLS is enabled
	EnableTLS bool `json:"enable_tls" env:"ENABLE_TLS" default:"false"`

	// Whether the server is behind NAT
	BehindNAT bool `json:"behind_nat" env:"BEHIND_NAT" default:"false"`

	// Whether only TLS (SIPS) connections should be accepted
	RequireTLSOnly bool `json:"require_tls_only" env:"SIP_REQUIRE_TLS" default:"false"`
	RequireSRTP    bool `json:"require_srtp" env:"SIP_REQUIRE_SRTP" default:"false"`

	// STUN servers for NAT traversal
	STUNServers []string `json:"stun_servers" env:"STUN_SERVER"`

	// Whether audio processing is enabled
	EnableAudioProcessing bool `json:"enable_audio_processing" env:"ENABLE_AUDIO_PROCESSING" default:"true"`
}

// HTTPConfig holds HTTP server configurations
type HTTPConfig struct {
	// HTTP port
	Port int `json:"port" env:"HTTP_PORT" default:"8080"`

	// Whether HTTP server is enabled
	Enabled bool `json:"enabled" env:"HTTP_ENABLED" default:"true"`

	// Whether metrics endpoint is enabled
	EnableMetrics bool `json:"enable_metrics" env:"HTTP_ENABLE_METRICS" default:"true"`

	// Whether API endpoints are enabled
	EnableAPI bool `json:"enable_api" env:"HTTP_ENABLE_API" default:"true"`

	// Read timeout for HTTP requests
	ReadTimeout time.Duration `json:"read_timeout" env:"HTTP_READ_TIMEOUT" default:"10s"`

	// Write timeout for HTTP responses
	WriteTimeout time.Duration `json:"write_timeout" env:"HTTP_WRITE_TIMEOUT" default:"30s"`

	// Enable TLS for the HTTP server
	TLSEnabled bool `json:"tls_enabled" env:"HTTP_TLS_ENABLED" default:"false"`

	// TLS certificate path for HTTP server
	TLSCertFile string `json:"tls_cert_file" env:"HTTP_TLS_CERT_FILE"`

	// TLS key path for HTTP server
	TLSKeyFile string `json:"tls_key_file" env:"HTTP_TLS_KEY_FILE"`
}

// RecordingConfig holds recording-related configurations
type RecordingConfig struct {
	// Directory to store recordings
	Directory string `json:"directory" env:"RECORDING_DIR" default:"./recordings"`

	// Maximum duration for recordings
	MaxDuration time.Duration `json:"max_duration" env:"RECORDING_MAX_DURATION_HOURS" default:"4h"`

	// Days to keep recordings before cleanup
	CleanupDays int `json:"cleanup_days" env:"RECORDING_CLEANUP_DAYS" default:"30"`

	// Storage configuration for recordings
	Storage RecordingStorageConfig `json:"storage"`

	// CombineLegs determines whether multi-stream SIPREC legs are merged into a single file
	CombineLegs bool `json:"combine_legs" env:"RECORDING_COMBINE_LEGS" default:"true"`

	// Audio format for recordings: wav, mp3, opus, mp4, ogg
	Format string `json:"format" env:"RECORDING_FORMAT" default:"wav"`

	// MP3 encoding settings (when format=mp3)
	MP3Bitrate int `json:"mp3_bitrate" env:"RECORDING_MP3_BITRATE" default:"128"`

	// Opus encoding settings (when format=opus or ogg)
	OpusBitrate int `json:"opus_bitrate" env:"RECORDING_OPUS_BITRATE" default:"64"`

	// Quality setting (1-10, higher is better, affects MP3/Opus)
	Quality int `json:"quality" env:"RECORDING_QUALITY" default:"5"`
}

// RecordingStorageConfig defines remote storage options for recordings
type RecordingStorageConfig struct {
	Enabled   bool `json:"enabled" env:"RECORDING_STORAGE_ENABLED" default:"false"`
	KeepLocal bool `json:"keep_local" env:"RECORDING_STORAGE_KEEP_LOCAL" default:"true"`

	S3    RecordingS3Config    `json:"s3"`
	GCS   RecordingGCSConfig   `json:"gcs"`
	Azure RecordingAzureConfig `json:"azure"`
}

// RecordingS3Config holds AWS S3 configuration for recordings
type RecordingS3Config struct {
	Enabled   bool   `json:"enabled" env:"RECORDING_STORAGE_S3_ENABLED" default:"false"`
	Bucket    string `json:"bucket" env:"RECORDING_STORAGE_S3_BUCKET"`
	Region    string `json:"region" env:"RECORDING_STORAGE_S3_REGION"`
	AccessKey string `json:"access_key" env:"RECORDING_STORAGE_S3_ACCESS_KEY"`
	SecretKey string `json:"secret_key" env:"RECORDING_STORAGE_S3_SECRET_KEY"`
	Prefix    string `json:"prefix" env:"RECORDING_STORAGE_S3_PREFIX"`
}

// RecordingGCSConfig holds Google Cloud Storage configuration for recordings
type RecordingGCSConfig struct {
	Enabled           bool   `json:"enabled" env:"RECORDING_STORAGE_GCS_ENABLED" default:"false"`
	Bucket            string `json:"bucket" env:"RECORDING_STORAGE_GCS_BUCKET"`
	ServiceAccountKey string `json:"service_account_key" env:"RECORDING_STORAGE_GCS_SERVICE_ACCOUNT"`
	Prefix            string `json:"prefix" env:"RECORDING_STORAGE_GCS_PREFIX"`
}

// RecordingAzureConfig holds Azure Blob Storage configuration for recordings
type RecordingAzureConfig struct {
	Enabled   bool   `json:"enabled" env:"RECORDING_STORAGE_AZURE_ENABLED" default:"false"`
	Account   string `json:"account" env:"RECORDING_STORAGE_AZURE_ACCOUNT"`
	Container string `json:"container" env:"RECORDING_STORAGE_AZURE_CONTAINER"`
	AccessKey string `json:"access_key" env:"RECORDING_STORAGE_AZURE_ACCESS_KEY"`
	Prefix    string `json:"prefix" env:"RECORDING_STORAGE_AZURE_PREFIX"`
}

// STTConfig holds speech-to-text configurations
type STTConfig struct {
	// Supported STT vendors
	SupportedVendors []string `json:"supported_vendors" env:"SUPPORTED_VENDORS" default:"google,openai"`

	// Supported audio codecs
	SupportedCodecs []string `json:"supported_codecs" env:"SUPPORTED_CODECS" default:"PCMU,PCMA,G722"`

	// Default STT vendor
	DefaultVendor string `json:"default_vendor" env:"DEFAULT_SPEECH_VENDOR" default:"google"`

	// Enable automatic fallback to other providers on failure
	// When false, only the requested/default provider is used with no fallback
	EnableFallback bool `json:"enable_fallback" env:"STT_ENABLE_FALLBACK" default:"true"`

	// Provider-specific configurations
	Google       GoogleSTTConfig       `json:"google"`
	Deepgram     DeepgramSTTConfig     `json:"deepgram"`
	Azure        AzureSTTConfig        `json:"azure"`
	Amazon       AmazonSTTConfig       `json:"amazon"`
	OpenAI       OpenAISTTConfig       `json:"openai"`
	Whisper      WhisperSTTConfig      `json:"whisper"`
	ElevenLabs   ElevenLabsSTTConfig   `json:"elevenlabs"`
	Speechmatics SpeechmaticsSTTConfig `json:"speechmatics"`
	OpenSource   OpenSourceSTTConfig   `json:"opensource"`

	// Language-specific routing (language code -> provider name)
	LanguageRouting map[string]string `json:"language_routing"`
}

// GoogleSTTConfig holds Google Speech-to-Text configuration
type GoogleSTTConfig struct {
	// Whether Google STT is enabled
	Enabled bool `json:"enabled" env:"GOOGLE_STT_ENABLED" default:"true"`

	// Google Cloud credentials file path
	CredentialsFile string `json:"credentials_file" env:"GOOGLE_APPLICATION_CREDENTIALS"`

	// Google Cloud project ID
	ProjectID string `json:"project_id" env:"GOOGLE_PROJECT_ID"`

	// API key (alternative to credentials file)
	APIKey string `json:"api_key" env:"GOOGLE_STT_API_KEY"`

	// Default language code
	Language string `json:"language" env:"GOOGLE_STT_LANGUAGE" default:"en-US"`

	// Sample rate for audio
	SampleRate int `json:"sample_rate" env:"GOOGLE_STT_SAMPLE_RATE" default:"16000"`

	// Enable enhanced models
	EnhancedModels bool `json:"enhanced_models" env:"GOOGLE_STT_ENHANCED_MODELS" default:"false"`

	// Model to use (latest_long, latest_short, etc.)
	Model string `json:"model" env:"GOOGLE_STT_MODEL" default:"latest_long"`

	// Enable automatic punctuation
	EnableAutomaticPunctuation bool `json:"enable_automatic_punctuation" env:"GOOGLE_STT_AUTO_PUNCTUATION" default:"true"`

	// Enable word time offsets
	EnableWordTimeOffsets bool `json:"enable_word_time_offsets" env:"GOOGLE_STT_WORD_TIME_OFFSETS" default:"true"`

	// Max alternatives to return
	MaxAlternatives int `json:"max_alternatives" env:"GOOGLE_STT_MAX_ALTERNATIVES" default:"1"`

	// Profanity filter
	ProfanityFilter bool `json:"profanity_filter" env:"GOOGLE_STT_PROFANITY_FILTER" default:"false"`

	// Use streaming gRPC for real-time transcription (recommended)
	UseStreaming bool `json:"use_streaming" env:"GOOGLE_STT_USE_STREAMING" default:"true"`

	// Enable speaker diarization
	EnableDiarization bool `json:"enable_diarization" env:"GOOGLE_STT_DIARIZATION" default:"false"`

	// Number of speakers for diarization
	DiarizationSpeakerCount int `json:"diarization_speaker_count" env:"GOOGLE_STT_DIARIZATION_SPEAKERS" default:"2"`
}

// DeepgramSTTConfig holds Deepgram Speech-to-Text configuration
type DeepgramSTTConfig struct {
	// Whether Deepgram STT is enabled
	Enabled bool `json:"enabled" env:"DEEPGRAM_STT_ENABLED" default:"false"`

	// Deepgram API key
	APIKey string `json:"api_key" env:"DEEPGRAM_API_KEY"`

	// API URL (for self-hosted)
	APIURL string `json:"api_url" env:"DEEPGRAM_API_URL" default:"https://api.deepgram.com"`

	// Model to use (nova-2, nova, enhanced, base)
	Model string `json:"model" env:"DEEPGRAM_MODEL" default:"nova-2"`

	// Language
	Language string `json:"language" env:"DEEPGRAM_LANGUAGE" default:"en-US"`

	// Audio encoding (mulaw, alaw, linear16, etc.)
	Encoding string `json:"encoding" env:"DEEPGRAM_ENCODING" default:"mulaw"`

	// Audio sample rate
	SampleRate int `json:"sample_rate" env:"DEEPGRAM_SAMPLE_RATE" default:"8000"`

	// Number of audio channels
	Channels int `json:"channels" env:"DEEPGRAM_CHANNELS" default:"1"`

	// Tier (nova, enhanced, base)
	Tier string `json:"tier" env:"DEEPGRAM_TIER" default:"nova"`

	// Version
	Version string `json:"version" env:"DEEPGRAM_VERSION" default:"latest"`

	// Enable punctuation
	Punctuate bool `json:"punctuate" env:"DEEPGRAM_PUNCTUATE" default:"true"`

	// Enable diarization
	Diarize bool `json:"diarize" env:"DEEPGRAM_DIARIZE" default:"false"`

	// Enable numerals conversion
	Numerals bool `json:"numerals" env:"DEEPGRAM_NUMERALS" default:"true"`

	// Smart formatting
	SmartFormat bool `json:"smart_format" env:"DEEPGRAM_SMART_FORMAT" default:"true"`

	// Profanity filter
	ProfanityFilter bool `json:"profanity_filter" env:"DEEPGRAM_PROFANITY_FILTER" default:"false"`

	// Redact sensitive information
	Redact []string `json:"redact" env:"DEEPGRAM_REDACT"`

	// Keywords to boost
	Keywords []string `json:"keywords" env:"DEEPGRAM_KEYWORDS"`

	// Multi-language and accent detection configuration
	// Enable automatic language detection
	DetectLanguage bool `json:"detect_language" env:"DEEPGRAM_DETECT_LANGUAGE" default:"true"`

	// Supported languages for detection (comma-separated)
	SupportedLanguages []string `json:"supported_languages" env:"DEEPGRAM_SUPPORTED_LANGUAGES"`

	// Language detection confidence threshold (0.0-1.0)
	LanguageConfidenceThreshold float64 `json:"language_confidence_threshold" env:"DEEPGRAM_LANGUAGE_CONFIDENCE" default:"0.7"`

	// Enable accent-specific model selection
	AccentAwareModels bool `json:"accent_aware_models" env:"DEEPGRAM_ACCENT_AWARE" default:"true"`

	// Fallback language when detection fails
	FallbackLanguage string `json:"fallback_language" env:"DEEPGRAM_FALLBACK_LANGUAGE" default:"en-US"`

	// Enable real-time language switching
	RealtimeLanguageSwitching bool `json:"realtime_language_switching" env:"DEEPGRAM_REALTIME_SWITCHING" default:"false"`

	// Language switching interval in seconds
	LanguageSwitchingInterval int `json:"language_switching_interval" env:"DEEPGRAM_SWITCHING_INTERVAL" default:"5"`

	// Enable multi-language alternative results
	MultiLanguageAlternatives bool `json:"multilang_alternatives" env:"DEEPGRAM_MULTILANG_ALTERNATIVES" default:"false"`

	// Maximum number of language alternatives to return
	MaxLanguageAlternatives int `json:"max_language_alternatives" env:"DEEPGRAM_MAX_LANG_ALTERNATIVES" default:"3"`

	// Use WebSocket streaming for real-time transcription (recommended)
	UseWebSocket bool `json:"use_websocket" env:"DEEPGRAM_USE_WEBSOCKET" default:"true"`
}

// ElevenLabsSTTConfig holds ElevenLabs Speech-to-Text configuration
type ElevenLabsSTTConfig struct {
	// Whether ElevenLabs STT is enabled
	Enabled bool `json:"enabled" env:"ELEVENLABS_STT_ENABLED" default:"false"`

	// ElevenLabs API key
	APIKey string `json:"api_key" env:"ELEVENLABS_API_KEY"`

	// Base API URL
	BaseURL string `json:"base_url" env:"ELEVENLABS_API_URL" default:"https://api.elevenlabs.io"`

	// STT model identifier
	ModelID string `json:"model_id" env:"ELEVENLABS_MODEL_ID" default:"eleven_monolingual_v1"`

	// Target language hint
	Language string `json:"language" env:"ELEVENLABS_LANGUAGE" default:"en"`

	// Enable speaker diarization
	EnableDiarization bool `json:"enable_diarization" env:"ELEVENLABS_ENABLE_DIARIZATION" default:"false"`

	// Enable word timestamps
	EnableTimestamps bool `json:"enable_timestamps" env:"ELEVENLABS_ENABLE_TIMESTAMPS" default:"true"`

	// Enable smart formatting / punctuation
	EnablePunctuation bool `json:"enable_punctuation" env:"ELEVENLABS_ENABLE_PUNCTUATION" default:"true"`

	// Group sentences into paragraphs
	EnableParagraphs bool `json:"enable_paragraphs" env:"ELEVENLABS_ENABLE_PARAGRAPHS" default:"false"`

	// Request timeout for API calls
	Timeout time.Duration `json:"timeout" env:"ELEVENLABS_TIMEOUT" default:"45s"`
}

// SpeechmaticsSTTConfig holds Speechmatics Speech-to-Text configuration
type SpeechmaticsSTTConfig struct {
	// Whether Speechmatics STT is enabled
	Enabled bool `json:"enabled" env:"SPEECHMATICS_STT_ENABLED" default:"false"`

	// Speechmatics auth token
	APIKey string `json:"api_key" env:"SPEECHMATICS_API_KEY"`

	// Base API URL
	BaseURL string `json:"base_url" env:"SPEECHMATICS_API_URL" default:"https://asr.api.speechmatics.com/v2"`

	// Language code (e.g., en-US)
	Language string `json:"language" env:"SPEECHMATICS_LANGUAGE" default:"en-US"`

	// Model (e.g., universal, meetings, etc.)
	Model string `json:"model" env:"SPEECHMATICS_MODEL" default:"universal"`

	// Enable speaker diarization
	EnableDiarization bool `json:"enable_diarization" env:"SPEECHMATICS_ENABLE_DIARIZATION" default:"false"`

	// Enable punctuation
	EnablePunctuation bool `json:"enable_punctuation" env:"SPEECHMATICS_ENABLE_PUNCTUATION" default:"true"`

	// Enable channel separation
	EnableChannelSeparation bool `json:"enable_channel_separation" env:"SPEECHMATICS_CHANNEL_SEPARATION" default:"false"`

	// Request timeout
	Timeout time.Duration `json:"timeout" env:"SPEECHMATICS_TIMEOUT" default:"60s"`
}

// AzureSTTConfig holds Azure Speech Services configuration
type AzureSTTConfig struct {
	// Whether Azure STT is enabled
	Enabled bool `json:"enabled" env:"AZURE_STT_ENABLED" default:"false"`

	// Azure Speech Services subscription key
	SubscriptionKey string `json:"subscription_key" env:"AZURE_SPEECH_KEY"`

	// Azure region
	Region string `json:"region" env:"AZURE_SPEECH_REGION"`

	// Language
	Language string `json:"language" env:"AZURE_STT_LANGUAGE" default:"en-US"`

	// Endpoint URL (for custom endpoints)
	EndpointURL string `json:"endpoint_url" env:"AZURE_STT_ENDPOINT"`

	// Enable detailed results
	EnableDetailedResults bool `json:"enable_detailed_results" env:"AZURE_STT_DETAILED_RESULTS" default:"true"`

	// Profanity filter
	ProfanityFilter string `json:"profanity_filter" env:"AZURE_STT_PROFANITY_FILTER" default:"masked"`

	// Output format (simple, detailed)
	OutputFormat string `json:"output_format" env:"AZURE_STT_OUTPUT_FORMAT" default:"detailed"`
}

// AmazonSTTConfig holds Amazon Transcribe configuration
type AmazonSTTConfig struct {
	// Whether Amazon Transcribe is enabled
	Enabled bool `json:"enabled" env:"AMAZON_STT_ENABLED" default:"false"`

	// AWS Access Key ID
	AccessKeyID string `json:"access_key_id" env:"AWS_ACCESS_KEY_ID"`

	// AWS Secret Access Key
	SecretAccessKey string `json:"secret_access_key" env:"AWS_SECRET_ACCESS_KEY"`

	// AWS Region
	Region string `json:"region" env:"AWS_REGION" default:"us-east-1"`

	// Language code
	Language string `json:"language" env:"AMAZON_STT_LANGUAGE" default:"en-US"`

	// Media format
	MediaFormat string `json:"media_format" env:"AMAZON_STT_MEDIA_FORMAT" default:"wav"`

	// Sample rate
	SampleRate int `json:"sample_rate" env:"AMAZON_STT_SAMPLE_RATE" default:"16000"`

	// Vocabulary name for custom vocabulary
	VocabularyName string `json:"vocabulary_name" env:"AMAZON_STT_VOCABULARY"`

	// Enable channel identification
	EnableChannelIdentification bool `json:"enable_channel_identification" env:"AMAZON_STT_CHANNEL_ID" default:"false"`

	// Enable speaker identification
	EnableSpeakerIdentification bool `json:"enable_speaker_identification" env:"AMAZON_STT_SPEAKER_ID" default:"false"`

	// Max speaker labels
	MaxSpeakerLabels int `json:"max_speaker_labels" env:"AMAZON_STT_MAX_SPEAKERS" default:"2"`
}

// OpenAISTTConfig holds OpenAI Whisper API configuration
type OpenAISTTConfig struct {
	// Whether OpenAI STT is enabled
	Enabled bool `json:"enabled" env:"OPENAI_STT_ENABLED" default:"false"`

	// OpenAI API key
	APIKey string `json:"api_key" env:"OPENAI_API_KEY"`

	// Organization ID (optional)
	OrganizationID string `json:"organization_id" env:"OPENAI_ORGANIZATION_ID"`

	// Model to use (whisper-1)
	Model string `json:"model" env:"OPENAI_STT_MODEL" default:"whisper-1"`

	// Language (optional, auto-detect if not specified)
	Language string `json:"language" env:"OPENAI_STT_LANGUAGE"`

	// Prompt for context
	Prompt string `json:"prompt" env:"OPENAI_STT_PROMPT"`

	// Response format (json, text, srt, verbose_json, vtt)
	ResponseFormat string `json:"response_format" env:"OPENAI_STT_RESPONSE_FORMAT" default:"verbose_json"`

	// Temperature for sampling
	Temperature float64 `json:"temperature" env:"OPENAI_STT_TEMPERATURE" default:"0.0"`

	// Base URL for API (for custom endpoints)
	BaseURL string `json:"base_url" env:"OPENAI_BASE_URL" default:"https://api.openai.com/v1"`
}

// WhisperSTTConfig holds configuration for the local Whisper CLI
type WhisperSTTConfig struct {
	// Whether Whisper CLI is enabled
	Enabled bool `json:"enabled" env:"WHISPER_ENABLED" default:"false"`

	// Path to the whisper binary (python -m whisper or compiled wrapper)
	BinaryPath string `json:"binary_path" env:"WHISPER_BINARY_PATH" default:"whisper"`

	// Model to run (tiny, base, small, medium, large, etc.)
	Model string `json:"model" env:"WHISPER_MODEL" default:"base"`

	// Language hint (leave empty for auto)
	Language string `json:"language" env:"WHISPER_LANGUAGE"`

	// Task (transcribe or translate)
	Task string `json:"task" env:"WHISPER_TASK" default:"transcribe"`

	// Whether to run in translate mode regardless of task flag
	Translate bool `json:"translate" env:"WHISPER_TRANSLATE" default:"false"`

	// Output format generated by whisper (txt, json, vtt, srt)
	OutputFormat string `json:"output_format" env:"WHISPER_OUTPUT_FORMAT" default:"json"`

	// Additional CLI arguments (space-delimited)
	ExtraArgs string `json:"extra_args" env:"WHISPER_EXTRA_ARGS"`

	// Audio parameters for the temporary WAV file
	SampleRate int `json:"sample_rate" env:"WHISPER_SAMPLE_RATE" default:"16000"`
	Channels   int `json:"channels" env:"WHISPER_CHANNELS" default:"1"`

	// Timeout for running the whisper command
	Timeout time.Duration `json:"timeout" env:"WHISPER_TIMEOUT" default:"10m"`

	// Maximum concurrent Whisper calls (0 = unlimited, -1 = auto based on CPU cores)
	MaxConcurrentCalls int `json:"max_concurrent_calls" env:"WHISPER_MAX_CONCURRENT" default:"-1"`
}

// OpenSourceSTTConfig holds configuration for open-source STT models
type OpenSourceSTTConfig struct {
	// Whether open-source STT is enabled
	Enabled bool `json:"enabled" env:"OPENSOURCE_STT_ENABLED" default:"false"`

	// Model type: granite-speech, canary-qwen, parakeet-tdt, whisper-turbo, kyutai-moshi, custom
	ModelType string `json:"model_type" env:"OPENSOURCE_MODEL_TYPE" default:"whisper-turbo"`

	// Model name/identifier (HuggingFace model ID or local path)
	ModelName string `json:"model_name" env:"OPENSOURCE_MODEL_NAME" default:"openai/whisper-large-v3-turbo"`

	// Local model path (for downloaded models)
	ModelPath string `json:"model_path" env:"OPENSOURCE_MODEL_PATH"`

	// Inference backend: http, websocket, cli, triton, vllm, tgi, ollama
	Backend string `json:"backend" env:"OPENSOURCE_BACKEND" default:"http"`

	// Base URL for HTTP-based backends
	BaseURL string `json:"base_url" env:"OPENSOURCE_BASE_URL" default:"http://localhost:8000"`

	// Transcribe endpoint path (appended to BaseURL)
	TranscribeEndpoint string `json:"transcribe_endpoint" env:"OPENSOURCE_TRANSCRIBE_ENDPOINT" default:"/stt/transcribe"`

	// WebSocket URL for streaming backends
	WebSocketURL string `json:"websocket_url" env:"OPENSOURCE_WEBSOCKET_URL"`

	// Multilingual support - auto-detects language switches mid-conversation
	UseMultilingual         bool   `json:"use_multilingual" env:"OPENSOURCE_USE_MULTILINGUAL" default:"false"`
	MultilingualWebSocketURL string `json:"multilingual_websocket_url" env:"OPENSOURCE_MULTILINGUAL_WEBSOCKET_URL"`

	// API key (optional, for secured endpoints)
	APIKey string `json:"api_key" env:"OPENSOURCE_API_KEY"`

	// Custom authentication header name
	AuthHeader string `json:"auth_header" env:"OPENSOURCE_AUTH_HEADER"`

	// Audio configuration
	SampleRate int    `json:"sample_rate" env:"OPENSOURCE_SAMPLE_RATE" default:"16000"`
	Encoding   string `json:"encoding" env:"OPENSOURCE_ENCODING" default:"wav"`
	Channels   int    `json:"channels" env:"OPENSOURCE_CHANNELS" default:"1"`
	Language   string `json:"language" env:"OPENSOURCE_LANGUAGE" default:"en"`

	// GPU configuration
	UseGPU   bool `json:"use_gpu" env:"OPENSOURCE_USE_GPU" default:"false"`
	DeviceID int  `json:"device_id" env:"OPENSOURCE_DEVICE_ID" default:"0"`

	// Performance settings
	Timeout    time.Duration `json:"timeout" env:"OPENSOURCE_TIMEOUT" default:"60s"`
	MaxRetries int           `json:"max_retries" env:"OPENSOURCE_MAX_RETRIES" default:"3"`
	BatchSize  int           `json:"batch_size" env:"OPENSOURCE_BATCH_SIZE" default:"1"`

	// Streaming configuration
	EnableStreaming bool          `json:"enable_streaming" env:"OPENSOURCE_ENABLE_STREAMING" default:"false"`
	ChunkDuration   time.Duration `json:"chunk_duration" env:"OPENSOURCE_CHUNK_DURATION" default:"5s"`

	// CLI-specific settings (for local model execution)
	ExecutablePath string   `json:"executable_path" env:"OPENSOURCE_EXECUTABLE_PATH"`
	ExtraArgs      []string `json:"extra_args"`

	// Model-specific options (passed to the inference backend)
	Options map[string]interface{} `json:"options"`
}

// ResourceConfig holds resource limitation configurations
type ResourceConfig struct {
	// Maximum concurrent calls
	MaxConcurrentCalls int `json:"max_concurrent_calls" env:"MAX_CONCURRENT_CALLS" default:"500"`
}

// LoggingConfig holds logging-related configurations
type LoggingConfig struct {
	// Log level
	Level string `json:"level" env:"LOG_LEVEL" default:"info"`

	// Log format (json or text)
	Format string `json:"format" env:"LOG_FORMAT" default:"json"`

	// Log output file (empty = stdout)
	OutputFile string `json:"output_file" env:"LOG_OUTPUT_FILE"`
}

// TracingConfig holds OpenTelemetry tracing configuration
type TracingConfig struct {
	Enabled     bool    `json:"enabled" env:"OTEL_TRACING_ENABLED" default:"false"`
	Endpoint    string  `json:"endpoint" env:"OTEL_EXPORTER_OTLP_ENDPOINT"`
	Insecure    bool    `json:"insecure" env:"OTEL_EXPORTER_OTLP_INSECURE" default:"false"`
	ServiceName string  `json:"service_name" env:"OTEL_SERVICE_NAME" default:"siprec-server"`
	SampleRatio float64 `json:"sample_ratio" env:"OTEL_TRACES_SAMPLER_RATIO" default:"1.0"`
}

// MessagingConfig holds messaging-related configurations
type MessagingConfig struct {
	// Basic AMQP configuration
	AMQPUrl       string `json:"amqp_url" env:"AMQP_URL"`
	AMQPQueueName string `json:"amqp_queue_name" env:"AMQP_QUEUE_NAME"`

	// AMQP Connection Pool Configuration
	AMQP AMQPConfig `json:"amqp"`

	// Real-time AMQP configuration
	EnableRealtimeAMQP   bool   `json:"enable_realtime_amqp" env:"ENABLE_REALTIME_AMQP" default:"false"`
	RealtimeQueueName    string `json:"realtime_queue_name" env:"REALTIME_QUEUE_NAME" default:"siprec_realtime"`
	RealtimeExchangeName string `json:"realtime_exchange_name" env:"REALTIME_EXCHANGE_NAME" default:""`
	RealtimeRoutingKey   string `json:"realtime_routing_key" env:"REALTIME_ROUTING_KEY" default:"siprec.realtime"`

	// Real-time AMQP batching
	RealtimeBatchSize    int           `json:"realtime_batch_size" env:"REALTIME_BATCH_SIZE" default:"10"`
	RealtimeBatchTimeout time.Duration `json:"realtime_batch_timeout" env:"REALTIME_BATCH_TIMEOUT" default:"1s"`
	RealtimeQueueSize    int           `json:"realtime_queue_size" env:"REALTIME_QUEUE_SIZE" default:"1000"`

	// Real-time event filtering
	PublishPartialTranscripts bool `json:"publish_partial_transcripts" env:"PUBLISH_PARTIAL_TRANSCRIPTS" default:"true"`
	PublishFinalTranscripts   bool `json:"publish_final_transcripts" env:"PUBLISH_FINAL_TRANSCRIPTS" default:"true"`
	PublishSentimentUpdates   bool `json:"publish_sentiment_updates" env:"PUBLISH_SENTIMENT_UPDATES" default:"true"`
	PublishKeywordDetections  bool `json:"publish_keyword_detections" env:"PUBLISH_KEYWORD_DETECTIONS" default:"true"`
	PublishSpeakerChanges     bool `json:"publish_speaker_changes" env:"PUBLISH_SPEAKER_CHANGES" default:"true"`

	// Additional real-time AMQP fan-out targets
	RealtimeEndpoints []RealtimeAMQPEndpointConfig `json:"realtime_amqp_endpoints"`
}

// AMQPConfig holds comprehensive AMQP configuration
type AMQPConfig struct {
	// Connection Configuration
	Hosts             []string      `json:"hosts" env:"AMQP_HOSTS" default:"localhost:5672"`
	Username          string        `json:"username" env:"AMQP_USERNAME" default:"guest"`
	Password          string        `json:"password" env:"AMQP_PASSWORD" default:"guest"`
	VirtualHost       string        `json:"virtual_host" env:"AMQP_VHOST" default:"/"`
	ConnectionTimeout time.Duration `json:"connection_timeout" env:"AMQP_CONNECTION_TIMEOUT" default:"30s"`
	Heartbeat         time.Duration `json:"heartbeat" env:"AMQP_HEARTBEAT" default:"10s"`

	// Connection Pool Configuration
	MaxConnections     int           `json:"max_connections" env:"AMQP_MAX_CONNECTIONS" default:"10"`
	MaxChannelsPerConn int           `json:"max_channels_per_conn" env:"AMQP_MAX_CHANNELS_PER_CONN" default:"100"`
	ConnectionIdleTime time.Duration `json:"connection_idle_time" env:"AMQP_CONNECTION_IDLE_TIME" default:"5m"`

	// Load Balancing Configuration
	LoadBalancing AMQPLoadBalancingConfig `json:"load_balancing"`

	// Exchange Configuration
	Exchanges []AMQPExchangeConfig `json:"exchanges"`

	// Queue Configuration
	Queues []AMQPQueueConfig `json:"queues"`

	// Message Configuration
	DefaultExchange   string        `json:"default_exchange" env:"AMQP_DEFAULT_EXCHANGE" default:""`
	DefaultRoutingKey string        `json:"default_routing_key" env:"AMQP_DEFAULT_ROUTING_KEY"`
	MessageTTL        time.Duration `json:"message_ttl" env:"AMQP_MESSAGE_TTL" default:"24h"`
	PublishTimeout    time.Duration `json:"publish_timeout" env:"AMQP_PUBLISH_TIMEOUT" default:"5s"`
	PublishConfirm    bool          `json:"publish_confirm" env:"AMQP_PUBLISH_CONFIRM" default:"true"`

	// Dead Letter Configuration
	DeadLetterExchange   string        `json:"dead_letter_exchange" env:"AMQP_DLX" default:"siprec.dlx"`
	DeadLetterRoutingKey string        `json:"dead_letter_routing_key" env:"AMQP_DLX_ROUTING_KEY" default:"failed"`
	MaxRetries           int           `json:"max_retries" env:"AMQP_MAX_RETRIES" default:"3"`
	RetryDelay           time.Duration `json:"retry_delay" env:"AMQP_RETRY_DELAY" default:"30s"`

	// Quality of Service Configuration
	PrefetchCount int  `json:"prefetch_count" env:"AMQP_PREFETCH_COUNT" default:"10"`
	PrefetchSize  int  `json:"prefetch_size" env:"AMQP_PREFETCH_SIZE" default:"0"`
	GlobalQos     bool `json:"global_qos" env:"AMQP_GLOBAL_QOS" default:"false"`

	// Security Configuration
	TLS AMQPTLSConfig `json:"tls"`

	// Monitoring Configuration
	EnableMetrics   bool          `json:"enable_metrics" env:"AMQP_ENABLE_METRICS" default:"true"`
	MetricsInterval time.Duration `json:"metrics_interval" env:"AMQP_METRICS_INTERVAL" default:"30s"`

	// Reconnection Configuration
	ReconnectDelay       time.Duration `json:"reconnect_delay" env:"AMQP_RECONNECT_DELAY" default:"5s"`
	MaxReconnectDelay    time.Duration `json:"max_reconnect_delay" env:"AMQP_MAX_RECONNECT_DELAY" default:"30s"`
	ReconnectMultiplier  float64       `json:"reconnect_multiplier" env:"AMQP_RECONNECT_MULTIPLIER" default:"2.0"`
	MaxReconnectAttempts int           `json:"max_reconnect_attempts" env:"AMQP_MAX_RECONNECT_ATTEMPTS" default:"0"`
}

// AMQPLoadBalancingConfig holds load balancing configuration
type AMQPLoadBalancingConfig struct {
	Enabled     bool   `json:"enabled" env:"AMQP_LB_ENABLED" default:"true"`
	Strategy    string `json:"strategy" env:"AMQP_LB_STRATEGY" default:"round_robin"`
	HealthCheck bool   `json:"health_check" env:"AMQP_LB_HEALTH_CHECK" default:"true"`
}

// AMQPExchangeConfig holds exchange configuration
type AMQPExchangeConfig struct {
	Name       string                 `json:"name"`
	Type       string                 `json:"type" default:"direct"`
	Durable    bool                   `json:"durable" default:"true"`
	AutoDelete bool                   `json:"auto_delete" default:"false"`
	Internal   bool                   `json:"internal" default:"false"`
	NoWait     bool                   `json:"no_wait" default:"false"`
	Arguments  map[string]interface{} `json:"arguments"`
}

// AMQPQueueConfig holds queue configuration
type AMQPQueueConfig struct {
	Name       string                 `json:"name"`
	Durable    bool                   `json:"durable" default:"true"`
	AutoDelete bool                   `json:"auto_delete" default:"false"`
	Exclusive  bool                   `json:"exclusive" default:"false"`
	NoWait     bool                   `json:"no_wait" default:"false"`
	Arguments  map[string]interface{} `json:"arguments"`

	// Binding configuration
	Bindings []AMQPBindingConfig `json:"bindings"`
}

// AMQPBindingConfig holds queue binding configuration
type AMQPBindingConfig struct {
	Exchange   string                 `json:"exchange"`
	RoutingKey string                 `json:"routing_key"`
	NoWait     bool                   `json:"no_wait" default:"false"`
	Arguments  map[string]interface{} `json:"arguments"`
}

// AMQPTLSConfig holds TLS configuration for AMQP
type AMQPTLSConfig struct {
	Enabled    bool   `json:"enabled" env:"AMQP_TLS_ENABLED" default:"false"`
	CertFile   string `json:"cert_file" env:"AMQP_TLS_CERT_FILE"`
	KeyFile    string `json:"key_file" env:"AMQP_TLS_KEY_FILE"`
	CAFile     string `json:"ca_file" env:"AMQP_TLS_CA_FILE"`
	SkipVerify bool   `json:"skip_verify" env:"AMQP_TLS_SKIP_VERIFY" default:"false"`
}

// RealtimeAMQPEndpointConfig configures an additional AMQP sink for live transcriptions.
type RealtimeAMQPEndpointConfig struct {
	Name           string        `json:"name"`
	Enabled        bool          `json:"enabled" default:"true"`
	UseEnhanced    bool          `json:"use_enhanced" default:"true"`
	URL            string        `json:"url"`
	QueueName      string        `json:"queue_name"`
	ExchangeName   string        `json:"exchange_name"`
	RoutingKey     string        `json:"routing_key"`
	AMQP           AMQPConfig    `json:"amqp"`
	TLS            AMQPTLSConfig `json:"tls"`
	PublishPartial *bool         `json:"publish_partial"`
	PublishFinal   *bool         `json:"publish_final"`
}

// CircuitBreakerConfig holds circuit breaker configurations
type CircuitBreakerConfig struct {
	// Global circuit breaker settings
	Enabled bool `json:"enabled" env:"CIRCUIT_BREAKER_ENABLED" default:"true"`

	// STT circuit breaker settings
	STTFailureThreshold int64         `json:"stt_failure_threshold" env:"STT_CB_FAILURE_THRESHOLD" default:"3"`
	STTTimeout          time.Duration `json:"stt_timeout" env:"STT_CB_TIMEOUT" default:"30s"`
	STTRequestTimeout   time.Duration `json:"stt_request_timeout" env:"STT_CB_REQUEST_TIMEOUT" default:"45s"`

	// AMQP circuit breaker settings
	AMQPFailureThreshold int64         `json:"amqp_failure_threshold" env:"AMQP_CB_FAILURE_THRESHOLD" default:"5"`
	AMQPTimeout          time.Duration `json:"amqp_timeout" env:"AMQP_CB_TIMEOUT" default:"60s"`
	AMQPRequestTimeout   time.Duration `json:"amqp_request_timeout" env:"AMQP_CB_REQUEST_TIMEOUT" default:"10s"`

	// Redis circuit breaker settings
	RedisFailureThreshold int64         `json:"redis_failure_threshold" env:"REDIS_CB_FAILURE_THRESHOLD" default:"8"`
	RedisTimeout          time.Duration `json:"redis_timeout" env:"REDIS_CB_TIMEOUT" default:"20s"`
	RedisRequestTimeout   time.Duration `json:"redis_request_timeout" env:"REDIS_CB_REQUEST_TIMEOUT" default:"5s"`

	// HTTP circuit breaker settings
	HTTPFailureThreshold int64         `json:"http_failure_threshold" env:"HTTP_CB_FAILURE_THRESHOLD" default:"5"`
	HTTPTimeout          time.Duration `json:"http_timeout" env:"HTTP_CB_TIMEOUT" default:"45s"`
	HTTPRequestTimeout   time.Duration `json:"http_request_timeout" env:"HTTP_CB_REQUEST_TIMEOUT" default:"30s"`

	// Monitoring settings
	MonitoringEnabled  bool          `json:"monitoring_enabled" env:"CB_MONITORING_ENABLED" default:"true"`
	MonitoringInterval time.Duration `json:"monitoring_interval" env:"CB_MONITORING_INTERVAL" default:"30s"`
}

// RedundancyConfig holds session redundancy configurations
type RedundancyConfig struct {
	// Whether session redundancy is enabled
	Enabled bool `json:"enabled" env:"ENABLE_REDUNDANCY" default:"true"`

	// Session timeout
	SessionTimeout time.Duration `json:"session_timeout" env:"SESSION_TIMEOUT" default:"30s"`

	// Session check interval
	SessionCheckInterval time.Duration `json:"session_check_interval" env:"SESSION_CHECK_INTERVAL" default:"10s"`

	// Storage type for redundancy (memory, redis)
	StorageType string `json:"storage_type" env:"REDUNDANCY_STORAGE_TYPE" default:"memory"`
}

// EncryptionConfig holds the configuration for encryption features
type EncryptionConfig struct {
	// Enable/disable encryption
	EnableRecordingEncryption bool `json:"enable_recording_encryption" env:"ENABLE_RECORDING_ENCRYPTION" default:"false"`
	EnableMetadataEncryption  bool `json:"enable_metadata_encryption" env:"ENABLE_METADATA_ENCRYPTION" default:"false"`

	// Algorithm configuration
	Algorithm           string `json:"algorithm" env:"ENCRYPTION_ALGORITHM" default:"AES-256-GCM"`
	KeyDerivationMethod string `json:"key_derivation_method" env:"KEY_DERIVATION_METHOD" default:"PBKDF2"`

	// Key management
	MasterKeyPath       string        `json:"master_key_path" env:"MASTER_KEY_PATH" default:"./keys"`
	KeyRotationInterval time.Duration `json:"key_rotation_interval" env:"KEY_ROTATION_INTERVAL" default:"24h"`
	KeyBackupEnabled    bool          `json:"key_backup_enabled" env:"KEY_BACKUP_ENABLED" default:"true"`
	BackupPassword      string        `json:"backup_password" env:"ENCRYPTION_BACKUP_PASSWORD"`

	// Security parameters
	KeySize          int `json:"key_size" env:"ENCRYPTION_KEY_SIZE" default:"32"`
	NonceSize        int `json:"nonce_size" env:"ENCRYPTION_NONCE_SIZE" default:"12"`
	SaltSize         int `json:"salt_size" env:"ENCRYPTION_SALT_SIZE" default:"32"`
	PBKDF2Iterations int `json:"pbkdf2_iterations" env:"PBKDF2_ITERATIONS" default:"100000"`

	// Storage encryption
	EncryptionKeyStore string `json:"encryption_key_store" env:"ENCRYPTION_KEY_STORE" default:"memory"`
}

// AsyncSTTConfig holds async STT processing configurations
type AsyncSTTConfig struct {
	// Whether async STT is enabled
	Enabled bool `json:"enabled" env:"STT_ASYNC_ENABLED" default:"true"`

	// Worker configuration
	WorkerCount  int           `json:"worker_count" env:"STT_WORKER_COUNT" default:"3"`
	MaxRetries   int           `json:"max_retries" env:"STT_MAX_RETRIES" default:"3"`
	RetryBackoff time.Duration `json:"retry_backoff" env:"STT_RETRY_BACKOFF" default:"30s"`
	JobTimeout   time.Duration `json:"job_timeout" env:"STT_JOB_TIMEOUT" default:"300s"`

	// Queue configuration
	QueueBufferSize      int           `json:"queue_buffer_size" env:"STT_QUEUE_BUFFER_SIZE" default:"1000"`
	BatchSize            int           `json:"batch_size" env:"STT_BATCH_SIZE" default:"10"`
	BatchTimeout         time.Duration `json:"batch_timeout" env:"STT_BATCH_TIMEOUT" default:"60s"`
	EnablePrioritization bool          `json:"enable_prioritization" env:"STT_ENABLE_PRIORITIZATION" default:"true"`

	// Resource limits
	MaxConcurrentJobs int `json:"max_concurrent_jobs" env:"STT_MAX_CONCURRENT_JOBS" default:"50"`

	// Cleanup configuration
	CleanupInterval  time.Duration `json:"cleanup_interval" env:"STT_CLEANUP_INTERVAL" default:"300s"`
	JobRetentionTime time.Duration `json:"job_retention_time" env:"STT_JOB_RETENTION_TIME" default:"24h"`

	// Cost tracking
	EnableCostTracking bool `json:"enable_cost_tracking" env:"STT_ENABLE_COST_TRACKING" default:"true"`
}

// HotReloadConfig holds configuration hot-reload settings
type HotReloadConfig struct {
	// Whether hot-reload is enabled
	Enabled bool `json:"enabled" env:"CONFIG_HOTRELOAD_ENABLED" default:"true"`

	// Debounce time for configuration changes
	DebounceTime time.Duration `json:"debounce_time" env:"CONFIG_HOTRELOAD_DEBOUNCE" default:"2s"`

	// Maximum time allowed for reload operation
	MaxReloadTime time.Duration `json:"max_reload_time" env:"CONFIG_HOTRELOAD_MAX_TIME" default:"30s"`

	// Backup configuration
	BackupEnabled bool   `json:"backup_enabled" env:"CONFIG_BACKUP_ENABLED" default:"true"`
	BackupDir     string `json:"backup_dir" env:"CONFIG_BACKUP_DIR" default:"./config_backups"`
}

// PerformanceConfig holds performance monitoring and optimization settings
type PerformanceConfig struct {
	// Whether performance monitoring is enabled
	Enabled bool `json:"enabled" env:"PERFORMANCE_MONITORING_ENABLED" default:"true"`

	// Performance monitoring interval
	MonitorInterval time.Duration `json:"monitor_interval" env:"PERFORMANCE_MONITOR_INTERVAL" default:"30s"`

	// Memory management settings
	GCThresholdMB int64 `json:"gc_threshold_mb" env:"PERFORMANCE_GC_THRESHOLD_MB" default:"100"`
	MemoryLimitMB int64 `json:"memory_limit_mb" env:"PERFORMANCE_MEMORY_LIMIT_MB" default:"512"`

	// CPU monitoring
	CPULimit float64 `json:"cpu_limit" env:"PERFORMANCE_CPU_LIMIT" default:"80.0"`

	// Optimization settings
	EnableAutoGC    bool `json:"enable_auto_gc" env:"PERFORMANCE_ENABLE_AUTO_GC" default:"true"`
	GCTargetPercent int  `json:"gc_target_percent" env:"PERFORMANCE_GC_TARGET_PERCENT" default:"50"`
}

// PauseResumeConfig holds configuration for pause/resume functionality
type PauseResumeConfig struct {
	// Whether pause/resume API is enabled
	Enabled bool `json:"enabled" env:"PAUSE_RESUME_ENABLED" default:"false"`

	// Whether to pause recording when API is called
	PauseRecording bool `json:"pause_recording" env:"PAUSE_RECORDING" default:"true"`

	// Whether to pause transcription when API is called
	PauseTranscription bool `json:"pause_transcription" env:"PAUSE_TRANSCRIPTION" default:"true"`

	// Whether to send notification events when paused/resumed
	SendNotifications bool `json:"send_notifications" env:"PAUSE_RESUME_NOTIFICATIONS" default:"true"`

	// Maximum pause duration (0 = unlimited)
	MaxPauseDuration time.Duration `json:"max_pause_duration" env:"MAX_PAUSE_DURATION" default:"0"`

	// Whether to auto-resume after max duration
	AutoResume bool `json:"auto_resume" env:"PAUSE_AUTO_RESUME" default:"false"`

	// Whether to allow pause/resume per session or globally
	PerSession bool `json:"per_session" env:"PAUSE_RESUME_PER_SESSION" default:"true"`

	// API authentication required
	RequireAuth bool `json:"require_auth" env:"PAUSE_RESUME_REQUIRE_AUTH" default:"true"`

	// API key for authentication (if RequireAuth is true)
	APIKey string `json:"api_key" env:"PAUSE_RESUME_API_KEY"`
}

// AnalyticsConfig controls analytics persistence and exports
type AnalyticsConfig struct {
	Enabled bool `json:"enabled" env:"ANALYTICS_ENABLED" default:"false"`

	Elasticsearch struct {
		Addresses []string      `json:"addresses" env:"ELASTICSEARCH_ADDRESSES" default:"http://localhost:9200"`
		Index     string        `json:"index" env:"ELASTICSEARCH_INDEX" default:"call-analytics"`
		Username  string        `json:"username" env:"ELASTICSEARCH_USERNAME"`
		Password  string        `json:"password" env:"ELASTICSEARCH_PASSWORD"`
		Timeout   time.Duration `json:"timeout" env:"ELASTICSEARCH_TIMEOUT" default:"10s"`
	}
}

// PIIConfig holds configuration for PII (Personally Identifiable Information) detection
type PIIConfig struct {
	// Whether PII detection is enabled
	Enabled bool `json:"enabled" env:"PII_DETECTION_ENABLED" default:"false"`

	// Types of PII to detect (comma-separated: ssn,credit_card,phone,email)
	EnabledTypes []string `json:"enabled_types" env:"PII_ENABLED_TYPES" default:"ssn,credit_card"`

	// Character to use for redaction
	RedactionChar string `json:"redaction_char" env:"PII_REDACTION_CHAR" default:"*"`

	// Whether to preserve format in redaction (e.g., XXX-XX-1234)
	PreserveFormat bool `json:"preserve_format" env:"PII_PRESERVE_FORMAT" default:"true"`

	// Context length around detected PII for logging
	ContextLength int `json:"context_length" env:"PII_CONTEXT_LENGTH" default:"20"`

	// Whether to apply PII detection to transcriptions
	ApplyToTranscriptions bool `json:"apply_to_transcriptions" env:"PII_APPLY_TO_TRANSCRIPTIONS" default:"true"`

	// Whether to apply PII detection to audio recordings
	ApplyToRecordings bool `json:"apply_to_recordings" env:"PII_APPLY_TO_RECORDINGS" default:"false"`

	// Whether to apply PII detection to CDR fields (CallerID, CalleeID)
	ApplyToCDR bool `json:"apply_to_cdr" env:"PII_APPLY_TO_CDR" default:"false"`

	// Log level for PII detection events (debug, info, warn, error)
	LogLevel string `json:"log_level" env:"PII_LOG_LEVEL" default:"info"`
}

// AuthConfig holds authentication and authorization configuration
type AuthConfig struct {
	// Whether authentication is enabled
	Enabled bool `json:"enabled" env:"AUTH_ENABLED" default:"false"`

	// JWT secret key for token signing
	JWTSecret string `json:"jwt_secret" env:"AUTH_JWT_SECRET"`

	// JWT issuer
	JWTIssuer string `json:"jwt_issuer" env:"AUTH_JWT_ISSUER" default:"siprec-server"`

	// Token expiry duration
	TokenExpiry time.Duration `json:"token_expiry" env:"AUTH_TOKEN_EXPIRY" default:"24h"`

	// Whether API key authentication is enabled
	EnableAPIKeys bool `json:"enable_api_keys" env:"AUTH_ENABLE_API_KEYS" default:"true"`

	// Default admin username
	AdminUsername string `json:"admin_username" env:"AUTH_ADMIN_USERNAME" default:"admin"`

	// Default admin password (only used if not already set)
	AdminPassword string `json:"admin_password" env:"AUTH_ADMIN_PASSWORD"`

	// SIP Authentication configuration
	SIP SIPAuthConfig `json:"sip"`
}

// SIPAuthConfig holds SIP-specific authentication configuration
type SIPAuthConfig struct {
	// Whether SIP authentication is enabled
	Enabled bool `json:"enabled" env:"SIP_AUTH_ENABLED" default:"false"`

	// Authentication realm (typically the domain)
	Realm string `json:"realm" env:"SIP_AUTH_REALM" default:"siprec.local"`

	// Nonce timeout in seconds
	NonceTimeout int `json:"nonce_timeout" env:"SIP_AUTH_NONCE_TIMEOUT" default:"300"`

	// SIP users for digest authentication (format: "user1:pass1,user2:pass2")
	Users string `json:"users" env:"SIP_AUTH_USERS"`

	// IP-based access control
	IPAccess SIPIPAccessConfig `json:"ip_access"`
}

// SIPIPAccessConfig holds IP-based access control configuration
type SIPIPAccessConfig struct {
	// Whether IP-based access control is enabled
	Enabled bool `json:"enabled" env:"SIP_IP_ACCESS_ENABLED" default:"false"`

	// Default policy: true = allow all except blocked, false = block all except allowed
	DefaultAllow bool `json:"default_allow" env:"SIP_IP_DEFAULT_ALLOW" default:"true"`

	// Comma-separated list of allowed IP addresses
	AllowedIPs string `json:"allowed_ips" env:"SIP_IP_ALLOWED_IPS"`

	// Comma-separated list of allowed networks (CIDR notation)
	AllowedNetworks string `json:"allowed_networks" env:"SIP_IP_ALLOWED_NETWORKS"`

	// Comma-separated list of blocked IP addresses
	BlockedIPs string `json:"blocked_ips" env:"SIP_IP_BLOCKED_IPS"`

	// Comma-separated list of blocked networks (CIDR notation)
	BlockedNetworks string `json:"blocked_networks" env:"SIP_IP_BLOCKED_NETWORKS"`
}

// AlertingConfig holds alerting system configuration
type AlertingConfig struct {
	// Whether alerting is enabled
	Enabled bool `json:"enabled" env:"ALERTING_ENABLED" default:"false"`

	// Alert evaluation interval
	EvaluationInterval time.Duration `json:"evaluation_interval" env:"ALERTING_EVALUATION_INTERVAL" default:"30s"`
}

// RateLimitConfig holds rate limiting configuration
type RateLimitConfig struct {
	// Whether HTTP rate limiting is enabled
	Enabled bool `json:"enabled" env:"RATE_LIMIT_ENABLED" default:"false"`

	// RequestsPerSecond is the sustained rate of HTTP requests allowed per second per client
	RequestsPerSecond float64 `json:"requests_per_second" env:"RATE_LIMIT_RPS" default:"100"`

	// BurstSize is the maximum number of HTTP requests allowed in a burst
	BurstSize int `json:"burst_size" env:"RATE_LIMIT_BURST" default:"200"`

	// BlockDuration is how long to block a client after exceeding limits
	BlockDuration time.Duration `json:"block_duration" env:"RATE_LIMIT_BLOCK_DURATION" default:"1m"`

	// WhitelistedIPs is a comma-separated list of IPs/CIDRs that bypass rate limiting
	WhitelistedIPs string `json:"whitelisted_ips" env:"RATE_LIMIT_WHITELIST_IPS"`

	// WhitelistedPaths is a comma-separated list of URL paths that bypass rate limiting
	WhitelistedPaths string `json:"whitelisted_paths" env:"RATE_LIMIT_WHITELIST_PATHS"`

	// SIP-specific rate limiting settings
	SIPEnabled           bool    `json:"sip_enabled" env:"RATE_LIMIT_SIP_ENABLED" default:"false"`
	SIPInvitesPerSecond  float64 `json:"sip_invites_per_second" env:"RATE_LIMIT_SIP_INVITE_RPS" default:"10"`
	SIPInviteBurst       int     `json:"sip_invite_burst" env:"RATE_LIMIT_SIP_INVITE_BURST" default:"50"`
	SIPRequestsPerSecond float64 `json:"sip_requests_per_second" env:"RATE_LIMIT_SIP_RPS" default:"100"`
	SIPRequestBurst      int     `json:"sip_request_burst" env:"RATE_LIMIT_SIP_REQUEST_BURST" default:"200"`
}

// Load loads the configuration from environment variables or .env file
func Load(logger *logrus.Logger) (*Config, error) {
	// Get current working directory
	wd, err := os.Getwd()
	if err != nil {
		logger.WithError(err).Warn("Failed to get current working directory")
		wd = "unknown"
	}

	// Define possible locations for .env file
	possibleEnvFiles := []string{
		".env",                    // Current directory
		"../.env",                 // Parent directory
		filepath.Join(wd, ".env"), // Absolute path
	}

	// Try loading .env file from each possible location
	var loadedFrom string
	var loadErr error

	for _, envFile := range possibleEnvFiles {
		// Try to load this .env file
		if _, statErr := os.Stat(envFile); statErr == nil {
			absPath, _ := filepath.Abs(envFile)
			logger.WithField("path", absPath).Debug("Attempting to load .env file")

			if loadErr = godotenv.Load(envFile); loadErr == nil {
				loadedFrom = absPath
				break
			}
		}
	}

	// If all attempts failed, try default Load() which uses working directory
	if loadedFrom == "" {
		if loadErr = godotenv.Load(); loadErr == nil {
			if _, statErr := os.Stat(".env"); statErr == nil {
				loadedFrom, _ = filepath.Abs(".env")
			}
		}
	}

	// Report results
	if loadedFrom != "" {
		logger.WithFields(logrus.Fields{
			"working_dir": wd,
			"path":        loadedFrom,
		}).Info("Successfully loaded .env file")
	} else {
		logger.WithField("working_dir", wd).Warn("No .env file found, using environment variables only")
	}

	// Initialize config with default values
	config := &Config{}

	// Load network configuration
	if err := loadNetworkConfig(logger, &config.Network); err != nil {
		return nil, errors.Wrap(err, "failed to load network configuration")
	}

	// Load HTTP configuration
	if err := loadHTTPConfig(logger, &config.HTTP); err != nil {
		return nil, errors.Wrap(err, "failed to load HTTP configuration")
	}

	// Load recording configuration
	if err := loadRecordingConfig(logger, &config.Recording); err != nil {
		return nil, errors.Wrap(err, "failed to load recording configuration")
	}

	// Load STT configuration
	if err := loadSTTConfig(logger, &config.STT); err != nil {
		return nil, errors.Wrap(err, "failed to load STT configuration")
	}

	// Load resource configuration
	if err := loadResourceConfig(logger, &config.Resources); err != nil {
		return nil, errors.Wrap(err, "failed to load resource configuration")
	}

	// Load logging configuration
	if err := loadLoggingConfig(logger, &config.Logging); err != nil {
		return nil, errors.Wrap(err, "failed to load logging configuration")
	}

	// Load messaging configuration
	if err := loadMessagingConfig(logger, &config.Messaging); err != nil {
		return nil, errors.Wrap(err, "failed to load messaging configuration")
	}

	// Load redundancy configuration
	if err := loadRedundancyConfig(logger, &config.Redundancy); err != nil {
		return nil, errors.Wrap(err, "failed to load redundancy configuration")
	}

	// Load encryption configuration
	if err := loadEncryptionConfig(logger, &config.Encryption); err != nil {
		return nil, errors.Wrap(err, "failed to load encryption configuration")
	}

	// Load async STT configuration
	if err := loadAsyncSTTConfig(logger, &config.AsyncSTT); err != nil {
		return nil, errors.Wrap(err, "failed to load async STT configuration")
	}

	// Load hot-reload configuration
	if err := loadHotReloadConfig(logger, &config.HotReload); err != nil {
		return nil, errors.Wrap(err, "failed to load hot-reload configuration")
	}

	// Load performance configuration
	if err := loadPerformanceConfig(logger, &config.Performance); err != nil {
		return nil, errors.Wrap(err, "failed to load performance configuration")
	}

	// Load circuit breaker configuration
	if err := loadCircuitBreakerConfig(logger, &config.CircuitBreaker); err != nil {
		return nil, errors.Wrap(err, "failed to load circuit breaker configuration")
	}

	// Load pause/resume configuration
	if err := loadPauseResumeConfig(logger, &config.PauseResume); err != nil {
		return nil, errors.Wrap(err, "failed to load pause/resume configuration")
	}

	// Load PII detection configuration
	if err := loadPIIConfig(logger, &config.PII); err != nil {
		return nil, errors.Wrap(err, "failed to load PII detection configuration")
	}

	// Load analytics configuration
	if err := loadAnalyticsConfig(logger, &config.Analytics); err != nil {
		return nil, errors.Wrap(err, "failed to load analytics configuration")
	}

	// Load database configuration
	if err := loadDatabaseConfig(logger, &config.Database); err != nil {
		return nil, errors.Wrap(err, "failed to load database configuration")
	}

	// Load compliance configuration
	if err := loadComplianceConfig(logger, &config.Compliance); err != nil {
		return nil, errors.Wrap(err, "failed to load compliance configuration")
	}

	// Load authentication configuration
	if err := loadAuthConfig(logger, &config.Auth); err != nil {
		return nil, errors.Wrap(err, "failed to load authentication configuration")
	}

	// Load alerting configuration
	if err := loadAlertingConfig(logger, &config.Alerting); err != nil {
		return nil, errors.Wrap(err, "failed to load alerting configuration")
	}

	// Load rate limiting configuration
	if err := loadRateLimitConfig(logger, &config.RateLimit); err != nil {
		return nil, errors.Wrap(err, "failed to load rate limit configuration")
	}

	// Validate the complete configuration
	if err := validateConfig(logger, config); err != nil {
		return nil, errors.Wrap(err, "configuration validation failed")
	}

	// Ensure required directories exist
	if err := ensureDirectories(logger, config); err != nil {
		return nil, errors.Wrap(err, "failed to create required directories")
	}

	return config, nil
}

// loadNetworkConfig loads the network configuration section
func loadNetworkConfig(logger *logrus.Logger, config *NetworkConfig) error {
	// Load external IP
	config.ExternalIP = getEnv("EXTERNAL_IP", "auto")
	if config.ExternalIP == "auto" {
		// Auto-detect external IP
		config.ExternalIP = getExternalIP(logger)
		logger.WithField("external_ip", config.ExternalIP).Info("Auto-detected external IP")
	}

	// Load internal IP
	config.InternalIP = getEnv("INTERNAL_IP", "auto")
	if config.InternalIP == "auto" {
		// Auto-detect internal IP
		config.InternalIP = getInternalIP(logger)
		logger.WithField("internal_ip", config.InternalIP).Info("Auto-detected internal IP")
	}

	// Load SIP host (bind address)
	config.Host = getEnv("SIP_HOST", "0.0.0.0")

	// Load SIP ports
	var err error
	config.Ports, err = parsePorts(getEnv("PORTS", "5060,5061"), "PORTS")
	if err != nil {
		return err
	}
	logger.WithField("sip_ports", config.Ports).Info("Configured SIP ports")

	// Load UDP-specific ports (optional)
	if udpPortsStr := getEnv("UDP_PORTS", ""); udpPortsStr != "" {
		config.UDPPorts, err = parsePorts(udpPortsStr, "UDP_PORTS")
		if err != nil {
			return err
		}
		logger.WithField("udp_ports", config.UDPPorts).Info("Configured UDP-specific ports")
	}

	// Load TCP-specific ports (optional)
	if tcpPortsStr := getEnv("TCP_PORTS", ""); tcpPortsStr != "" {
		config.TCPPorts, err = parsePorts(tcpPortsStr, "TCP_PORTS")
		if err != nil {
			return err
		}
		logger.WithField("tcp_ports", config.TCPPorts).Info("Configured TCP-specific ports")
	}

	// If no valid ports were specified, use defaults
	if len(config.Ports) == 0 {
		config.Ports = []int{5060, 5061}
		logger.Warn("No valid ports specified, using defaults: 5060, 5061")
	} else {
		logger.WithField("sip_ports", config.Ports).Info("Configured SIP ports")
	}

	// Load RTP port range
	rtpMinStr := getEnv("RTP_PORT_MIN", "10000")
	rtpMin, err := strconv.Atoi(rtpMinStr)
	if err != nil || rtpMin < 1024 || rtpMin > 65000 {
		logger.Warn("Invalid RTP_PORT_MIN value, using default: 10000")
		config.RTPPortMin = 10000
	} else {
		config.RTPPortMin = rtpMin
	}

	rtpMaxStr := getEnv("RTP_PORT_MAX", "20000")
	rtpMax, err := strconv.Atoi(rtpMaxStr)
	if err != nil || rtpMax <= config.RTPPortMin || rtpMax > 65535 {
		logger.Warn("Invalid RTP_PORT_MAX value, using default: 20000")
		config.RTPPortMax = 20000
	} else {
		config.RTPPortMax = rtpMax
	}

	// Ensure there are enough ports in the range
	if (config.RTPPortMax - config.RTPPortMin) < 100 {
		logger.Warn("RTP port range too small, at least 100 ports are recommended")
	}

	// Load RTP timeout duration
	rtpTimeoutStr := getEnv("RTP_TIMEOUT", "30s")
	rtpTimeout, err := time.ParseDuration(rtpTimeoutStr)
	if err != nil || rtpTimeout <= 0 {
		logger.WithError(err).Warn("Invalid RTP_TIMEOUT value, defaulting to 30s")
		config.RTPTimeout = 30 * time.Second
	} else {
		config.RTPTimeout = rtpTimeout
	}
	logger.WithField("rtp_timeout", config.RTPTimeout).Info("Configured RTP timeout")

	// Load RTP bind IP address (optional - defaults to all interfaces)
	config.RTPBindIP = getEnv("RTP_BIND_IP", "")
	if config.RTPBindIP != "" {
		// Validate that it's a valid IP address
		if ip := net.ParseIP(config.RTPBindIP); ip == nil {
			logger.WithField("rtp_bind_ip", config.RTPBindIP).Warn("Invalid RTP_BIND_IP, binding to all interfaces instead")
			config.RTPBindIP = ""
		} else {
			logger.WithField("rtp_bind_ip", config.RTPBindIP).Info("RTP listener will bind to specific interface")
		}
	} else {
		logger.Info("RTP listener will bind to all interfaces (0.0.0.0)")
	}

	// Load TLS configuration
	config.TLSCertFile = getEnv("TLS_CERT_PATH", "")
	config.TLSKeyFile = getEnv("TLS_KEY_PATH", "")

	tlsPortStr := getEnv("TLS_PORT", "5062")
	tlsPort, err := strconv.Atoi(tlsPortStr)
	if err != nil || tlsPort < 1 || tlsPort > 65535 {
		logger.Warn("Invalid TLS_PORT value, using default: 5062")
		config.TLSPort = 5062
	} else {
		config.TLSPort = tlsPort
	}

	// Load feature flags
	config.EnableTLS = getEnvBool("ENABLE_TLS", false)
	config.EnableSRTP = getEnvBool("ENABLE_SRTP", false)
	config.BehindNAT = getEnvBool("BEHIND_NAT", false)
	config.RequireTLSOnly = getEnvBool("SIP_REQUIRE_TLS", false)
	config.RequireSRTP = getEnvBool("SIP_REQUIRE_SRTP", false)

	// If TLS is enabled, ensure certificates are provided
	if config.EnableTLS && (config.TLSCertFile == "" || config.TLSKeyFile == "") {
		return errors.New("TLS is enabled but certificate or key file is missing. Please provide both TLS_CERT_PATH and TLS_KEY_PATH environment variables")
	}

	if config.RequireSRTP && !config.EnableSRTP {
		logger.Info("SIP_REQUIRE_SRTP enabled; enabling SRTP")
		config.EnableSRTP = true
	}

	// Load STUN servers
	stunServersStr := getEnv("STUN_SERVER", "")
	if stunServersStr == "" {
		// Default Google STUN servers
		config.STUNServers = []string{
			"stun.l.google.com:19302",
			"stun1.l.google.com:19302",
			"stun2.l.google.com:19302",
			"stun3.l.google.com:19302",
			"stun4.l.google.com:19302",
		}
		logger.Info("Using default Google STUN servers")
	} else {
		config.STUNServers = strings.Split(stunServersStr, ",")
		for i, server := range config.STUNServers {
			config.STUNServers[i] = strings.TrimSpace(server)
		}
	}

	return nil
}

// loadHTTPConfig loads the HTTP server configuration section
func loadHTTPConfig(logger *logrus.Logger, config *HTTPConfig) error {
	// Load HTTP port
	httpPortStr := getEnv("HTTP_PORT", "8080")
	httpPort, err := strconv.Atoi(httpPortStr)
	if err != nil || httpPort < 1 || httpPort > 65535 {
		logger.Warn("Invalid HTTP_PORT value, using default: 8080")
		config.Port = 8080
	} else {
		config.Port = httpPort
	}

	// Load feature flags
	config.Enabled = getEnvBool("HTTP_ENABLED", true)
	config.EnableMetrics = getEnvBool("HTTP_ENABLE_METRICS", true)
	config.EnableAPI = getEnvBool("HTTP_ENABLE_API", true)

	// Load timeouts
	readTimeoutStr := getEnv("HTTP_READ_TIMEOUT", "10s")
	readTimeout, err := time.ParseDuration(readTimeoutStr)
	if err != nil {
		logger.Warn("Invalid HTTP_READ_TIMEOUT value, using default: 10s")
		config.ReadTimeout = 10 * time.Second
	} else {
		config.ReadTimeout = readTimeout
	}

	writeTimeoutStr := getEnv("HTTP_WRITE_TIMEOUT", "30s")
	writeTimeout, err := time.ParseDuration(writeTimeoutStr)
	if err != nil {
		logger.Warn("Invalid HTTP_WRITE_TIMEOUT value, using default: 30s")
		config.WriteTimeout = 30 * time.Second
	} else {
		config.WriteTimeout = writeTimeout
	}

	config.TLSEnabled = getEnvBool("HTTP_TLS_ENABLED", false)
	config.TLSCertFile = getEnv("HTTP_TLS_CERT_FILE", "")
	config.TLSKeyFile = getEnv("HTTP_TLS_KEY_FILE", "")

	return nil
}

// loadRecordingConfig loads the recording configuration section
func loadRecordingConfig(logger *logrus.Logger, config *RecordingConfig) error {
	// Load recording directory
	config.Directory = getEnv("RECORDING_DIR", "./recordings")

	// Load recording max duration
	maxDurationStr := getEnv("RECORDING_MAX_DURATION_HOURS", "4")
	maxDuration, err := strconv.Atoi(maxDurationStr)
	if err != nil || maxDuration < 1 {
		logger.Warn("Invalid RECORDING_MAX_DURATION_HOURS value, using default: 4 hours")
		config.MaxDuration = 4 * time.Hour
	} else {
		config.MaxDuration = time.Duration(maxDuration) * time.Hour
	}

	// Load recording cleanup days
	cleanupDaysStr := getEnv("RECORDING_CLEANUP_DAYS", "30")
	cleanupDays, err := strconv.Atoi(cleanupDaysStr)
	if err != nil || cleanupDays < 1 {
		logger.Warn("Invalid RECORDING_CLEANUP_DAYS value, using default: 30 days")
		config.CleanupDays = 30
	} else {
		config.CleanupDays = cleanupDays
	}

	// Load recording storage configuration
	config.Storage.Enabled = getEnvBool("RECORDING_STORAGE_ENABLED", false)
	config.Storage.KeepLocal = getEnvBool("RECORDING_STORAGE_KEEP_LOCAL", true)

	config.Storage.S3.Enabled = getEnvBool("RECORDING_STORAGE_S3_ENABLED", false)
	config.Storage.S3.Bucket = getEnv("RECORDING_STORAGE_S3_BUCKET", "")
	config.Storage.S3.Region = getEnv("RECORDING_STORAGE_S3_REGION", "")
	config.Storage.S3.AccessKey = getEnv("RECORDING_STORAGE_S3_ACCESS_KEY", "")
	config.Storage.S3.SecretKey = getEnv("RECORDING_STORAGE_S3_SECRET_KEY", "")
	config.Storage.S3.Prefix = getEnv("RECORDING_STORAGE_S3_PREFIX", "")

	config.Storage.GCS.Enabled = getEnvBool("RECORDING_STORAGE_GCS_ENABLED", false)
	config.Storage.GCS.Bucket = getEnv("RECORDING_STORAGE_GCS_BUCKET", "")
	config.Storage.GCS.ServiceAccountKey = getEnv("RECORDING_STORAGE_GCS_SERVICE_ACCOUNT", "")
	config.Storage.GCS.Prefix = getEnv("RECORDING_STORAGE_GCS_PREFIX", "")

	config.Storage.Azure.Enabled = getEnvBool("RECORDING_STORAGE_AZURE_ENABLED", false)
	config.Storage.Azure.Account = getEnv("RECORDING_STORAGE_AZURE_ACCOUNT", "")
	config.Storage.Azure.Container = getEnv("RECORDING_STORAGE_AZURE_CONTAINER", "")
	config.Storage.Azure.AccessKey = getEnv("RECORDING_STORAGE_AZURE_ACCESS_KEY", "")
	config.Storage.Azure.Prefix = getEnv("RECORDING_STORAGE_AZURE_PREFIX", "")

	if config.Storage.Enabled {
		logger.WithFields(logrus.Fields{
			"keep_local": config.Storage.KeepLocal,
			"s3":         config.Storage.S3.Enabled,
			"gcs":        config.Storage.GCS.Enabled,
			"azure":      config.Storage.Azure.Enabled,
		}).Info("Recording storage enabled")
	}

	// Whether to merge SIPREC legs into a single WAV
	config.CombineLegs = getEnvBool("RECORDING_COMBINE_LEGS", true)

	// Load recording format settings
	config.Format = strings.ToLower(getEnv("RECORDING_FORMAT", "wav"))
	if config.Format != "wav" && config.Format != "mp3" && config.Format != "opus" &&
		config.Format != "ogg" && config.Format != "mp4" && config.Format != "m4a" && config.Format != "flac" {
		logger.WithField("format", config.Format).Warn("Invalid RECORDING_FORMAT, defaulting to wav")
		config.Format = "wav"
	}

	// Load MP3 bitrate (kbps)
	mp3BitrateStr := getEnv("RECORDING_MP3_BITRATE", "128")
	mp3Bitrate, err := strconv.Atoi(mp3BitrateStr)
	if err != nil || mp3Bitrate < 32 || mp3Bitrate > 320 {
		config.MP3Bitrate = 128
	} else {
		config.MP3Bitrate = mp3Bitrate
	}

	// Load Opus bitrate (kbps)
	opusBitrateStr := getEnv("RECORDING_OPUS_BITRATE", "64")
	opusBitrate, err := strconv.Atoi(opusBitrateStr)
	if err != nil || opusBitrate < 6 || opusBitrate > 510 {
		config.OpusBitrate = 64
	} else {
		config.OpusBitrate = opusBitrate
	}

	// Load quality setting (1-10)
	qualityStr := getEnv("RECORDING_QUALITY", "5")
	quality, err := strconv.Atoi(qualityStr)
	if err != nil || quality < 1 || quality > 10 {
		config.Quality = 5
	} else {
		config.Quality = quality
	}

	if config.Format != "wav" {
		logger.WithFields(logrus.Fields{
			"format":       config.Format,
			"mp3_bitrate":  config.MP3Bitrate,
			"opus_bitrate": config.OpusBitrate,
			"quality":      config.Quality,
		}).Info("Recording format encoding configured")
	}

	return nil
}

// loadSTTConfig loads the speech-to-text configuration section
func loadSTTConfig(logger *logrus.Logger, config *STTConfig) error {
	// Load supported vendors - check both STT_VENDORS and SUPPORTED_VENDORS for compatibility
	vendorsStr := getEnv("STT_VENDORS", "")
	if vendorsStr == "" {
		vendorsStr = getEnv("SUPPORTED_VENDORS", "google,deepgram,elevenlabs,speechmatics,openai")
	}
	if vendorsStr == "" {
		config.SupportedVendors = []string{"google", "openai"}
		logger.Info("No STT vendors specified, defaulting to: google, openai")
	} else {
		vendors := strings.Split(vendorsStr, ",")
		for i, vendor := range vendors {
			vendors[i] = strings.TrimSpace(vendor)
		}
		config.SupportedVendors = vendors
		logger.WithField("vendors", config.SupportedVendors).Info("Configured STT vendors")
	}

	// Load supported codecs - check both SUPPORTED_CODECS and STT_SUPPORTED_CODECS
	codecsStr := getEnv("STT_SUPPORTED_CODECS", "")
	if codecsStr == "" {
		codecsStr = getEnv("SUPPORTED_CODECS", "PCMU,PCMA,G722,OPUS")
	}
	if codecsStr == "" {
		config.SupportedCodecs = []string{"PCMU", "PCMA", "G722", "OPUS"}
		logger.Info("No codecs specified, defaulting to: PCMU, PCMA, G722, OPUS")
	} else {
		codecs := strings.Split(codecsStr, ",")
		for i, codec := range codecs {
			codecs[i] = strings.TrimSpace(codec)
		}
		config.SupportedCodecs = codecs
		logger.WithField("codecs", config.SupportedCodecs).Info("Configured supported codecs")
	}

	// Load default vendor - check both STT_DEFAULT_VENDOR and DEFAULT_SPEECH_VENDOR for compatibility
	config.DefaultVendor = getEnv("STT_DEFAULT_VENDOR", "")
	if config.DefaultVendor == "" {
		config.DefaultVendor = getEnv("DEFAULT_SPEECH_VENDOR", "google")
	}
	if config.DefaultVendor == "" {
		logger.Warn("STT_DEFAULT_VENDOR not set, using default: google")
		config.DefaultVendor = "google"
	}

	// Validate that the default vendor is in the supported vendors list
	found := false
	for _, vendor := range config.SupportedVendors {
		if vendor == config.DefaultVendor {
			found = true
			break
		}
	}

	if !found {
		logger.Warnf("Default vendor '%s' is not in the supported vendors list, adding it", config.DefaultVendor)
		config.SupportedVendors = append(config.SupportedVendors, config.DefaultVendor)
	}

	// Load provider-specific configurations
	if err := loadGoogleSTTConfig(logger, &config.Google); err != nil {
		return fmt.Errorf("failed to load Google STT config: %w", err)
	}

	if err := loadDeepgramSTTConfig(logger, &config.Deepgram); err != nil {
		return fmt.Errorf("failed to load Deepgram STT config: %w", err)
	}

	if err := loadAzureSTTConfig(logger, &config.Azure); err != nil {
		return fmt.Errorf("failed to load Azure STT config: %w", err)
	}

	if err := loadAmazonSTTConfig(logger, &config.Amazon); err != nil {
		return fmt.Errorf("failed to load Amazon STT config: %w", err)
	}

	if err := loadOpenAISTTConfig(logger, &config.OpenAI); err != nil {
		return fmt.Errorf("failed to load OpenAI STT config: %w", err)
	}

	if err := loadWhisperSTTConfig(logger, &config.Whisper); err != nil {
		return fmt.Errorf("failed to load Whisper STT config: %w", err)
	}

	if err := loadElevenLabsSTTConfig(logger, &config.ElevenLabs); err != nil {
		return fmt.Errorf("failed to load ElevenLabs STT config: %w", err)
	}

	if err := loadSpeechmaticsSTTConfig(logger, &config.Speechmatics); err != nil {
		return fmt.Errorf("failed to load Speechmatics STT config: %w", err)
	}

	if err := loadOpenSourceSTTConfig(logger, &config.OpenSource); err != nil {
		return fmt.Errorf("failed to load open-source STT config: %w", err)
	}

	// Load language routing mapping (e.g., "en-US:google,es-ES:deepgram")
	routingEnv := getEnv("LANGUAGE_ROUTING", "")
	config.LanguageRouting = make(map[string]string)
	if routingEnv != "" {
		routes := strings.Split(routingEnv, ",")
		for _, route := range routes {
			parts := strings.SplitN(strings.TrimSpace(route), ":", 2)
			if len(parts) != 2 {
				logger.WithField("entry", route).Warn("Invalid LANGUAGE_ROUTING entry, expected language:provider")
				continue
			}
			lang := strings.ToLower(strings.TrimSpace(parts[0]))
			provider := strings.TrimSpace(parts[1])
			if lang == "" || provider == "" {
				logger.WithField("entry", route).Warn("Invalid LANGUAGE_ROUTING entry, language or provider empty")
				continue
			}
			config.LanguageRouting[lang] = provider
		}
		if len(config.LanguageRouting) > 0 {
			logger.WithField("language_routing", config.LanguageRouting).Info("Configured STT language routing")
		}
	}

	return nil
}

// loadResourceConfig loads the resource configuration section
func loadResourceConfig(logger *logrus.Logger, config *ResourceConfig) error {
	// Load max concurrent calls
	maxCallsStr := getEnv("MAX_CONCURRENT_CALLS", "500")
	maxCalls, err := strconv.Atoi(maxCallsStr)
	if err != nil || maxCalls < 1 {
		logger.Warn("Invalid MAX_CONCURRENT_CALLS value, using default: 500")
		config.MaxConcurrentCalls = 500
	} else {
		config.MaxConcurrentCalls = maxCalls
	}

	return nil
}

// loadLoggingConfig loads the logging configuration section
func loadLoggingConfig(logger *logrus.Logger, config *LoggingConfig) error {
	// Load log level
	config.Level = getEnv("LOG_LEVEL", "info")

	// Validate log level
	_, err := logrus.ParseLevel(config.Level)
	if err != nil {
		logger.Warnf("Invalid LOG_LEVEL '%s', defaulting to 'info'", config.Level)
		config.Level = "info"
	}

	// Load log format
	config.Format = getEnv("LOG_FORMAT", "json")
	if config.Format != "json" && config.Format != "text" {
		logger.Warn("Invalid LOG_FORMAT, must be 'json' or 'text', defaulting to 'json'")
		config.Format = "json"
	}

	// Load log output file
	config.OutputFile = getEnv("LOG_OUTPUT_FILE", "")

	return nil
}

// loadMessagingConfig loads the messaging configuration section
func loadMessagingConfig(logger *logrus.Logger, config *MessagingConfig) error {
	// Load legacy AMQP URL and queue name for backward compatibility
	config.AMQPUrl = getEnv("AMQP_URL", "")
	config.AMQPQueueName = getEnv("AMQP_QUEUE_NAME", "")

	// Validate legacy AMQP config
	if (config.AMQPUrl != "" && config.AMQPQueueName == "") || (config.AMQPUrl == "" && config.AMQPQueueName != "") {
		logger.Warn("Incomplete AMQP configuration: both AMQP_URL and AMQP_QUEUE_NAME must be provided")
	}

	// Load enhanced AMQP configuration
	if err := loadAMQPConfig(logger, &config.AMQP); err != nil {
		return err
	}

	// Load real-time AMQP configuration
	config.EnableRealtimeAMQP = getEnvBool("ENABLE_REALTIME_AMQP", false)
	config.RealtimeQueueName = getEnv("REALTIME_QUEUE_NAME", "siprec_realtime")
	config.RealtimeExchangeName = getEnv("REALTIME_EXCHANGE_NAME", "")
	config.RealtimeRoutingKey = getEnv("REALTIME_ROUTING_KEY", "siprec.realtime")

	// Load real-time AMQP batching configuration
	config.RealtimeBatchSize = getEnvInt("REALTIME_BATCH_SIZE", 10)
	config.RealtimeQueueSize = getEnvInt("REALTIME_QUEUE_SIZE", 1000)

	// Load real-time batch timeout
	realtimeBatchTimeoutStr := getEnv("REALTIME_BATCH_TIMEOUT", "1s")
	realtimeBatchTimeout, err := time.ParseDuration(realtimeBatchTimeoutStr)
	if err != nil {
		logger.Warn("Invalid REALTIME_BATCH_TIMEOUT value, using default: 1s")
		config.RealtimeBatchTimeout = 1 * time.Second
	} else {
		config.RealtimeBatchTimeout = realtimeBatchTimeout
	}

	// Load real-time event filtering configuration
	config.PublishPartialTranscripts = getEnvBool("PUBLISH_PARTIAL_TRANSCRIPTS", true)
	config.PublishFinalTranscripts = getEnvBool("PUBLISH_FINAL_TRANSCRIPTS", true)
	config.PublishSentimentUpdates = getEnvBool("PUBLISH_SENTIMENT_UPDATES", true)
	config.PublishKeywordDetections = getEnvBool("PUBLISH_KEYWORD_DETECTIONS", true)
	config.PublishSpeakerChanges = getEnvBool("PUBLISH_SPEAKER_CHANGES", true)

	// Apply defaults for additional endpoints defined via configuration file
	for i := range config.RealtimeEndpoints {
		endpoint := &config.RealtimeEndpoints[i]
		if endpoint.Name == "" {
			endpoint.Name = fmt.Sprintf("realtime_amqp_%d", i+1)
		}
		if endpoint.QueueName == "" {
			endpoint.QueueName = config.RealtimeQueueName
		}
		if endpoint.ExchangeName == "" {
			endpoint.ExchangeName = config.RealtimeExchangeName
		}
		if endpoint.RoutingKey == "" {
			endpoint.RoutingKey = config.RealtimeRoutingKey
		}

		// If the endpoint relies on enhanced client but lacks explicit hosts, inherit from global config
		if endpoint.UseEnhanced {
			if len(endpoint.AMQP.Hosts) == 0 {
				endpoint.AMQP = config.AMQP
			}
			if endpoint.AMQP.LoadBalancing.Strategy == "" {
				endpoint.AMQP.LoadBalancing = config.AMQP.LoadBalancing
			}
			// Allow TLS overrides via endpoint.TLS when provided
			if endpoint.TLS.Enabled {
				endpoint.AMQP.TLS = endpoint.TLS
			}
		}
	}

	return nil
}

// loadAMQPConfig loads the enhanced AMQP configuration
func loadAMQPConfig(logger *logrus.Logger, config *AMQPConfig) error {
	// Load connection configuration
	hostsStr := getEnv("AMQP_HOSTS", "localhost:5672")
	config.Hosts = strings.Split(hostsStr, ",")
	for i, host := range config.Hosts {
		config.Hosts[i] = strings.TrimSpace(host)
	}

	config.Username = getEnv("AMQP_USERNAME", "guest")
	config.Password = getEnv("AMQP_PASSWORD", "guest")
	config.VirtualHost = getEnv("AMQP_VHOST", "/")

	// Load timeouts
	connectionTimeoutStr := getEnv("AMQP_CONNECTION_TIMEOUT", "30s")
	connectionTimeout, err := time.ParseDuration(connectionTimeoutStr)
	if err != nil {
		logger.Warn("Invalid AMQP_CONNECTION_TIMEOUT value, using default: 30s")
		config.ConnectionTimeout = 30 * time.Second
	} else {
		config.ConnectionTimeout = connectionTimeout
	}

	heartbeatStr := getEnv("AMQP_HEARTBEAT", "10s")
	heartbeat, err := time.ParseDuration(heartbeatStr)
	if err != nil {
		logger.Warn("Invalid AMQP_HEARTBEAT value, using default: 10s")
		config.Heartbeat = 10 * time.Second
	} else {
		config.Heartbeat = heartbeat
	}

	// Load connection pool configuration
	config.MaxConnections = getEnvInt("AMQP_MAX_CONNECTIONS", 10)
	config.MaxChannelsPerConn = getEnvInt("AMQP_MAX_CHANNELS_PER_CONN", 100)

	connectionIdleTimeStr := getEnv("AMQP_CONNECTION_IDLE_TIME", "5m")
	connectionIdleTime, err := time.ParseDuration(connectionIdleTimeStr)
	if err != nil {
		logger.Warn("Invalid AMQP_CONNECTION_IDLE_TIME value, using default: 5m")
		config.ConnectionIdleTime = 5 * time.Minute
	} else {
		config.ConnectionIdleTime = connectionIdleTime
	}

	// Load load balancing configuration
	config.LoadBalancing.Enabled = getEnvBool("AMQP_LB_ENABLED", true)
	config.LoadBalancing.Strategy = getEnv("AMQP_LB_STRATEGY", "round_robin")
	config.LoadBalancing.HealthCheck = getEnvBool("AMQP_LB_HEALTH_CHECK", true)

	// Load message configuration
	config.DefaultExchange = getEnv("AMQP_DEFAULT_EXCHANGE", "")
	config.DefaultRoutingKey = getEnv("AMQP_DEFAULT_ROUTING_KEY", "")

	messageTTLStr := getEnv("AMQP_MESSAGE_TTL", "24h")
	messageTTL, err := time.ParseDuration(messageTTLStr)
	if err != nil {
		logger.Warn("Invalid AMQP_MESSAGE_TTL value, using default: 24h")
		config.MessageTTL = 24 * time.Hour
	} else {
		config.MessageTTL = messageTTL
	}

	publishTimeoutStr := getEnv("AMQP_PUBLISH_TIMEOUT", "5s")
	publishTimeout, err := time.ParseDuration(publishTimeoutStr)
	if err != nil {
		logger.Warn("Invalid AMQP_PUBLISH_TIMEOUT value, using default: 5s")
		config.PublishTimeout = 5 * time.Second
	} else {
		config.PublishTimeout = publishTimeout
	}

	config.PublishConfirm = getEnvBool("AMQP_PUBLISH_CONFIRM", true)

	// Load dead letter configuration
	config.DeadLetterExchange = getEnv("AMQP_DLX", "siprec.dlx")
	config.DeadLetterRoutingKey = getEnv("AMQP_DLX_ROUTING_KEY", "failed")
	config.MaxRetries = getEnvInt("AMQP_MAX_RETRIES", 3)

	retryDelayStr := getEnv("AMQP_RETRY_DELAY", "30s")
	retryDelay, err := time.ParseDuration(retryDelayStr)
	if err != nil {
		logger.Warn("Invalid AMQP_RETRY_DELAY value, using default: 30s")
		config.RetryDelay = 30 * time.Second
	} else {
		config.RetryDelay = retryDelay
	}

	// Load QoS configuration
	config.PrefetchCount = getEnvInt("AMQP_PREFETCH_COUNT", 10)
	config.PrefetchSize = getEnvInt("AMQP_PREFETCH_SIZE", 0)
	config.GlobalQos = getEnvBool("AMQP_GLOBAL_QOS", false)

	// Load TLS configuration
	config.TLS.Enabled = getEnvBool("AMQP_TLS_ENABLED", false)
	config.TLS.CertFile = getEnv("AMQP_TLS_CERT_FILE", "")
	config.TLS.KeyFile = getEnv("AMQP_TLS_KEY_FILE", "")
	config.TLS.CAFile = getEnv("AMQP_TLS_CA_FILE", "")
	config.TLS.SkipVerify = getEnvBool("AMQP_TLS_SKIP_VERIFY", false)

	// Load monitoring configuration
	config.EnableMetrics = getEnvBool("AMQP_ENABLE_METRICS", true)

	metricsIntervalStr := getEnv("AMQP_METRICS_INTERVAL", "30s")
	metricsInterval, err := time.ParseDuration(metricsIntervalStr)
	if err != nil {
		logger.Warn("Invalid AMQP_METRICS_INTERVAL value, using default: 30s")
		config.MetricsInterval = 30 * time.Second
	} else {
		config.MetricsInterval = metricsInterval
	}

	// Load reconnection configuration
	reconnectDelayStr := getEnv("AMQP_RECONNECT_DELAY", "5s")
	reconnectDelay, err := time.ParseDuration(reconnectDelayStr)
	if err != nil {
		logger.Warn("Invalid AMQP_RECONNECT_DELAY value, using default: 5s")
		config.ReconnectDelay = 5 * time.Second
	} else {
		config.ReconnectDelay = reconnectDelay
	}

	maxReconnectDelayStr := getEnv("AMQP_MAX_RECONNECT_DELAY", "30s")
	maxReconnectDelay, err := time.ParseDuration(maxReconnectDelayStr)
	if err != nil {
		logger.Warn("Invalid AMQP_MAX_RECONNECT_DELAY value, using default: 30s")
		config.MaxReconnectDelay = 30 * time.Second
	} else {
		config.MaxReconnectDelay = maxReconnectDelay
	}

	config.ReconnectMultiplier = getEnvFloat("AMQP_RECONNECT_MULTIPLIER", 2.0)
	config.MaxReconnectAttempts = getEnvInt("AMQP_MAX_RECONNECT_ATTEMPTS", 0)

	return nil
}

// loadRedundancyConfig loads the redundancy configuration section
func loadRedundancyConfig(logger *logrus.Logger, config *RedundancyConfig) error {
	// Load redundancy enabled flag
	config.Enabled = getEnvBool("ENABLE_REDUNDANCY", true)

	// Load session timeout
	sessionTimeoutStr := getEnv("SESSION_TIMEOUT", "30s")
	sessionTimeout, err := time.ParseDuration(sessionTimeoutStr)
	if err != nil {
		logger.Warn("Invalid SESSION_TIMEOUT value, using default: 30s")
		config.SessionTimeout = 30 * time.Second
	} else {
		config.SessionTimeout = sessionTimeout
	}

	// Load session check interval
	sessionCheckIntervalStr := getEnv("SESSION_CHECK_INTERVAL", "10s")
	sessionCheckInterval, err := time.ParseDuration(sessionCheckIntervalStr)
	if err != nil {
		logger.Warn("Invalid SESSION_CHECK_INTERVAL value, using default: 10s")
		config.SessionCheckInterval = 10 * time.Second
	} else {
		config.SessionCheckInterval = sessionCheckInterval
	}

	// Load storage type
	config.StorageType = getEnv("REDUNDANCY_STORAGE_TYPE", "memory")
	if config.StorageType != "memory" && config.StorageType != "redis" {
		logger.Warn("Invalid REDUNDANCY_STORAGE_TYPE value, must be 'memory' or 'redis', using default: memory")
		config.StorageType = "memory"
	}

	return nil
}

// loadEncryptionConfig loads the encryption configuration section
func loadEncryptionConfig(logger *logrus.Logger, config *EncryptionConfig) error {
	// Load encryption enabled flags
	config.EnableRecordingEncryption = getEnvBool("ENABLE_RECORDING_ENCRYPTION", false)
	config.EnableMetadataEncryption = getEnvBool("ENABLE_METADATA_ENCRYPTION", false)

	// Load algorithm configuration
	config.Algorithm = getEnv("ENCRYPTION_ALGORITHM", "AES-256-GCM")
	if config.Algorithm != "AES-256-GCM" && config.Algorithm != "AES-256-CBC" && config.Algorithm != "ChaCha20-Poly1305" {
		logger.Warn("Invalid ENCRYPTION_ALGORITHM value, using default: AES-256-GCM")
		config.Algorithm = "AES-256-GCM"
	}

	config.KeyDerivationMethod = getEnv("KEY_DERIVATION_METHOD", "PBKDF2")
	if config.KeyDerivationMethod != "PBKDF2" && config.KeyDerivationMethod != "Argon2id" {
		logger.Warn("Invalid KEY_DERIVATION_METHOD value, using default: PBKDF2")
		config.KeyDerivationMethod = "PBKDF2"
	}

	// Load key management configuration
	config.MasterKeyPath = getEnv("MASTER_KEY_PATH", "./keys")
	config.KeyBackupEnabled = getEnvBool("KEY_BACKUP_ENABLED", true)

	// Load key rotation interval
	keyRotationIntervalStr := getEnv("KEY_ROTATION_INTERVAL", "24h")
	keyRotationInterval, err := time.ParseDuration(keyRotationIntervalStr)
	if err != nil {
		logger.Warn("Invalid KEY_ROTATION_INTERVAL value, using default: 24h")
		config.KeyRotationInterval = 24 * time.Hour
	} else {
		config.KeyRotationInterval = keyRotationInterval
	}

	// Load security parameters
	config.KeySize = getEnvInt("ENCRYPTION_KEY_SIZE", 32)
	if config.KeySize != 16 && config.KeySize != 24 && config.KeySize != 32 {
		logger.Warn("Invalid ENCRYPTION_KEY_SIZE value, using default: 32")
		config.KeySize = 32
	}

	config.NonceSize = getEnvInt("ENCRYPTION_NONCE_SIZE", 12)
	if config.NonceSize < 8 || config.NonceSize > 24 {
		logger.Warn("Invalid ENCRYPTION_NONCE_SIZE value, using default: 12")
		config.NonceSize = 12
	}

	config.SaltSize = getEnvInt("ENCRYPTION_SALT_SIZE", 32)
	if config.SaltSize < 16 || config.SaltSize > 64 {
		logger.Warn("Invalid ENCRYPTION_SALT_SIZE value, using default: 32")
		config.SaltSize = 32
	}

	config.PBKDF2Iterations = getEnvInt("PBKDF2_ITERATIONS", 100000)
	if config.PBKDF2Iterations < 10000 {
		logger.Warn("PBKDF2_ITERATIONS too low for security, using minimum: 100000")
		config.PBKDF2Iterations = 100000
	}

	// Load storage configuration
	config.EncryptionKeyStore = getEnv("ENCRYPTION_KEY_STORE", "memory")
	if config.EncryptionKeyStore != "file" && config.EncryptionKeyStore != "env" && config.EncryptionKeyStore != "vault" && config.EncryptionKeyStore != "memory" {
		logger.Warn("Invalid ENCRYPTION_KEY_STORE value, using default: memory")
		config.EncryptionKeyStore = "memory"
	}

	// Log encryption status
	if config.EnableRecordingEncryption || config.EnableMetadataEncryption {
		logger.WithFields(logrus.Fields{
			"recording_encryption": config.EnableRecordingEncryption,
			"metadata_encryption":  config.EnableMetadataEncryption,
			"algorithm":            config.Algorithm,
			"key_store":            config.EncryptionKeyStore,
		}).Info("Encryption enabled")
	} else {
		logger.Debug("Encryption disabled")
	}

	return nil
}

// loadAsyncSTTConfig loads the async STT configuration section
func loadAsyncSTTConfig(logger *logrus.Logger, config *AsyncSTTConfig) error {
	// Load enabled flag
	config.Enabled = getEnvBool("STT_ASYNC_ENABLED", true)

	// Load worker configuration
	config.WorkerCount = getEnvInt("STT_WORKER_COUNT", 3)
	if config.WorkerCount < 1 || config.WorkerCount > 100 {
		logger.Warn("Invalid STT_WORKER_COUNT value, using default: 3")
		config.WorkerCount = 3
	}

	config.MaxRetries = getEnvInt("STT_MAX_RETRIES", 3)
	if config.MaxRetries < 0 || config.MaxRetries > 10 {
		logger.Warn("Invalid STT_MAX_RETRIES value, using default: 3")
		config.MaxRetries = 3
	}

	// Load retry backoff
	retryBackoffStr := getEnv("STT_RETRY_BACKOFF", "30s")
	retryBackoff, err := time.ParseDuration(retryBackoffStr)
	if err != nil {
		logger.Warn("Invalid STT_RETRY_BACKOFF value, using default: 30s")
		config.RetryBackoff = 30 * time.Second
	} else {
		config.RetryBackoff = retryBackoff
	}

	// Load job timeout
	jobTimeoutStr := getEnv("STT_JOB_TIMEOUT", "300s")
	jobTimeout, err := time.ParseDuration(jobTimeoutStr)
	if err != nil {
		logger.Warn("Invalid STT_JOB_TIMEOUT value, using default: 300s")
		config.JobTimeout = 300 * time.Second
	} else {
		config.JobTimeout = jobTimeout
	}

	// Load queue configuration
	config.QueueBufferSize = getEnvInt("STT_QUEUE_BUFFER_SIZE", 1000)
	if config.QueueBufferSize < 10 || config.QueueBufferSize > 100000 {
		logger.Warn("Invalid STT_QUEUE_BUFFER_SIZE value, using default: 1000")
		config.QueueBufferSize = 1000
	}

	config.BatchSize = getEnvInt("STT_BATCH_SIZE", 10)
	if config.BatchSize < 1 || config.BatchSize > 100 {
		logger.Warn("Invalid STT_BATCH_SIZE value, using default: 10")
		config.BatchSize = 10
	}

	// Load batch timeout
	batchTimeoutStr := getEnv("STT_BATCH_TIMEOUT", "60s")
	batchTimeout, err := time.ParseDuration(batchTimeoutStr)
	if err != nil {
		logger.Warn("Invalid STT_BATCH_TIMEOUT value, using default: 60s")
		config.BatchTimeout = 60 * time.Second
	} else {
		config.BatchTimeout = batchTimeout
	}

	config.EnablePrioritization = getEnvBool("STT_ENABLE_PRIORITIZATION", true)

	// Load resource limits
	config.MaxConcurrentJobs = getEnvInt("STT_MAX_CONCURRENT_JOBS", 50)
	if config.MaxConcurrentJobs < 1 || config.MaxConcurrentJobs > 1000 {
		logger.Warn("Invalid STT_MAX_CONCURRENT_JOBS value, using default: 50")
		config.MaxConcurrentJobs = 50
	}

	// Load cleanup configuration
	cleanupIntervalStr := getEnv("STT_CLEANUP_INTERVAL", "300s")
	cleanupInterval, err := time.ParseDuration(cleanupIntervalStr)
	if err != nil {
		logger.Warn("Invalid STT_CLEANUP_INTERVAL value, using default: 300s")
		config.CleanupInterval = 300 * time.Second
	} else {
		config.CleanupInterval = cleanupInterval
	}

	jobRetentionStr := getEnv("STT_JOB_RETENTION_TIME", "24h")
	jobRetention, err := time.ParseDuration(jobRetentionStr)
	if err != nil {
		logger.Warn("Invalid STT_JOB_RETENTION_TIME value, using default: 24h")
		config.JobRetentionTime = 24 * time.Hour
	} else {
		config.JobRetentionTime = jobRetention
	}

	// Load cost tracking
	config.EnableCostTracking = getEnvBool("STT_ENABLE_COST_TRACKING", true)

	// Log async STT configuration
	if config.Enabled {
		logger.WithFields(logrus.Fields{
			"workers":        config.WorkerCount,
			"max_retries":    config.MaxRetries,
			"queue_size":     config.QueueBufferSize,
			"max_concurrent": config.MaxConcurrentJobs,
			"cost_tracking":  config.EnableCostTracking,
		}).Info("Async STT processing enabled")
	} else {
		logger.Debug("Async STT processing disabled")
	}

	return nil
}

// loadHotReloadConfig loads the hot-reload configuration section
func loadHotReloadConfig(logger *logrus.Logger, config *HotReloadConfig) error {
	// Load enabled flag
	config.Enabled = getEnvBool("CONFIG_HOTRELOAD_ENABLED", true)

	// Load debounce time
	debounceStr := getEnv("CONFIG_HOTRELOAD_DEBOUNCE", "2s")
	debounce, err := time.ParseDuration(debounceStr)
	if err != nil {
		logger.Warn("Invalid CONFIG_HOTRELOAD_DEBOUNCE value, using default: 2s")
		config.DebounceTime = 2 * time.Second
	} else {
		config.DebounceTime = debounce
	}

	// Load max reload time
	maxReloadStr := getEnv("CONFIG_HOTRELOAD_MAX_TIME", "30s")
	maxReload, err := time.ParseDuration(maxReloadStr)
	if err != nil {
		logger.Warn("Invalid CONFIG_HOTRELOAD_MAX_TIME value, using default: 30s")
		config.MaxReloadTime = 30 * time.Second
	} else {
		config.MaxReloadTime = maxReload
	}

	// Load backup configuration
	config.BackupEnabled = getEnvBool("CONFIG_BACKUP_ENABLED", true)
	config.BackupDir = getEnv("CONFIG_BACKUP_DIR", "./config_backups")

	// Log hot-reload configuration
	if config.Enabled {
		logger.WithFields(logrus.Fields{
			"debounce_time":   config.DebounceTime,
			"max_reload_time": config.MaxReloadTime,
			"backup_enabled":  config.BackupEnabled,
			"backup_dir":      config.BackupDir,
		}).Info("Configuration hot-reload enabled")
	} else {
		logger.Debug("Configuration hot-reload disabled")
	}

	return nil
}

// loadPerformanceConfig loads the performance configuration section
func loadPerformanceConfig(logger *logrus.Logger, config *PerformanceConfig) error {
	// Load enabled flag
	config.Enabled = getEnvBool("PERFORMANCE_MONITORING_ENABLED", true)

	// Load monitor interval
	intervalStr := getEnv("PERFORMANCE_MONITOR_INTERVAL", "30s")
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		logger.Warn("Invalid PERFORMANCE_MONITOR_INTERVAL value, using default: 30s")
		config.MonitorInterval = 30 * time.Second
	} else {
		config.MonitorInterval = interval
	}

	// Load memory settings
	config.GCThresholdMB = int64(getEnvInt("PERFORMANCE_GC_THRESHOLD_MB", 100))
	if config.GCThresholdMB < 10 || config.GCThresholdMB > 1024 {
		logger.Warn("Invalid PERFORMANCE_GC_THRESHOLD_MB value, using default: 100")
		config.GCThresholdMB = 100
	}

	config.MemoryLimitMB = int64(getEnvInt("PERFORMANCE_MEMORY_LIMIT_MB", 512))
	if config.MemoryLimitMB < 64 || config.MemoryLimitMB > 8192 {
		logger.Warn("Invalid PERFORMANCE_MEMORY_LIMIT_MB value, using default: 512")
		config.MemoryLimitMB = 512
	}

	// Load CPU settings
	cpuLimitStr := getEnv("PERFORMANCE_CPU_LIMIT", "80.0")
	if cpuLimit, err := strconv.ParseFloat(cpuLimitStr, 64); err != nil {
		logger.Warn("Invalid PERFORMANCE_CPU_LIMIT value, using default: 80.0")
		config.CPULimit = 80.0
	} else {
		config.CPULimit = cpuLimit
	}

	// Load optimization settings
	config.EnableAutoGC = getEnvBool("PERFORMANCE_ENABLE_AUTO_GC", true)
	config.GCTargetPercent = getEnvInt("PERFORMANCE_GC_TARGET_PERCENT", 50)
	if config.GCTargetPercent < 10 || config.GCTargetPercent > 200 {
		logger.Warn("Invalid PERFORMANCE_GC_TARGET_PERCENT value, using default: 50")
		config.GCTargetPercent = 50
	}

	// Log performance configuration
	if config.Enabled {
		logger.WithFields(logrus.Fields{
			"monitor_interval":  config.MonitorInterval,
			"gc_threshold_mb":   config.GCThresholdMB,
			"memory_limit_mb":   config.MemoryLimitMB,
			"cpu_limit":         config.CPULimit,
			"auto_gc":           config.EnableAutoGC,
			"gc_target_percent": config.GCTargetPercent,
		}).Info("Performance monitoring enabled")
	} else {
		logger.Debug("Performance monitoring disabled")
	}

	return nil
}

// loadPauseResumeConfig loads the pause/resume configuration
func loadPauseResumeConfig(logger *logrus.Logger, config *PauseResumeConfig) error {
	// Load enabled flag
	config.Enabled = getEnvBool("PAUSE_RESUME_ENABLED", false)

	// Load pause options
	config.PauseRecording = getEnvBool("PAUSE_RECORDING", true)
	config.PauseTranscription = getEnvBool("PAUSE_TRANSCRIPTION", true)

	// Load notification settings
	config.SendNotifications = getEnvBool("PAUSE_RESUME_NOTIFICATIONS", true)

	// Load pause duration settings
	maxPauseDurationStr := getEnv("MAX_PAUSE_DURATION", "0")
	if maxPauseDurationStr != "0" {
		maxPauseDuration, err := time.ParseDuration(maxPauseDurationStr)
		if err != nil {
			logger.Warn("Invalid MAX_PAUSE_DURATION value, using default: 0 (unlimited)")
			config.MaxPauseDuration = 0
		} else {
			config.MaxPauseDuration = maxPauseDuration
		}
	} else {
		config.MaxPauseDuration = 0
	}

	// Load auto-resume settings
	config.AutoResume = getEnvBool("PAUSE_AUTO_RESUME", false)

	// Load session settings
	config.PerSession = getEnvBool("PAUSE_RESUME_PER_SESSION", true)

	// Load authentication settings
	config.RequireAuth = getEnvBool("PAUSE_RESUME_REQUIRE_AUTH", true)
	config.APIKey = getEnv("PAUSE_RESUME_API_KEY", "")

	// Validate API key if auth is required
	if config.RequireAuth && config.APIKey == "" {
		logger.Warn("Pause/Resume API authentication is required but no API key is set")
	}

	// Log configuration
	if config.Enabled {
		logger.WithFields(logrus.Fields{
			"pause_recording":     config.PauseRecording,
			"pause_transcription": config.PauseTranscription,
			"send_notifications":  config.SendNotifications,
			"max_pause_duration":  config.MaxPauseDuration,
			"auto_resume":         config.AutoResume,
			"per_session":         config.PerSession,
			"require_auth":        config.RequireAuth,
		}).Info("Pause/Resume API enabled")
	} else {
		logger.Debug("Pause/Resume API disabled")
	}

	return nil
}

// validateConfig performs cross-section validation of the configuration
func validateConfig(logger *logrus.Logger, config *Config) error {
	// Validate port conflicts
	for _, sipPort := range config.Network.Ports {
		if sipPort == config.HTTP.Port {
			return errors.New(fmt.Sprintf("port conflict: SIP port %d conflicts with HTTP port", sipPort))
		}

		if config.Network.EnableTLS && sipPort == config.Network.TLSPort {
			return errors.New(fmt.Sprintf("port conflict: SIP port %d conflicts with TLS port", sipPort))
		}
	}

	if config.Network.EnableTLS && config.Network.TLSPort == config.HTTP.Port {
		return errors.New(fmt.Sprintf("port conflict: TLS port %d conflicts with HTTP port", config.Network.TLSPort))
	}

	if config.Network.RequireTLSOnly {
		if !config.Network.EnableTLS {
			return errors.New("SIP_REQUIRE_TLS is enabled but TLS is disabled. Please set ENABLE_TLS=true and configure TLS certificates")
		}
		if len(config.Network.Ports) > 0 {
			logger.Warn("SIP_REQUIRE_TLS enabled; UDP/TCP listeners will be skipped in favor of TLS-only mode")
		}
	}

	if config.Network.RequireSRTP && !config.Network.EnableSRTP {
		return errors.New("SIP_REQUIRE_SRTP is enabled but SRTP is disabled. Set ENABLE_SRTP=true")
	}

	// Validate RTP port range
	if config.Network.RTPPortMax <= config.Network.RTPPortMin {
		return errors.New("invalid RTP port range: RTP_PORT_MAX must be greater than RTP_PORT_MIN")
	}

	// Validate redundancy configuration
	if config.Redundancy.Enabled {
		if config.Redundancy.SessionTimeout <= 0 {
			return errors.New("invalid SESSION_TIMEOUT: must be a positive duration")
		}

		if config.Redundancy.SessionCheckInterval <= 0 {
			return errors.New("invalid SESSION_CHECK_INTERVAL: must be a positive duration")
		}

		if config.Redundancy.SessionCheckInterval >= config.Redundancy.SessionTimeout {
			logger.Warn("SESSION_CHECK_INTERVAL should be smaller than SESSION_TIMEOUT for effective monitoring")
		}

		if config.Redundancy.StorageType == "redis" && config.Messaging.AMQPUrl == "" {
			logger.Warn("Redis storage type selected but AMQP not configured for notifications")
		}
	}

	// Validate logging configuration
	if config.Logging.OutputFile != "" {
		// Check if the log file can be created/written
		f, err := os.OpenFile(config.Logging.OutputFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("cannot write to log file: %s", config.Logging.OutputFile))
		}
		f.Close()
	}

	if config.Analytics.Enabled {
		if len(config.Analytics.Elasticsearch.Addresses) == 0 {
			return errors.New("analytics enabled but ELASTICSEARCH_ADDRESSES is empty")
		}
		if strings.TrimSpace(config.Analytics.Elasticsearch.Index) == "" {
			return errors.New("analytics enabled but ELASTICSEARCH_INDEX is empty")
		}
	}

	return nil
}

// ensureDirectories ensures that required directories exist
func ensureDirectories(logger *logrus.Logger, config *Config) error {
	// Ensure recording directory exists
	if err := os.MkdirAll(config.Recording.Directory, 0755); err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to create recording directory: %s", config.Recording.Directory))
	}

	// Ensure sessions directory exists if redundancy is enabled
	if config.Redundancy.Enabled && config.Redundancy.StorageType == "memory" {
		sessionsDir := "sessions"
		if err := os.MkdirAll(sessionsDir, 0755); err != nil {
			return errors.Wrap(err, fmt.Sprintf("failed to create sessions directory: %s", sessionsDir))
		}
	}

	return nil
}

// Apply applies the configuration to the logger
func (c *Config) ApplyLogging(logger *logrus.Logger) error {
	// Set log level
	level, err := logrus.ParseLevel(c.Logging.Level)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("invalid log level: %s", c.Logging.Level))
	}
	logger.SetLevel(level)

	// Set log format
	if c.Logging.Format == "json" {
		logger.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: time.RFC3339Nano,
			FieldMap: logrus.FieldMap{
				logrus.FieldKeyTime:  "timestamp",
				logrus.FieldKeyLevel: "level",
				logrus.FieldKeyMsg:   "message",
			},
		})
	} else {
		logger.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: time.RFC3339Nano,
		})
	}

	// Set log output
	if c.Logging.OutputFile != "" {
		f, err := os.OpenFile(c.Logging.OutputFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("failed to open log file: %s", c.Logging.OutputFile))
		}
		logger.SetOutput(f)
	} else {
		logger.SetOutput(os.Stdout)
	}

	return nil
}

// Helper function to parse comma-separated port list
func parsePorts(portsStr, envName string) ([]int, error) {
	portsStr = strings.TrimSpace(portsStr)
	if portsStr == "" {
		return nil, nil
	}

	portsSlice := strings.Split(portsStr, ",")
	var ports []int

	for _, portStr := range portsSlice {
		portStr = strings.TrimSpace(portStr)
		if portStr == "" {
			continue
		}

		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("invalid port in %s: %s", envName, portStr))
		}

		if port < 1 || port > 65535 {
			return nil, errors.New(fmt.Sprintf("port out of range in %s: %d", envName, port))
		}

		ports = append(ports, port)
	}

	return ports, nil
}

// GetUDPPorts returns the ports to use for UDP listeners
func (n *NetworkConfig) GetUDPPorts() []int {
	if len(n.UDPPorts) > 0 {
		return n.UDPPorts
	}
	return n.Ports
}

// GetTCPPorts returns the ports to use for TCP listeners
func (n *NetworkConfig) GetTCPPorts() []int {
	if len(n.TCPPorts) > 0 {
		return n.TCPPorts
	}
	return n.Ports
}

// Helper function to get an environment variable with a default value
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// Helper function to get a boolean environment variable with a default value
func getEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	switch strings.ToLower(value) {
	case "true", "yes", "1", "on":
		return true
	case "false", "no", "0", "off":
		return false
	default:
		return defaultValue
	}
}

// Helper function to get an integer environment variable with a default value
func getEnvInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	intValue, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}

	return intValue
}

// Helper function to get a duration environment variable with a default value
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return defaultValue
	}

	return duration
}

// getEnvFloat retrieves an environment variable and converts it to float64
func getEnvFloat(key string, defaultValue float64) float64 {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	floatValue, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return defaultValue
	}

	return floatValue
}

// Helper function to get external IP
func getExternalIP(logger *logrus.Logger) string {
	// Try multiple services to be resilient
	services := []string{
		"https://api.ipify.org",
		"https://ifconfig.me",
		"https://icanhazip.com",
	}

	for _, service := range services {
		resp, err := http.Get(service)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			body := make([]byte, 100)
			n, err := resp.Body.Read(body)
			if err != nil && err.Error() != "EOF" {
				continue
			}
			return strings.TrimSpace(string(body[:n]))
		}
	}

	logger.Warn("Could not auto-detect external IP, using localhost as fallback")
	return "127.0.0.1" // Fallback
}

// Helper function to get internal IP
func getInternalIP(logger *logrus.Logger) string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		logger.Warn("Could not get interface addresses, using localhost as fallback")
		return "127.0.0.1"
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}

	logger.Warn("Could not find non-loopback interface address, using localhost as fallback")
	return "127.0.0.1"
}

// loadCircuitBreakerConfig loads the circuit breaker configuration section
func loadCircuitBreakerConfig(logger *logrus.Logger, config *CircuitBreakerConfig) error {
	// Load global circuit breaker settings
	config.Enabled = getEnvBool("CIRCUIT_BREAKER_ENABLED", true)

	// Load STT circuit breaker settings
	config.STTFailureThreshold = int64(getEnvInt("STT_CB_FAILURE_THRESHOLD", 3))

	sttTimeoutStr := getEnv("STT_CB_TIMEOUT", "30s")
	sttTimeout, err := time.ParseDuration(sttTimeoutStr)
	if err != nil {
		logger.Warn("Invalid STT_CB_TIMEOUT value, using default: 30s")
		config.STTTimeout = 30 * time.Second
	} else {
		config.STTTimeout = sttTimeout
	}

	sttRequestTimeoutStr := getEnv("STT_CB_REQUEST_TIMEOUT", "45s")
	sttRequestTimeout, err := time.ParseDuration(sttRequestTimeoutStr)
	if err != nil {
		logger.Warn("Invalid STT_CB_REQUEST_TIMEOUT value, using default: 45s")
		config.STTRequestTimeout = 45 * time.Second
	} else {
		config.STTRequestTimeout = sttRequestTimeout
	}

	// Load AMQP circuit breaker settings
	config.AMQPFailureThreshold = int64(getEnvInt("AMQP_CB_FAILURE_THRESHOLD", 5))

	amqpTimeoutStr := getEnv("AMQP_CB_TIMEOUT", "60s")
	amqpTimeout, err := time.ParseDuration(amqpTimeoutStr)
	if err != nil {
		logger.Warn("Invalid AMQP_CB_TIMEOUT value, using default: 60s")
		config.AMQPTimeout = 60 * time.Second
	} else {
		config.AMQPTimeout = amqpTimeout
	}

	amqpRequestTimeoutStr := getEnv("AMQP_CB_REQUEST_TIMEOUT", "10s")
	amqpRequestTimeout, err := time.ParseDuration(amqpRequestTimeoutStr)
	if err != nil {
		logger.Warn("Invalid AMQP_CB_REQUEST_TIMEOUT value, using default: 10s")
		config.AMQPRequestTimeout = 10 * time.Second
	} else {
		config.AMQPRequestTimeout = amqpRequestTimeout
	}

	// Load Redis circuit breaker settings
	config.RedisFailureThreshold = int64(getEnvInt("REDIS_CB_FAILURE_THRESHOLD", 8))

	redisTimeoutStr := getEnv("REDIS_CB_TIMEOUT", "20s")
	redisTimeout, err := time.ParseDuration(redisTimeoutStr)
	if err != nil {
		logger.Warn("Invalid REDIS_CB_TIMEOUT value, using default: 20s")
		config.RedisTimeout = 20 * time.Second
	} else {
		config.RedisTimeout = redisTimeout
	}

	redisRequestTimeoutStr := getEnv("REDIS_CB_REQUEST_TIMEOUT", "5s")
	redisRequestTimeout, err := time.ParseDuration(redisRequestTimeoutStr)
	if err != nil {
		logger.Warn("Invalid REDIS_CB_REQUEST_TIMEOUT value, using default: 5s")
		config.RedisRequestTimeout = 5 * time.Second
	} else {
		config.RedisRequestTimeout = redisRequestTimeout
	}

	// Load HTTP circuit breaker settings
	config.HTTPFailureThreshold = int64(getEnvInt("HTTP_CB_FAILURE_THRESHOLD", 5))

	httpTimeoutStr := getEnv("HTTP_CB_TIMEOUT", "45s")
	httpTimeout, err := time.ParseDuration(httpTimeoutStr)
	if err != nil {
		logger.Warn("Invalid HTTP_CB_TIMEOUT value, using default: 45s")
		config.HTTPTimeout = 45 * time.Second
	} else {
		config.HTTPTimeout = httpTimeout
	}

	httpRequestTimeoutStr := getEnv("HTTP_CB_REQUEST_TIMEOUT", "30s")
	httpRequestTimeout, err := time.ParseDuration(httpRequestTimeoutStr)
	if err != nil {
		logger.Warn("Invalid HTTP_CB_REQUEST_TIMEOUT value, using default: 30s")
		config.HTTPRequestTimeout = 30 * time.Second
	} else {
		config.HTTPRequestTimeout = httpRequestTimeout
	}

	// Load monitoring settings
	config.MonitoringEnabled = getEnvBool("CB_MONITORING_ENABLED", true)

	monitoringIntervalStr := getEnv("CB_MONITORING_INTERVAL", "30s")
	monitoringInterval, err := time.ParseDuration(monitoringIntervalStr)
	if err != nil {
		logger.Warn("Invalid CB_MONITORING_INTERVAL value, using default: 30s")
		config.MonitoringInterval = 30 * time.Second
	} else {
		config.MonitoringInterval = monitoringInterval
	}

	return nil
}

// loadGoogleSTTConfig loads Google STT configuration
func loadGoogleSTTConfig(logger *logrus.Logger, config *GoogleSTTConfig) error {
	config.Enabled = getEnvBool("GOOGLE_STT_ENABLED", true)
	config.CredentialsFile = getEnv("GOOGLE_APPLICATION_CREDENTIALS", "")
	config.ProjectID = getEnv("GOOGLE_PROJECT_ID", "")
	config.APIKey = getEnv("GOOGLE_STT_API_KEY", "")
	config.Language = getEnv("GOOGLE_STT_LANGUAGE", "en-US")
	config.SampleRate = getEnvInt("GOOGLE_STT_SAMPLE_RATE", 16000)
	config.EnhancedModels = getEnvBool("GOOGLE_STT_ENHANCED_MODELS", false)
	config.Model = getEnv("GOOGLE_STT_MODEL", "latest_long")
	config.EnableAutomaticPunctuation = getEnvBool("GOOGLE_STT_AUTO_PUNCTUATION", true)
	config.EnableWordTimeOffsets = getEnvBool("GOOGLE_STT_WORD_TIME_OFFSETS", true)
	config.MaxAlternatives = getEnvInt("GOOGLE_STT_MAX_ALTERNATIVES", 1)
	config.ProfanityFilter = getEnvBool("GOOGLE_STT_PROFANITY_FILTER", false)

	if config.Enabled && config.CredentialsFile == "" && config.APIKey == "" {
		logger.Warn("Google STT enabled but neither GOOGLE_APPLICATION_CREDENTIALS nor GOOGLE_STT_API_KEY is set")
	}

	return nil
}

// loadDeepgramSTTConfig loads Deepgram STT configuration
func loadDeepgramSTTConfig(logger *logrus.Logger, config *DeepgramSTTConfig) error {
	config.Enabled = getEnvBool("DEEPGRAM_ENABLED", getEnvBool("DEEPGRAM_STT_ENABLED", false))
	config.APIKey = getEnv("DEEPGRAM_API_KEY", "")
	config.APIURL = getEnv("DEEPGRAM_API_URL", "https://api.deepgram.com")
	config.Model = getEnv("DEEPGRAM_MODEL", "nova-2")
	config.Language = getEnv("DEEPGRAM_LANGUAGE", "en-US")
	config.Encoding = getEnv("DEEPGRAM_ENCODING", "mulaw")
	config.SampleRate = getEnvInt("DEEPGRAM_SAMPLE_RATE", 8000)
	config.Channels = getEnvInt("DEEPGRAM_CHANNELS", 1)
	config.Tier = getEnv("DEEPGRAM_TIER", "nova")
	config.Version = getEnv("DEEPGRAM_VERSION", "latest")
	config.Punctuate = getEnvBool("DEEPGRAM_PUNCTUATE", true)
	config.Diarize = getEnvBool("DEEPGRAM_DIARIZE", false)
	config.Numerals = getEnvBool("DEEPGRAM_NUMERALS", true)
	config.SmartFormat = getEnvBool("DEEPGRAM_SMART_FORMAT", true)
	config.ProfanityFilter = getEnvBool("DEEPGRAM_PROFANITY_FILTER", false)

	// Parse redact list
	redactStr := getEnv("DEEPGRAM_REDACT", "")
	if redactStr != "" {
		config.Redact = strings.Split(redactStr, ",")
		for i, item := range config.Redact {
			config.Redact[i] = strings.TrimSpace(item)
		}
	}

	// Parse keywords list
	keywordsStr := getEnv("DEEPGRAM_KEYWORDS", "")
	if keywordsStr != "" {
		config.Keywords = strings.Split(keywordsStr, ",")
		for i, keyword := range config.Keywords {
			config.Keywords[i] = strings.TrimSpace(keyword)
		}
	}

	// Load multi-language and accent detection configuration
	config.DetectLanguage = getEnvBool("DEEPGRAM_DETECT_LANGUAGE", true)

	// Parse supported languages list
	supportedLangsStr := getEnv("DEEPGRAM_SUPPORTED_LANGUAGES", "en-US,es-ES,fr-FR,de-DE,it-IT,pt-BR,ru-RU,ja-JP,zh-CN,ko-KR,ar-SA,hi-IN")
	if supportedLangsStr != "" {
		config.SupportedLanguages = strings.Split(supportedLangsStr, ",")
		for i, lang := range config.SupportedLanguages {
			config.SupportedLanguages[i] = strings.TrimSpace(lang)
		}
	} else {
		// Default to common languages if not specified
		config.SupportedLanguages = []string{"en-US", "es-ES", "fr-FR", "de-DE", "it-IT", "pt-BR"}
	}

	// Load language confidence threshold
	confidenceStr := getEnv("DEEPGRAM_LANGUAGE_CONFIDENCE", "0.7")
	if confidence, err := strconv.ParseFloat(confidenceStr, 64); err != nil {
		logger.Warn("Invalid DEEPGRAM_LANGUAGE_CONFIDENCE value, using default: 0.7")
		config.LanguageConfidenceThreshold = 0.7
	} else {
		config.LanguageConfidenceThreshold = confidence
	}

	// Load accent-aware configuration
	config.AccentAwareModels = getEnvBool("DEEPGRAM_ACCENT_AWARE", true)
	config.FallbackLanguage = getEnv("DEEPGRAM_FALLBACK_LANGUAGE", "en-US")
	config.RealtimeLanguageSwitching = getEnvBool("DEEPGRAM_REALTIME_SWITCHING", false)
	config.LanguageSwitchingInterval = getEnvInt("DEEPGRAM_SWITCHING_INTERVAL", 5)
	config.MultiLanguageAlternatives = getEnvBool("DEEPGRAM_MULTILANG_ALTERNATIVES", false)
	config.MaxLanguageAlternatives = getEnvInt("DEEPGRAM_MAX_LANG_ALTERNATIVES", 3)

	// Validate accent detection configuration
	if config.DetectLanguage && len(config.SupportedLanguages) == 0 {
		logger.Warn("Language detection enabled but no supported languages specified")
		config.SupportedLanguages = []string{"en-US"}
	}

	if config.LanguageConfidenceThreshold < 0.0 || config.LanguageConfidenceThreshold > 1.0 {
		logger.Warn("Invalid language confidence threshold, using default: 0.7")
		config.LanguageConfidenceThreshold = 0.7
	}

	// Validate fallback language is in supported languages
	fallbackFound := false
	for _, lang := range config.SupportedLanguages {
		if lang == config.FallbackLanguage {
			fallbackFound = true
			break
		}
	}
	if !fallbackFound {
		logger.Warnf("Fallback language '%s' not in supported languages, adding it", config.FallbackLanguage)
		config.SupportedLanguages = append(config.SupportedLanguages, config.FallbackLanguage)
	}

	if config.Enabled && config.APIKey == "" {
		logger.Warn("Deepgram STT enabled but DEEPGRAM_API_KEY is not set")
	}

	// Log accent detection configuration if enabled
	if config.Enabled && config.DetectLanguage {
		logger.WithFields(logrus.Fields{
			"supported_languages":    config.SupportedLanguages,
			"confidence_threshold":   config.LanguageConfidenceThreshold,
			"accent_aware_models":    config.AccentAwareModels,
			"fallback_language":      config.FallbackLanguage,
			"realtime_switching":     config.RealtimeLanguageSwitching,
			"multilang_alternatives": config.MultiLanguageAlternatives,
		}).Info("Deepgram multi-language accent detection enabled")
	}

	return nil
}

// loadAzureSTTConfig loads Azure STT configuration
func loadAzureSTTConfig(logger *logrus.Logger, config *AzureSTTConfig) error {
	config.Enabled = getEnvBool("AZURE_STT_ENABLED", false)
	config.SubscriptionKey = getEnv("AZURE_SPEECH_KEY", "")
	config.Region = getEnv("AZURE_SPEECH_REGION", "")
	config.Language = getEnv("AZURE_STT_LANGUAGE", "en-US")
	config.EndpointURL = getEnv("AZURE_STT_ENDPOINT", "")
	config.EnableDetailedResults = getEnvBool("AZURE_STT_DETAILED_RESULTS", true)
	config.ProfanityFilter = getEnv("AZURE_STT_PROFANITY_FILTER", "masked")
	config.OutputFormat = getEnv("AZURE_STT_OUTPUT_FORMAT", "detailed")

	if config.Enabled && (config.SubscriptionKey == "" || config.Region == "") {
		logger.Warn("Azure STT enabled but AZURE_SPEECH_KEY or AZURE_SPEECH_REGION is not set")
	}

	return nil
}

// loadAmazonSTTConfig loads Amazon STT configuration
func loadAmazonSTTConfig(logger *logrus.Logger, config *AmazonSTTConfig) error {
	config.Enabled = getEnvBool("AMAZON_STT_ENABLED", false)
	config.AccessKeyID = getEnv("AWS_ACCESS_KEY_ID", "")
	config.SecretAccessKey = getEnv("AWS_SECRET_ACCESS_KEY", "")
	config.Region = getEnv("AWS_REGION", "us-east-1")
	config.Language = getEnv("AMAZON_STT_LANGUAGE", "en-US")
	config.MediaFormat = getEnv("AMAZON_STT_MEDIA_FORMAT", "wav")
	config.SampleRate = getEnvInt("AMAZON_STT_SAMPLE_RATE", 16000)
	config.VocabularyName = getEnv("AMAZON_STT_VOCABULARY", "")
	config.EnableChannelIdentification = getEnvBool("AMAZON_STT_CHANNEL_ID", false)
	config.EnableSpeakerIdentification = getEnvBool("AMAZON_STT_SPEAKER_ID", false)
	config.MaxSpeakerLabels = getEnvInt("AMAZON_STT_MAX_SPEAKERS", 2)

	if config.Enabled && (config.AccessKeyID == "" || config.SecretAccessKey == "") {
		logger.Warn("Amazon STT enabled but AWS_ACCESS_KEY_ID or AWS_SECRET_ACCESS_KEY is not set")
	}

	return nil
}

// loadOpenAISTTConfig loads OpenAI STT configuration
func loadOpenAISTTConfig(logger *logrus.Logger, config *OpenAISTTConfig) error {
	config.Enabled = getEnvBool("OPENAI_STT_ENABLED", false)
	config.APIKey = getEnv("OPENAI_API_KEY", "")
	config.OrganizationID = getEnv("OPENAI_ORGANIZATION_ID", "")
	config.Model = getEnv("OPENAI_STT_MODEL", "whisper-1")
	config.Language = getEnv("OPENAI_STT_LANGUAGE", "")
	config.Prompt = getEnv("OPENAI_STT_PROMPT", "")
	config.ResponseFormat = getEnv("OPENAI_STT_RESPONSE_FORMAT", "verbose_json")
	config.BaseURL = getEnv("OPENAI_BASE_URL", "https://api.openai.com/v1")

	// Parse temperature
	tempStr := getEnv("OPENAI_STT_TEMPERATURE", "0.0")
	temp, err := strconv.ParseFloat(tempStr, 64)
	if err != nil {
		logger.Warn("Invalid OPENAI_STT_TEMPERATURE value, using default: 0.0")
		config.Temperature = 0.0
	} else {
		config.Temperature = temp
	}

	if config.Enabled && config.APIKey == "" {
		logger.Warn("OpenAI STT enabled but OPENAI_API_KEY is not set")
	}

	return nil
}

// loadWhisperSTTConfig loads Whisper CLI configuration
func loadWhisperSTTConfig(logger *logrus.Logger, config *WhisperSTTConfig) error {
	config.Enabled = getEnvBool("WHISPER_ENABLED", false)
	config.BinaryPath = getEnv("WHISPER_BINARY_PATH", "whisper")
	config.Model = getEnv("WHISPER_MODEL", "base")
	config.Language = getEnv("WHISPER_LANGUAGE", "")
	config.Task = getEnv("WHISPER_TASK", "transcribe")
	config.Translate = getEnvBool("WHISPER_TRANSLATE", false)
	config.OutputFormat = getEnv("WHISPER_OUTPUT_FORMAT", "json")
	config.ExtraArgs = getEnv("WHISPER_EXTRA_ARGS", "")
	config.SampleRate = getEnvInt("WHISPER_SAMPLE_RATE", 16000)
	config.Channels = getEnvInt("WHISPER_CHANNELS", 1)

	timeoutStr := getEnv("WHISPER_TIMEOUT", "10m")
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		logger.Warn("Invalid WHISPER_TIMEOUT value, using default: 10m")
		timeout = 10 * time.Minute
	}
	config.Timeout = timeout

	config.MaxConcurrentCalls = getEnvInt("WHISPER_MAX_CONCURRENT", -1)

	return nil
}

// loadElevenLabsSTTConfig loads ElevenLabs STT configuration
func loadElevenLabsSTTConfig(logger *logrus.Logger, config *ElevenLabsSTTConfig) error {
	config.Enabled = getEnvBool("ELEVENLABS_STT_ENABLED", false)
	config.APIKey = getEnv("ELEVENLABS_API_KEY", "")
	config.BaseURL = getEnv("ELEVENLABS_API_URL", "https://api.elevenlabs.io")
	config.ModelID = getEnv("ELEVENLABS_MODEL_ID", "eleven_monolingual_v1")
	config.Language = getEnv("ELEVENLABS_LANGUAGE", "en")
	config.EnableDiarization = getEnvBool("ELEVENLABS_ENABLE_DIARIZATION", false)
	config.EnableTimestamps = getEnvBool("ELEVENLABS_ENABLE_TIMESTAMPS", true)
	config.EnablePunctuation = getEnvBool("ELEVENLABS_ENABLE_PUNCTUATION", true)
	config.EnableParagraphs = getEnvBool("ELEVENLABS_ENABLE_PARAGRAPHS", false)

	timeoutStr := getEnv("ELEVENLABS_TIMEOUT", "45s")
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		logger.Warn("Invalid ELEVENLABS_TIMEOUT value, using default: 45s")
		timeout = 45 * time.Second
	}
	config.Timeout = timeout

	if config.Enabled && config.APIKey == "" {
		logger.Warn("ElevenLabs STT enabled but ELEVENLABS_API_KEY is not set")
	}

	return nil
}

// loadSpeechmaticsSTTConfig loads Speechmatics STT configuration
func loadSpeechmaticsSTTConfig(logger *logrus.Logger, config *SpeechmaticsSTTConfig) error {
	config.Enabled = getEnvBool("SPEECHMATICS_STT_ENABLED", false)
	config.APIKey = getEnv("SPEECHMATICS_API_KEY", "")
	config.BaseURL = getEnv("SPEECHMATICS_API_URL", "https://asr.api.speechmatics.com/v2")
	config.Language = getEnv("SPEECHMATICS_LANGUAGE", "en-US")
	config.Model = getEnv("SPEECHMATICS_MODEL", "universal")
	config.EnableDiarization = getEnvBool("SPEECHMATICS_ENABLE_DIARIZATION", false)
	config.EnablePunctuation = getEnvBool("SPEECHMATICS_ENABLE_PUNCTUATION", true)
	config.EnableChannelSeparation = getEnvBool("SPEECHMATICS_CHANNEL_SEPARATION", false)

	timeoutStr := getEnv("SPEECHMATICS_TIMEOUT", "60s")
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		logger.Warn("Invalid SPEECHMATICS_TIMEOUT value, using default: 60s")
		timeout = 60 * time.Second
	}
	config.Timeout = timeout

	if config.Enabled && config.APIKey == "" {
		logger.Warn("Speechmatics STT enabled but SPEECHMATICS_API_KEY is not set")
	}

	return nil
}

// loadOpenSourceSTTConfig loads open-source STT model configuration
func loadOpenSourceSTTConfig(logger *logrus.Logger, config *OpenSourceSTTConfig) error {
	config.Enabled = getEnvBool("OPENSOURCE_STT_ENABLED", false)
	config.ModelType = getEnv("OPENSOURCE_MODEL_TYPE", "whisper-turbo")
	config.ModelName = getEnv("OPENSOURCE_MODEL_NAME", "openai/whisper-large-v3-turbo")
	config.ModelPath = getEnv("OPENSOURCE_MODEL_PATH", "")
	config.Backend = getEnv("OPENSOURCE_BACKEND", "http")
	config.BaseURL = getEnv("OPENSOURCE_BASE_URL", "http://localhost:8000")
	config.TranscribeEndpoint = getEnv("OPENSOURCE_TRANSCRIBE_ENDPOINT", "/stt/transcribe")
	config.WebSocketURL = getEnv("OPENSOURCE_WEBSOCKET_URL", "")
	config.UseMultilingual = getEnvBool("OPENSOURCE_USE_MULTILINGUAL", false)
	config.MultilingualWebSocketURL = getEnv("OPENSOURCE_MULTILINGUAL_WEBSOCKET_URL", "")
	config.APIKey = getEnv("OPENSOURCE_API_KEY", "")
	config.AuthHeader = getEnv("OPENSOURCE_AUTH_HEADER", "")
	config.SampleRate = getEnvInt("OPENSOURCE_SAMPLE_RATE", 16000)
	config.Encoding = getEnv("OPENSOURCE_ENCODING", "wav")
	config.Channels = getEnvInt("OPENSOURCE_CHANNELS", 1)
	config.Language = getEnv("OPENSOURCE_LANGUAGE", "en")
	config.UseGPU = getEnvBool("OPENSOURCE_USE_GPU", false)
	config.DeviceID = getEnvInt("OPENSOURCE_DEVICE_ID", 0)
	config.MaxRetries = getEnvInt("OPENSOURCE_MAX_RETRIES", 3)
	config.BatchSize = getEnvInt("OPENSOURCE_BATCH_SIZE", 1)
	config.EnableStreaming = getEnvBool("OPENSOURCE_ENABLE_STREAMING", false)
	config.ExecutablePath = getEnv("OPENSOURCE_EXECUTABLE_PATH", "")

	// Parse timeout
	timeoutStr := getEnv("OPENSOURCE_TIMEOUT", "60s")
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		logger.Warn("Invalid OPENSOURCE_TIMEOUT value, using default: 60s")
		timeout = 60 * time.Second
	}
	config.Timeout = timeout

	// Parse chunk duration
	chunkStr := getEnv("OPENSOURCE_CHUNK_DURATION", "5s")
	chunkDuration, err := time.ParseDuration(chunkStr)
	if err != nil {
		logger.Warn("Invalid OPENSOURCE_CHUNK_DURATION value, using default: 5s")
		chunkDuration = 5 * time.Second
	}
	config.ChunkDuration = chunkDuration

	// Parse extra args
	extraArgsStr := getEnv("OPENSOURCE_EXTRA_ARGS", "")
	if extraArgsStr != "" {
		config.ExtraArgs = strings.Split(extraArgsStr, " ")
	}

	if config.Enabled {
		logger.WithFields(logrus.Fields{
			"model_type": config.ModelType,
			"model_name": config.ModelName,
			"backend":    config.Backend,
			"base_url":   config.BaseURL,
			"use_gpu":    config.UseGPU,
		}).Info("Open-source STT model enabled")
	}

	return nil
}

// loadPIIConfig loads the PII detection configuration section
func loadPIIConfig(logger *logrus.Logger, config *PIIConfig) error {
	// Load enabled flag
	config.Enabled = getEnvBool("PII_DETECTION_ENABLED", false)

	// Load enabled types
	enabledTypesStr := getEnv("PII_ENABLED_TYPES", "ssn,credit_card")
	if enabledTypesStr == "" {
		config.EnabledTypes = []string{"ssn", "credit_card"}
	} else {
		types := strings.Split(enabledTypesStr, ",")
		for i, piiType := range types {
			types[i] = strings.TrimSpace(piiType)
		}
		config.EnabledTypes = types
	}

	// Validate enabled types
	validTypes := map[string]bool{
		"ssn":         true,
		"credit_card": true,
		"phone":       true,
		"email":       true,
	}
	var filteredTypes []string
	for _, piiType := range config.EnabledTypes {
		if validTypes[piiType] {
			filteredTypes = append(filteredTypes, piiType)
		} else {
			logger.WithField("type", piiType).Warn("Invalid PII type specified, ignoring")
		}
	}
	config.EnabledTypes = filteredTypes

	// Load redaction character
	config.RedactionChar = getEnv("PII_REDACTION_CHAR", "*")
	if len(config.RedactionChar) != 1 {
		logger.Warn("Invalid PII_REDACTION_CHAR, must be a single character, using default: *")
		config.RedactionChar = "*"
	}

	// Load preserve format flag
	config.PreserveFormat = getEnvBool("PII_PRESERVE_FORMAT", true)

	// Load context length
	config.ContextLength = getEnvInt("PII_CONTEXT_LENGTH", 20)
	if config.ContextLength < 0 || config.ContextLength > 100 {
		logger.Warn("Invalid PII_CONTEXT_LENGTH value, using default: 20")
		config.ContextLength = 20
	}

	// Load application flags
	config.ApplyToTranscriptions = getEnvBool("PII_APPLY_TO_TRANSCRIPTIONS", true)
	config.ApplyToRecordings = getEnvBool("PII_APPLY_TO_RECORDINGS", false)
	config.ApplyToCDR = getEnvBool("PII_APPLY_TO_CDR", false)

	// Load log level
	config.LogLevel = getEnv("PII_LOG_LEVEL", "info")
	if _, err := logrus.ParseLevel(config.LogLevel); err != nil {
		logger.Warnf("Invalid PII_LOG_LEVEL '%s', defaulting to 'info'", config.LogLevel)
		config.LogLevel = "info"
	}

	// Log configuration
	if config.Enabled {
		logger.WithFields(logrus.Fields{
			"enabled_types":           config.EnabledTypes,
			"redaction_char":          config.RedactionChar,
			"preserve_format":         config.PreserveFormat,
			"apply_to_transcriptions": config.ApplyToTranscriptions,
			"apply_to_recordings":     config.ApplyToRecordings,
			"apply_to_cdr":            config.ApplyToCDR,
			"log_level":               config.LogLevel,
		}).Info("PII detection enabled")
	} else {
		logger.Debug("PII detection disabled")
	}

	return nil
}

// loadAnalyticsConfig loads analytics persistence configuration
func loadAnalyticsConfig(logger *logrus.Logger, config *AnalyticsConfig) error {
	config.Enabled = getEnvBool("ANALYTICS_ENABLED", false)

	addressesStr := getEnv("ELASTICSEARCH_ADDRESSES", "http://localhost:9200")
	if addressesStr != "" {
		parts := strings.Split(addressesStr, ",")
		var addresses []string
		for _, addr := range parts {
			trimmed := strings.TrimSpace(addr)
			if trimmed != "" {
				addresses = append(addresses, trimmed)
			}
		}
		if len(addresses) > 0 {
			config.Elasticsearch.Addresses = addresses
		}
	}
	if len(config.Elasticsearch.Addresses) == 0 {
		config.Elasticsearch.Addresses = []string{"http://localhost:9200"}
	}

	config.Elasticsearch.Index = getEnv("ELASTICSEARCH_INDEX", "call-analytics")
	config.Elasticsearch.Username = getEnv("ELASTICSEARCH_USERNAME", "")
	config.Elasticsearch.Password = getEnv("ELASTICSEARCH_PASSWORD", "")

	timeoutStr := getEnv("ELASTICSEARCH_TIMEOUT", "10s")
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		logger.WithError(err).Warn("Invalid ELASTICSEARCH_TIMEOUT value, using default 10s")
		timeout = 10 * time.Second
	}
	config.Elasticsearch.Timeout = timeout

	if config.Enabled {
		logger.WithFields(logrus.Fields{
			"addresses": config.Elasticsearch.Addresses,
			"index":     config.Elasticsearch.Index,
		}).Info("Analytics persistence enabled")
	} else {
		logger.Debug("Analytics persistence disabled")
	}

	return nil
}

func loadDatabaseConfig(logger *logrus.Logger, config *DatabaseConfig) error {
	config.Enabled = getEnvBool("DATABASE_ENABLED", false)

	if config.Enabled {
		logger.Info("Database persistence enabled")
	} else {
		logger.Debug("Database persistence disabled")
	}

	return nil
}

func loadComplianceConfig(logger *logrus.Logger, config *ComplianceConfig) error {
	if config == nil {
		return nil
	}

	// Apply defaults regardless of whether features are enabled
	if config.GDPR.ExportDir == "" {
		config.GDPR.ExportDir = "./exports"
	}
	if config.Audit.LogPath == "" {
		config.Audit.LogPath = "./logs/audit-chain.log"
	}

	if config.PCI.Enabled {
		logger.Info("PCI compliance mode enabled")
	}

	if config.GDPR.Enabled {
		logger.WithField("export_dir", config.GDPR.ExportDir).Info("GDPR tools enabled")
	}

	if config.Audit.TamperProof {
		logger.WithField("log_path", config.Audit.LogPath).Info("Tamper-proof audit logging enabled")
	}

	return nil
}

// loadAuthConfig loads authentication and authorization configuration
func loadAuthConfig(logger *logrus.Logger, config *AuthConfig) error {
	// Load enabled flag
	config.Enabled = getEnvBool("AUTH_ENABLED", false)

	// Load JWT secret - required if auth is enabled
	config.JWTSecret = getEnv("AUTH_JWT_SECRET", "")
	if config.Enabled && config.JWTSecret == "" {
		return errors.New("AUTH_ENABLED is true but AUTH_JWT_SECRET is not set; configure a strong secret")
	}

	// Load JWT issuer
	config.JWTIssuer = getEnv("AUTH_JWT_ISSUER", "siprec-server")

	// Load token expiry
	tokenExpiryStr := getEnv("AUTH_TOKEN_EXPIRY", "24h")
	var err error
	config.TokenExpiry, err = time.ParseDuration(tokenExpiryStr)
	if err != nil {
		logger.Warnf("Invalid AUTH_TOKEN_EXPIRY '%s', defaulting to 24h", tokenExpiryStr)
		config.TokenExpiry = 24 * time.Hour
	}

	// Load API key authentication flag
	config.EnableAPIKeys = getEnvBool("AUTH_ENABLE_API_KEYS", true)

	// Load admin credentials
	config.AdminUsername = getEnv("AUTH_ADMIN_USERNAME", "admin")
	config.AdminPassword = getEnv("AUTH_ADMIN_PASSWORD", "")

	// Log configuration
	if config.Enabled {
		logger.WithFields(logrus.Fields{
			"jwt_issuer":         config.JWTIssuer,
			"token_expiry":       config.TokenExpiry.String(),
			"enable_api_keys":    config.EnableAPIKeys,
			"admin_username":     config.AdminUsername,
			"has_admin_password": config.AdminPassword != "",
		}).Info("Authentication enabled")
	} else {
		logger.Debug("Authentication disabled")
	}

	// Load SIP authentication configuration
	config.SIP.Enabled = getEnvBool("SIP_AUTH_ENABLED", false)
	config.SIP.Realm = getEnv("SIP_AUTH_REALM", "siprec.local")
	config.SIP.NonceTimeout = getEnvInt("SIP_AUTH_NONCE_TIMEOUT", 300)
	config.SIP.Users = getEnv("SIP_AUTH_USERS", "")

	// Load IP-based access control configuration
	config.SIP.IPAccess.Enabled = getEnvBool("SIP_IP_ACCESS_ENABLED", false)
	config.SIP.IPAccess.DefaultAllow = getEnvBool("SIP_IP_DEFAULT_ALLOW", true)
	config.SIP.IPAccess.AllowedIPs = getEnv("SIP_IP_ALLOWED_IPS", "")
	config.SIP.IPAccess.AllowedNetworks = getEnv("SIP_IP_ALLOWED_NETWORKS", "")
	config.SIP.IPAccess.BlockedIPs = getEnv("SIP_IP_BLOCKED_IPS", "")
	config.SIP.IPAccess.BlockedNetworks = getEnv("SIP_IP_BLOCKED_NETWORKS", "")

	// Log SIP auth configuration
	if config.SIP.Enabled {
		userCount := 0
		if config.SIP.Users != "" {
			userCount = len(strings.Split(config.SIP.Users, ","))
		}
		logger.WithFields(logrus.Fields{
			"realm":         config.SIP.Realm,
			"nonce_timeout": config.SIP.NonceTimeout,
			"user_count":    userCount,
		}).Info("SIP Digest authentication enabled")
	}

	if config.SIP.IPAccess.Enabled {
		logger.WithFields(logrus.Fields{
			"default_allow":    config.SIP.IPAccess.DefaultAllow,
			"allowed_ips":      config.SIP.IPAccess.AllowedIPs != "",
			"allowed_networks": config.SIP.IPAccess.AllowedNetworks != "",
			"blocked_ips":      config.SIP.IPAccess.BlockedIPs != "",
			"blocked_networks": config.SIP.IPAccess.BlockedNetworks != "",
		}).Info("SIP IP-based access control enabled")
	}

	return nil
}

// loadAlertingConfig loads alerting system configuration
func loadAlertingConfig(logger *logrus.Logger, config *AlertingConfig) error {
	// Load enabled flag
	config.Enabled = getEnvBool("ALERTING_ENABLED", false)

	// Load evaluation interval
	intervalStr := getEnv("ALERTING_EVALUATION_INTERVAL", "30s")
	var err error
	config.EvaluationInterval, err = time.ParseDuration(intervalStr)
	if err != nil {
		logger.Warnf("Invalid ALERTING_EVALUATION_INTERVAL '%s', defaulting to 30s", intervalStr)
		config.EvaluationInterval = 30 * time.Second
	}

	// Log configuration
	if config.Enabled {
		logger.WithField("evaluation_interval", config.EvaluationInterval).Info("Alerting system enabled")
		logger.Warn("Alerting is enabled but no alert rules or channels are configured - alerts will not be sent")
	} else {
		logger.Debug("Alerting system disabled")
	}

	return nil
}

// loadRateLimitConfig loads rate limiting configuration
func loadRateLimitConfig(logger *logrus.Logger, config *RateLimitConfig) error {
	// Load HTTP rate limiting settings
	config.Enabled = getEnvBool("RATE_LIMIT_ENABLED", false)
	config.RequestsPerSecond = getEnvFloat("RATE_LIMIT_RPS", 100)
	config.BurstSize = getEnvInt("RATE_LIMIT_BURST", 200)

	// Load block duration
	blockDurationStr := getEnv("RATE_LIMIT_BLOCK_DURATION", "1m")
	var err error
	config.BlockDuration, err = time.ParseDuration(blockDurationStr)
	if err != nil {
		logger.Warnf("Invalid RATE_LIMIT_BLOCK_DURATION '%s', defaulting to 1m", blockDurationStr)
		config.BlockDuration = time.Minute
	}

	// Load whitelisted IPs and paths
	config.WhitelistedIPs = getEnv("RATE_LIMIT_WHITELIST_IPS", "127.0.0.1,::1")
	config.WhitelistedPaths = getEnv("RATE_LIMIT_WHITELIST_PATHS", "/health,/health/live,/health/ready")

	// Load SIP rate limiting settings
	config.SIPEnabled = getEnvBool("RATE_LIMIT_SIP_ENABLED", false)
	config.SIPInvitesPerSecond = getEnvFloat("RATE_LIMIT_SIP_INVITE_RPS", 10)
	config.SIPInviteBurst = getEnvInt("RATE_LIMIT_SIP_INVITE_BURST", 50)
	config.SIPRequestsPerSecond = getEnvFloat("RATE_LIMIT_SIP_RPS", 100)
	config.SIPRequestBurst = getEnvInt("RATE_LIMIT_SIP_REQUEST_BURST", 200)

	// Log configuration
	if config.Enabled {
		logger.WithFields(logrus.Fields{
			"rps":   config.RequestsPerSecond,
			"burst": config.BurstSize,
			"block": config.BlockDuration,
		}).Info("HTTP rate limiting enabled")
	}

	if config.SIPEnabled {
		logger.WithFields(logrus.Fields{
			"invite_rps":    config.SIPInvitesPerSecond,
			"invite_burst":  config.SIPInviteBurst,
			"request_rps":   config.SIPRequestsPerSecond,
			"request_burst": config.SIPRequestBurst,
		}).Info("SIP rate limiting enabled")
	}

	return nil
}
