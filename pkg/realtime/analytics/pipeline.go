package analytics

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Processor defines a stage in the analytics pipeline.
type Processor interface {
	Process(ctx context.Context, event *TranscriptEvent, state *State) error
}

// State holds per-call analytics state.
type State struct {
	CallID string

	SentimentTrend      []SentimentResult
	Keywords            map[string]int
	Topics              map[string]int
	Violations          []ComplianceViolation
	Metrics             AgentMetrics
	QualityScore        float64
	ComplianceRules     map[string]ComplianceRule
	ComplianceSatisfied map[string]bool
	Audio               AudioMetrics
	AcousticEvents      []AcousticEvent

	// Vendor-specific metadata for Elasticsearch indexing
	VendorType           string
	OracleUCID           string
	OracleConversationID string
	CiscoSessionID       string
	AvayaUCID            string
	UCID                 string

	LastUpdated time.Time
}

// Clone returns a copy of the state for read-only usage.
func (s *State) Clone() AnalyticsSnapshot {
	keywords := make([]string, 0, len(s.Keywords))
	for k := range s.Keywords {
		keywords = append(keywords, k)
	}

	topics := make([]string, 0, len(s.Topics))
	for k := range s.Topics {
		topics = append(topics, k)
	}

	trendCopy := make([]SentimentResult, len(s.SentimentTrend))
	copy(trendCopy, s.SentimentTrend)

	violationsCopy := make([]ComplianceViolation, len(s.Violations))
	copy(violationsCopy, s.Violations)

	eventsCopy := make([]AcousticEvent, len(s.AcousticEvents))
	copy(eventsCopy, s.AcousticEvents)

	return AnalyticsSnapshot{
		CallID:               s.CallID,
		SentimentTrend:       trendCopy,
		Keywords:             keywords,
		Topics:               topics,
		Compliance:           violationsCopy,
		Metrics:              s.Metrics,
		QualityScore:         s.QualityScore,
		UpdatedAt:            s.LastUpdated,
		Audio:                s.Audio,
		Events:               eventsCopy,
		VendorType:           s.VendorType,
		OracleUCID:           s.OracleUCID,
		OracleConversationID: s.OracleConversationID,
		CiscoSessionID:       s.CiscoSessionID,
		AvayaUCID:            s.AvayaUCID,
		UCID:                 s.UCID,
	}
}

// Pipeline orchestrates ordered processor execution and state management.
type Pipeline struct {
	logger     *logrus.Logger
	processors []Processor
	store      StateStore
}

// StateStore abstracts per-call state persistence (e.g., Redis).
type StateStore interface {
	Get(callID string) (*State, error)
	Set(callID string, state *State) error
	Delete(callID string) error
}

// InMemoryStateStore provides simple state storage for initial implementation.
type InMemoryStateStore struct {
	mu    sync.RWMutex
	items map[string]*State
}

// NewInMemoryStateStore creates a new in-memory store.
func NewInMemoryStateStore() *InMemoryStateStore {
	return &InMemoryStateStore{items: make(map[string]*State)}
}

func (s *InMemoryStateStore) Get(callID string) (*State, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.items[callID]
	if !ok {
		return nil, nil
	}
	// Clone the state to avoid external mutation.
	clone := *state
	return &clone, nil
}

func (s *InMemoryStateStore) Set(callID string, state *State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *state
	s.items[callID] = &copy
	return nil
}

func (s *InMemoryStateStore) Delete(callID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, callID)
	return nil
}

// NewPipeline constructs a pipeline with processors and state store.
func NewPipeline(logger *logrus.Logger, store StateStore, processors ...Processor) *Pipeline {
	if store == nil {
		store = NewInMemoryStateStore()
	}
	return &Pipeline{
		logger:     logger,
		processors: processors,
		store:      store,
	}
}

