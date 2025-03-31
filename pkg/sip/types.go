package sip

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/sirupsen/logrus"
	"siprec-server/pkg/media"
	"siprec-server/pkg/siprec"
)

// Handler provides SIP protocol handling for the SIPREC server
type Handler struct {
	Logger  *logrus.Logger
	Server  *sipgo.Server
	UA      *sipgo.UserAgent
	Config  *Config
	
	// Active calls storage
	ActiveCalls *sync.Map
	
	// STT provider function
	SttProvider func(context.Context, string, io.Reader, string) error

	// Session persistence store - used for redundancy
	SessionStore SessionStore
	
	// Shutdown resources
	monitorCtx       context.Context
	monitorCancel    context.CancelFunc
	sessionMonitorWG sync.WaitGroup
}

// Config defines SIP handler configuration
type Config struct {
	// General configuration
	MaxConcurrentCalls int
	
	// Media configuration
	MediaConfig *media.Config

	// Session redundancy configuration
	RedundancyEnabled  bool
	StateCheckInterval time.Duration    // How often to check session state
	SessionTimeout     time.Duration    // How long before a session is considered stale
}

// NewHandler creates a new SIP handler
func NewHandler(logger *logrus.Logger, config *Config, sttProvider func(context.Context, string, io.Reader, string) error) (*Handler, error) {
	ua, err := sipgo.NewUA()
	if err != nil {
		return nil, err
	}
	
	server, err := sipgo.NewServer(ua)
	if err != nil {
		return nil, err
	}
	
	// Set defaults for redundancy config if not provided
	if config.StateCheckInterval == 0 {
		config.StateCheckInterval = 10 * time.Second
	}
	if config.SessionTimeout == 0 {
		config.SessionTimeout = 30 * time.Second
	}
	
	// Create and initialize session store based on config
	var sessionStore SessionStore
	if config.RedundancyEnabled {
		sessionStore = NewInMemorySessionStore()
	} else {
		sessionStore = NewNoOpSessionStore()
	}
	
	// Create a context for monitoring sessions
	monitorCtx, monitorCancel := context.WithCancel(context.Background())
	
	handler := &Handler{
		Logger:         logger,
		Server:         server,
		UA:             ua,
		Config:         config,
		ActiveCalls:    &sync.Map{},
		SttProvider:    sttProvider,
		SessionStore:   sessionStore,
		monitorCtx:     monitorCtx,
		monitorCancel:  monitorCancel,
	}
	
	// Start session monitoring if redundancy is enabled
	if config.RedundancyEnabled {
		handler.sessionMonitorWG.Add(1)
		go func() {
			defer handler.sessionMonitorWG.Done()
			handler.monitorSessions()
		}()
	}
	
	return handler, nil
}

// CallData holds data for an active call
type CallData struct {
	CallUUID         string
	Context          context.Context
	CancelFunc       context.CancelFunc
	Forwarder        *media.RTPForwarder
	RecordingSession *siprec.RecordingSession
	LastActivity     time.Time        // Used for session monitoring
	Sequence         int              // Used for message ordering in redundancy
	State            string           // Current call state: "establishing", "active", "terminating"
	RemoteAddress    string           // Remote IP:port for reconnection
	DialogInfo       *DialogInfo      // SIP dialog information for reconnection
}

// DialogInfo stores SIP dialog information needed for session resumption
type DialogInfo struct {
	LocalTag     string
	RemoteTag    string
	LocalURI     string
	RemoteURI    string
	CallID       string
	LocalSeq     int
	RemoteSeq    int
	RouteSet     []string
	Contact      string
}

// NewCallData creates a new call data structure
func NewCallData(ctx context.Context, cancelFunc context.CancelFunc, callUUID string) *CallData {
	return &CallData{
		CallUUID:     callUUID,
		Context:      ctx,
		CancelFunc:   cancelFunc,
		LastActivity: time.Now(),
		State:        "establishing",
		Sequence:     1,
	}
}

// UpdateActivity marks the call as active with current timestamp
func (c *CallData) UpdateActivity() {
	c.LastActivity = time.Now()
}

// IsStale checks if the call has been inactive too long
func (c *CallData) IsStale(timeout time.Duration) bool {
	return time.Since(c.LastActivity) > timeout
}

// SessionStore defines an interface for session persistence
type SessionStore interface {
	// Save stores call data for redundancy
	Save(callUUID string, data *CallData) error
	
	// Load retrieves call data by UUID
	Load(callUUID string) (*CallData, error)
	
	// Delete removes call data from storage
	Delete(callUUID string) error
	
	// List returns all active call UUIDs
	List() ([]string, error)
}

// InMemorySessionStore implements session storage in memory
type InMemorySessionStore struct {
	data sync.Map
}

// NewInMemorySessionStore creates a new in-memory session store
func NewInMemorySessionStore() *InMemorySessionStore {
	return &InMemorySessionStore{}
}

// Save stores call data in memory
func (s *InMemorySessionStore) Save(callUUID string, data *CallData) error {
	s.data.Store(callUUID, data)
	return nil
}

// Load retrieves call data from memory
func (s *InMemorySessionStore) Load(callUUID string) (*CallData, error) {
	if data, ok := s.data.Load(callUUID); ok {
		return data.(*CallData), nil
	}
	return nil, ErrSessionNotFound
}

// Delete removes call data from memory
func (s *InMemorySessionStore) Delete(callUUID string) error {
	s.data.Delete(callUUID)
	return nil
}

// List returns all call UUIDs
func (s *InMemorySessionStore) List() ([]string, error) {
	var callUUIDs []string
	s.data.Range(func(key, value interface{}) bool {
		callUUIDs = append(callUUIDs, key.(string))
		return true
	})
	return callUUIDs, nil
}

// NoOpSessionStore implements a no-op session store when redundancy is disabled
type NoOpSessionStore struct{}

// NewNoOpSessionStore creates a new no-op session store
func NewNoOpSessionStore() *NoOpSessionStore {
	return &NoOpSessionStore{}
}

// Save is a no-op
func (s *NoOpSessionStore) Save(callUUID string, data *CallData) error {
	return nil
}

// Load always returns not found
func (s *NoOpSessionStore) Load(callUUID string) (*CallData, error) {
	return nil, ErrSessionNotFound
}

// Delete is a no-op
func (s *NoOpSessionStore) Delete(callUUID string) error {
	return nil
}

// List returns an empty slice
func (s *NoOpSessionStore) List() ([]string, error) {
	return []string{}, nil
}

// Errors for session management
var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionStale    = errors.New("session too old to resume")
)