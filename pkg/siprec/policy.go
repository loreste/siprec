package siprec

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// PolicyAction defines what action a policy mandates
type PolicyAction string

const (
	// PolicyActionRecord indicates recording should proceed
	PolicyActionRecord PolicyAction = "record"
	// PolicyActionNoRecord indicates recording should not proceed
	PolicyActionNoRecord PolicyAction = "no-record"
	// PolicyActionSelective indicates selective recording based on criteria
	PolicyActionSelective PolicyAction = "selective"
	// PolicyActionPause indicates recording should be paused
	PolicyActionPause PolicyAction = "pause"
	// PolicyActionResume indicates recording should resume
	PolicyActionResume PolicyAction = "resume"
)

// PolicyDecision represents the outcome of policy evaluation
type PolicyDecision struct {
	Action       PolicyAction
	PolicyID     string
	Reason       string
	AllowAudio   bool
	AllowVideo   bool
	AllowText    bool
	RetentionDays int
	Priority     int
	Timestamp    time.Time
}

// PolicyRule defines a recording policy rule
type PolicyRule struct {
	ID              string
	Name            string
	Priority        int
	Action          PolicyAction
	MatchCriteria   PolicyCriteria
	MediaFilter     MediaFilter
	RetentionDays   int
	RequireConsent  bool
	NotifyOnRecord  bool
	Enabled         bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// PolicyCriteria defines matching criteria for a policy
type PolicyCriteria struct {
	// Participant matching
	ParticipantAOR     []string // Match by AOR patterns (supports wildcards)
	ParticipantRole    []string // Match by role (active, passive, focus)

	// Session matching
	SessionType        []string // Match by session type
	Direction          []string // inbound, outbound, both

	// Time-based criteria
	TimeRanges         []TimeRange // Only apply during these time ranges

	// Custom metadata matching
	MetadataPatterns   map[string]string // Key-value patterns to match
}

// MediaFilter defines which media types to record
type MediaFilter struct {
	AllowAudio   bool
	AllowVideo   bool
	AllowText    bool
	AllowMessage bool
}

// TimeRange defines a time-based policy application window
type TimeRange struct {
	StartHour int // 0-23
	EndHour   int // 0-23
	Days      []time.Weekday
}

// PolicyManager manages recording policies and their enforcement
type PolicyManager struct {
	mu              sync.RWMutex
	rules           map[string]*PolicyRule
	sessionPolicies map[string]*PolicyDecision // sessionID -> decision
	ackStates       map[string]map[string]*PolicyAckStatus // sessionID -> policyID -> status
	defaultAction   PolicyAction
}

// NewPolicyManager creates a new policy manager
func NewPolicyManager(defaultAction PolicyAction) *PolicyManager {
	if defaultAction == "" {
		defaultAction = PolicyActionRecord // Default to recording
	}
	return &PolicyManager{
		rules:           make(map[string]*PolicyRule),
		sessionPolicies: make(map[string]*PolicyDecision),
		ackStates:       make(map[string]map[string]*PolicyAckStatus),
		defaultAction:   defaultAction,
	}
}

// AddRule adds a new policy rule
func (pm *PolicyManager) AddRule(rule *PolicyRule) error {
	if rule == nil {
		return fmt.Errorf("rule cannot be nil")
	}
	if rule.ID == "" {
		return fmt.Errorf("rule ID cannot be empty")
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	rule.CreatedAt = time.Now()
	rule.UpdatedAt = time.Now()
	pm.rules[rule.ID] = rule
	return nil
}

// RemoveRule removes a policy rule
func (pm *PolicyManager) RemoveRule(ruleID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.rules, ruleID)
}

// GetRule retrieves a policy rule by ID
func (pm *PolicyManager) GetRule(ruleID string) *PolicyRule {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.rules[ruleID]
}

// EvaluateSession evaluates policies for a recording session and returns a decision
func (pm *PolicyManager) EvaluateSession(session *RecordingSession, metadata *RSMetadata) *PolicyDecision {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// Check cached decision
	if session != nil && session.ID != "" {
		if cached, ok := pm.sessionPolicies[session.ID]; ok {
			return cached
		}
	}

	// Collect all matching rules sorted by priority
	matchingRules := make([]*PolicyRule, 0)
	for _, rule := range pm.rules {
		if rule.Enabled && pm.ruleMatches(rule, session, metadata) {
			matchingRules = append(matchingRules, rule)
		}
	}

	// Sort by priority (higher priority wins)
	for i := 0; i < len(matchingRules)-1; i++ {
		for j := i + 1; j < len(matchingRules); j++ {
			if matchingRules[j].Priority > matchingRules[i].Priority {
				matchingRules[i], matchingRules[j] = matchingRules[j], matchingRules[i]
			}
		}
	}

	// Build decision from highest priority matching rule
	decision := &PolicyDecision{
		Action:      pm.defaultAction,
		AllowAudio:  true,
		AllowVideo:  true,
		AllowText:   true,
		Timestamp:   time.Now(),
	}

	if len(matchingRules) > 0 {
		rule := matchingRules[0]
		decision.Action = rule.Action
		decision.PolicyID = rule.ID
		decision.Reason = rule.Name
		decision.AllowAudio = rule.MediaFilter.AllowAudio
		decision.AllowVideo = rule.MediaFilter.AllowVideo
		decision.AllowText = rule.MediaFilter.AllowText
		decision.RetentionDays = rule.RetentionDays
		decision.Priority = rule.Priority
	}

	// Cache the decision
	if session != nil && session.ID != "" {
		pm.mu.RUnlock()
		pm.mu.Lock()
		pm.sessionPolicies[session.ID] = decision
		pm.mu.Unlock()
		pm.mu.RLock()
	}

	return decision
}

// ruleMatches checks if a rule matches the session/metadata
func (pm *PolicyManager) ruleMatches(rule *PolicyRule, session *RecordingSession, metadata *RSMetadata) bool {
	criteria := rule.MatchCriteria

	// Check time ranges
	if len(criteria.TimeRanges) > 0 {
		now := time.Now()
		inRange := false
		for _, tr := range criteria.TimeRanges {
			if pm.isInTimeRange(now, tr) {
				inRange = true
				break
			}
		}
		if !inRange {
			return false
		}
	}

	// Check participant AOR patterns
	if len(criteria.ParticipantAOR) > 0 && metadata != nil {
		matched := false
		for _, p := range metadata.Participants {
			for _, aor := range p.Aor {
				for _, pattern := range criteria.ParticipantAOR {
					if pm.matchPattern(aor.Value, pattern) || pm.matchPattern(aor.URI, pattern) {
						matched = true
						break
					}
				}
			}
		}
		if !matched {
			return false
		}
	}

	// Check participant roles
	if len(criteria.ParticipantRole) > 0 && metadata != nil {
		matched := false
		for _, p := range metadata.Participants {
			role := strings.ToLower(strings.TrimSpace(p.Role))
			for _, targetRole := range criteria.ParticipantRole {
				if role == strings.ToLower(targetRole) {
					matched = true
					break
				}
			}
		}
		if !matched {
			return false
		}
	}

	// Check direction
	if len(criteria.Direction) > 0 && session != nil {
		matched := false
		for _, dir := range criteria.Direction {
			if strings.EqualFold(session.Direction, dir) || dir == "both" {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check session type
	if len(criteria.SessionType) > 0 && session != nil {
		matched := false
		for _, stype := range criteria.SessionType {
			if strings.EqualFold(session.RecordingType, stype) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

// isInTimeRange checks if a time falls within a time range
func (pm *PolicyManager) isInTimeRange(t time.Time, tr TimeRange) bool {
	hour := t.Hour()
	weekday := t.Weekday()

	// Check day
	if len(tr.Days) > 0 {
		dayMatch := false
		for _, d := range tr.Days {
			if d == weekday {
				dayMatch = true
				break
			}
		}
		if !dayMatch {
			return false
		}
	}

	// Check hour range
	if tr.StartHour <= tr.EndHour {
		return hour >= tr.StartHour && hour < tr.EndHour
	}
	// Handle overnight ranges (e.g., 22:00 - 06:00)
	return hour >= tr.StartHour || hour < tr.EndHour
}

// matchPattern performs simple pattern matching (supports * wildcard)
func (pm *PolicyManager) matchPattern(value, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") {
		return strings.Contains(value, pattern[1:len(pattern)-1])
	}
	if strings.HasPrefix(pattern, "*") {
		return strings.HasSuffix(value, pattern[1:])
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(value, pattern[:len(pattern)-1])
	}
	return strings.EqualFold(value, pattern)
}

// ProcessPolicyUpdate handles a policy update from metadata
func (pm *PolicyManager) ProcessPolicyUpdate(sessionID string, update PolicyUpdate) *PolicyAckStatus {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.ackStates[sessionID] == nil {
		pm.ackStates[sessionID] = make(map[string]*PolicyAckStatus)
	}

	ackStatus := &PolicyAckStatus{
		Status:       update.Status,
		Acknowledged: update.Acknowledged,
		ReportedAt:   time.Now(),
		RawTimestamp: update.Timestamp,
	}

	pm.ackStates[sessionID][update.PolicyID] = ackStatus
	return ackStatus
}

// GetPolicyAckStatus retrieves the acknowledgement status for a policy in a session
func (pm *PolicyManager) GetPolicyAckStatus(sessionID, policyID string) *PolicyAckStatus {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if sessionAcks, ok := pm.ackStates[sessionID]; ok {
		return sessionAcks[policyID]
	}
	return nil
}

// AcknowledgePolicy marks a policy as acknowledged for a session
func (pm *PolicyManager) AcknowledgePolicy(sessionID, policyID, status string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.ackStates[sessionID] == nil {
		pm.ackStates[sessionID] = make(map[string]*PolicyAckStatus)
	}

	pm.ackStates[sessionID][policyID] = &PolicyAckStatus{
		Status:       status,
		Acknowledged: true,
		ReportedAt:   time.Now(),
	}
}

// ClearSessionPolicies removes cached policy decisions for a session
func (pm *PolicyManager) ClearSessionPolicies(sessionID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.sessionPolicies, sessionID)
	delete(pm.ackStates, sessionID)
}

// ShouldRecord returns true if recording should proceed based on policy decision
func (decision *PolicyDecision) ShouldRecord() bool {
	return decision.Action == PolicyActionRecord || decision.Action == PolicyActionSelective
}

// ShouldPause returns true if recording should be paused
func (decision *PolicyDecision) ShouldPause() bool {
	return decision.Action == PolicyActionPause
}

// IsBlocked returns true if recording is blocked by policy
func (decision *PolicyDecision) IsBlocked() bool {
	return decision.Action == PolicyActionNoRecord
}

// GeneratePolicyUpdateResponse generates a policy update response for metadata
func (pm *PolicyManager) GeneratePolicyUpdateResponse(sessionID string, policyIDs []string) []PolicyUpdate {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	updates := make([]PolicyUpdate, 0, len(policyIDs))
	sessionAcks := pm.ackStates[sessionID]

	for _, policyID := range policyIDs {
		update := PolicyUpdate{
			PolicyID:     policyID,
			Status:       "acknowledged",
			Acknowledged: true,
			Timestamp:    time.Now().UTC().Format(time.RFC3339),
		}

		// Check if we have existing ack state
		if sessionAcks != nil {
			if ack, ok := sessionAcks[policyID]; ok {
				update.Status = ack.Status
				update.Acknowledged = ack.Acknowledged
			}
		}

		updates = append(updates, update)
	}

	return updates
}

// ValidatePolicyRequirements checks if a session meets policy requirements
func (pm *PolicyManager) ValidatePolicyRequirements(session *RecordingSession, metadata *RSMetadata) []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	violations := make([]string, 0)

	for _, rule := range pm.rules {
		if !rule.Enabled {
			continue
		}

		// Check consent requirement
		if rule.RequireConsent && session != nil {
			consentObtained := false
			for _, p := range session.Participants {
				if p.ConsentObtained {
					consentObtained = true
					break
				}
			}
			if !consentObtained && pm.ruleMatches(rule, session, metadata) {
				violations = append(violations, fmt.Sprintf("policy %s requires consent but none obtained", rule.ID))
			}
		}
	}

	return violations
}