// Process executes the pipeline for a transcript event.
func (p *Pipeline) Process(ctx context.Context, event *TranscriptEvent) (*AnalyticsSnapshot, error) {
	if event == nil {
		return nil, nil
	}

	state, err := p.store.Get(event.CallID)
	if err != nil {
		return nil, err
	}
	if state == nil {
		state = &State{
			CallID:              event.CallID,
			SentimentTrend:      make([]SentimentResult, 0, 32),
			Keywords:            make(map[string]int),
			Topics:              make(map[string]int),
			Violations:          make([]ComplianceViolation, 0, 8),
			Metrics:             AgentMetrics{},
			ComplianceRules:     make(map[string]ComplianceRule),
			ComplianceSatisfied: make(map[string]bool),
			AcousticEvents:      make([]AcousticEvent, 0, 16),
			LastUpdated:         time.Now(),
		}
	}

	// Extract vendor metadata from event metadata (injected by TranscriptionService)
	if event.Metadata != nil {
		if v, ok := event.Metadata["sip_vendor_type"].(string); ok && state.VendorType == "" {
			state.VendorType = v
		}
		if v, ok := event.Metadata["sip_oracle_ucid"].(string); ok && state.OracleUCID == "" {
			state.OracleUCID = v
		}
		if v, ok := event.Metadata["sip_oracle_conversation_id"].(string); ok && state.OracleConversationID == "" {
			state.OracleConversationID = v
		}
		if v, ok := event.Metadata["sip_cisco_session_id"].(string); ok && state.CiscoSessionID == "" {
			state.CiscoSessionID = v
		}
		if v, ok := event.Metadata["sip_ucid"].(string); ok && state.UCID == "" {
			state.UCID = v
		}
		// Check for Avaya UCID
		if state.VendorType == "avaya" && state.UCID != "" && state.AvayaUCID == "" {
			state.AvayaUCID = state.UCID
		}
	}

	for _, processor := range p.processors {
		if err := processor.Process(ctx, event, state); err != nil {
			p.logger.WithError(err).WithField("call_id", event.CallID).Warn("Analytics processor failed")
		}
	}

	state.LastUpdated = time.Now()

	if err := p.store.Set(event.CallID, state); err != nil {
		return nil, err
	}

	snapshot := state.Clone()
	return &snapshot, nil
}

// CompleteCall finalizes analytics for a call and removes state.
func (p *Pipeline) CompleteCall(callID string) (*AnalyticsSnapshot, error) {
	state, err := p.store.Get(callID)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, nil
	}

	if err := p.store.Delete(callID); err != nil {
		return nil, err
	}

	if state.ComplianceRules != nil {
		for id, rule := range state.ComplianceRules {
			if len(rule.Contains) == 0 {
				continue
			}
			if state.ComplianceSatisfied != nil && state.ComplianceSatisfied[id] {
				continue
			}
			state.Violations = append(state.Violations, ComplianceViolation{
				RuleID:      rule.ID,
				Description: rule.Description,
				Severity:    rule.Severity,
				Timestamp:   time.Now(),
			})
		}
	}

	snapshot := state.Clone()
	return &snapshot, nil
}

// ProcessAudioMetrics updates the analytics state with audio metrics and acoustic events.
func (p *Pipeline) ProcessAudioMetrics(callID string, metrics *AudioMetrics, events []AcousticEvent) (*AnalyticsSnapshot, error) {
	if metrics == nil && len(events) == 0 {
		return nil, nil
	}

	state, err := p.store.Get(callID)
	if err != nil {
		return nil, err
	}
	if state == nil {
		state = &State{
			CallID:         callID,
			SentimentTrend: make([]SentimentResult, 0, 32),
			Keywords:       make(map[string]int),
			Topics:         make(map[string]int),
			Violations:     make([]ComplianceViolation, 0, 8),
			Metrics:        AgentMetrics{},
			AcousticEvents: make([]AcousticEvent, 0, 16),
		}
	}

	if metrics != nil {
		state.Audio = *metrics
		state.LastUpdated = time.Now()
	}

	if len(events) > 0 {
		state.AcousticEvents = append(state.AcousticEvents, events...)
		if len(state.AcousticEvents) > 32 {
			state.AcousticEvents = state.AcousticEvents[len(state.AcousticEvents)-32:]
		}
	}

	if err := p.store.Set(callID, state); err != nil {
		return nil, err
	}

	snapshot := state.Clone()
	return &snapshot, nil
}
