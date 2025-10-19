package analytics

import (
	"context"
	"strings"
	"time"
)

// SentimentProcessor provides basic sentiment scoring using simple heuristics.
type SentimentProcessor struct {
	positiveWords map[string]float64
	negativeWords map[string]float64
	decay         time.Duration
}

// NewSentimentProcessor creates a simple sentiment processor.
func NewSentimentProcessor() *SentimentProcessor {
	return &SentimentProcessor{
		positiveWords: map[string]float64{
			"great": 0.8, "good": 0.6, "happy": 0.7, "satisfied": 0.6, "excellent": 0.9,
		},
		negativeWords: map[string]float64{
			"bad": 0.7, "angry": 0.8, "upset": 0.7, "terrible": 0.9, "cancel": 0.6,
		},
		decay: 5 * time.Minute,
	}
}

func (p *SentimentProcessor) Process(_ context.Context, event *TranscriptEvent, state *State) error {
	score := 0.0
	text := strings.ToLower(event.Text)
	words := strings.Fields(text)

	for _, w := range words {
		if val, ok := p.positiveWords[w]; ok {
			score += val
		}
		if val, ok := p.negativeWords[w]; ok {
			score -= val
		}
	}

	result := SentimentResult{Label: "neutral", Score: score}
	if score > 0.2 {
		result.Label = "positive"
	} else if score < -0.2 {
		result.Label = "negative"
	}

	state.SentimentTrend = append(state.SentimentTrend, result)
	if len(state.SentimentTrend) > 200 {
		state.SentimentTrend = state.SentimentTrend[len(state.SentimentTrend)-200:]
	}

	// Update quality score with a simple rolling average.
	alpha := 0.1
	state.QualityScore = (1-alpha)*state.QualityScore + alpha*(result.Score+1)/2 // normalize to 0..1

	return nil
}

// KeywordProcessor extracts keywords using simple frequency heuristics.
type KeywordProcessor struct {
	ignore map[string]struct{}
}

// NewKeywordProcessor constructs a keyword processor.
func NewKeywordProcessor(ignoreWords []string) *KeywordProcessor {
	ig := make(map[string]struct{}, len(ignoreWords))
	for _, w := range ignoreWords {
		ig[strings.ToLower(w)] = struct{}{}
	}
	return &KeywordProcessor{ignore: ig}
}

func (p *KeywordProcessor) Process(_ context.Context, event *TranscriptEvent, state *State) error {
	text := strings.ToLower(event.Text)
	words := strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == ',' || r == '.' || r == '!' || r == '?'
	})

	for _, w := range words {
		if len(w) < 3 {
			continue
		}
		if _, skip := p.ignore[w]; skip {
			continue
		}
		state.Keywords[w]++
	}

	return nil
}

// ComplianceRule defines a simple rule for compliance checking.
type ComplianceRule struct {
	ID          string
	Description string
	Severity    string
	Contains    []string
	Missing     []string
}

// ComplianceProcessor checks transcripts against rules.
type ComplianceProcessor struct {
	rules []ComplianceRule
}

func NewComplianceProcessor(rules []ComplianceRule) *ComplianceProcessor {
	return &ComplianceProcessor{rules: rules}
}

func (p *ComplianceProcessor) Process(_ context.Context, event *TranscriptEvent, state *State) error {
	text := strings.ToLower(event.Text)

	for _, rule := range p.rules {
		if state.ComplianceRules == nil {
			state.ComplianceRules = make(map[string]ComplianceRule, len(p.rules))
		}
		if state.ComplianceSatisfied == nil {
			state.ComplianceSatisfied = make(map[string]bool, len(p.rules))
		}

		state.ComplianceRules[rule.ID] = rule

		containsSatisfied := len(rule.Contains) == 0
		if len(rule.Contains) > 0 {
			containsSatisfied = true
			for _, mustContain := range rule.Contains {
				if !strings.Contains(text, strings.ToLower(mustContain)) {
					containsSatisfied = false
					break
				}
			}
		}
		if containsSatisfied {
			state.ComplianceSatisfied[rule.ID] = true
		}

		violated := false
		for _, mustNotContain := range rule.Missing {
			if strings.Contains(text, strings.ToLower(mustNotContain)) {
				violated = true
				break
			}
		}

		if violated {
			state.Violations = append(state.Violations, ComplianceViolation{
				RuleID:      rule.ID,
				Description: rule.Description,
				Severity:    rule.Severity,
				Timestamp:   time.Now(),
			})
		}
	}

	return nil
}

// AgentMetricsProcessor updates agent performance metrics based on speaker turns.
type AgentMetricsProcessor struct {
	agentSpeakers map[string]struct{}
}

func NewAgentMetricsProcessor(agentSpeakers []string) *AgentMetricsProcessor {
	agents := make(map[string]struct{}, len(agentSpeakers))
	for _, id := range agentSpeakers {
		agents[id] = struct{}{}
	}
	return &AgentMetricsProcessor{agentSpeakers: agents}
}

func (p *AgentMetricsProcessor) Process(_ context.Context, event *TranscriptEvent, state *State) error {
	isAgent := false
	if event.Speaker != "" {
		if _, ok := p.agentSpeakers[event.Speaker]; ok {
			isAgent = true
		}
	}

	if isAgent {
		if state.Metrics.LastSpeechAt.IsZero() {
			state.Metrics.LastSpeechAt = event.Timestamp
		}
		delta := event.Timestamp.Sub(state.Metrics.LastSpeechAt)
		if delta < 0 {
			delta = 0
		}
		state.Metrics.TotalTalkTime += delta
		state.Metrics.LastSpeechAt = event.Timestamp
	} else {
		if !state.Metrics.LastSpeechAt.IsZero() {
			delta := event.Timestamp.Sub(state.Metrics.LastSpeechAt)
			if delta > 2*time.Second {
				state.Metrics.TotalSilenceTime += delta
			}
		}
	}

	return nil
}
