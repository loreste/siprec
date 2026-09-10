package realtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// MLSentimentProvider calls an external HTTP inference endpoint for
// ML-based sentiment analysis. It speaks a simple JSON request/response
// protocol that works with most model-serving frameworks (TorchServe,
// Triton, TF Serving, HuggingFace Inference API, vLLM, etc.).
//
// Request:  POST {"text": "...", "model": "..."}
// Response: {"label": "positive|negative|neutral", "score": 0.0-1.0,
//
//	"magnitude": 0.0-1.0, "subjectivity": 0.0-1.0}
type MLSentimentProvider struct {
	endpoint string
	model    string
	timeout  time.Duration
	client   *http.Client
	logger   *logrus.Entry
}

type mlRequest struct {
	Text  string `json:"text"`
	Model string `json:"model,omitempty"`
}

type mlResponse struct {
	Label        string  `json:"label"`
	Score        float64 `json:"score"`
	Magnitude    float64 `json:"magnitude"`
	Subjectivity float64 `json:"subjectivity"`
}

// NewMLSentimentProvider creates a provider that calls an ML inference endpoint.
func NewMLSentimentProvider(endpoint, model string, timeout time.Duration, logger *logrus.Logger) *MLSentimentProvider {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &MLSentimentProvider{
		endpoint: endpoint,
		model:    model,
		timeout:  timeout,
		client:   &http.Client{Timeout: timeout},
		logger:   logger.WithField("component", "sentiment_ml"),
	}
}

func (p *MLSentimentProvider) AnalyzeText(text string) Sentiment {
	if len(strings.TrimSpace(text)) < 3 {
		return Sentiment{Label: "neutral", Score: 0.5}
	}

	body, err := json.Marshal(mlRequest{Text: text, Model: p.model})
	if err != nil {
		p.logger.WithError(err).Warn("Failed to marshal ML sentiment request")
		return Sentiment{Label: "neutral", Score: 0.5}
	}

	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		p.logger.WithError(err).Warn("Failed to create ML sentiment request")
		return Sentiment{Label: "neutral", Score: 0.5}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		p.logger.WithError(err).Debug("ML sentiment endpoint unreachable")
		return Sentiment{Label: "neutral", Score: 0.5}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		p.logger.WithField("status", resp.StatusCode).Warn("ML sentiment endpoint returned non-200")
		return Sentiment{Label: "neutral", Score: 0.5}
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		p.logger.WithError(err).Warn("Failed to read ML sentiment response")
		return Sentiment{Label: "neutral", Score: 0.5}
	}

	var result mlResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		p.logger.WithError(err).WithField("body", string(respBody)).Warn("Failed to decode ML sentiment response")
		return Sentiment{Label: "neutral", Score: 0.5}
	}

	label := strings.ToLower(result.Label)
	if label != "positive" && label != "negative" && label != "neutral" {
		label = "neutral"
	}

	score := result.Score
	if score < 0 {
		score = 0
	} else if score > 1 {
		score = 1
	}

	return Sentiment{
		Label:        label,
		Score:        score,
		Magnitude:    clamp01(result.Magnitude),
		Subjectivity: clamp01(result.Subjectivity),
	}
}

// FallbackSentimentProvider tries the primary provider first; on any
// neutral-with-zero-magnitude result (the sentinel for ML failure),
// it falls back to the secondary.
type FallbackSentimentProvider struct {
	Primary   SentimentProvider
	Fallback  SentimentProvider
	logger    *logrus.Entry
}

func (f *FallbackSentimentProvider) AnalyzeText(text string) Sentiment {
	result := f.Primary.AnalyzeText(text)
	if isMLFailureSentinel(result) {
		f.logger.Debug("ML sentiment returned failure sentinel, falling back to lexicon")
		return f.Fallback.AnalyzeText(text)
	}
	return result
}

// isMLFailureSentinel detects the neutral/0.5/0.0 placeholder the ML provider
// returns on any error. Real neutral results from a working model will
// normally have non-zero magnitude.
func isMLFailureSentinel(s Sentiment) bool {
	return s.Label == "neutral" && s.Score == 0.5 && s.Magnitude == 0.0 && s.Subjectivity == 0.0
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// newSentimentProvider constructs the right provider based on config.
func newSentimentProvider(config *StreamingConfig, logger *logrus.Logger) SentimentProvider {
	lexicon := NewSentimentAnalyzer(logger)

	switch strings.ToLower(config.SentimentMode) {
	case "ml":
		if config.SentimentEndpoint == "" {
			logger.Warn("sentiment_mode=ml but no sentiment_endpoint configured, falling back to lexicon")
			return lexicon
		}
		return NewMLSentimentProvider(config.SentimentEndpoint, config.SentimentModel, config.SentimentTimeout, logger)

	case "auto":
		if config.SentimentEndpoint == "" {
			logger.Info("sentiment_mode=auto but no endpoint, using lexicon only")
			return lexicon
		}
		ml := NewMLSentimentProvider(config.SentimentEndpoint, config.SentimentModel, config.SentimentTimeout, logger)
		return &FallbackSentimentProvider{
			Primary:  ml,
			Fallback: lexicon,
			logger:   logger.WithField("component", "sentiment_fallback"),
		}

	default: // "lexicon" or anything else
		return lexicon
	}
}

// Compile-time interface checks
var (
	_ SentimentProvider = (*SentimentAnalyzer)(nil)
	_ SentimentProvider = (*MLSentimentProvider)(nil)
	_ SentimentProvider = (*FallbackSentimentProvider)(nil)
)

// FormatProviderName returns a human-readable name for logging.
func FormatProviderName(p SentimentProvider) string {
	switch p.(type) {
	case *SentimentAnalyzer:
		return "lexicon"
	case *MLSentimentProvider:
		return "ml"
	case *FallbackSentimentProvider:
		return "auto (ml+lexicon)"
	default:
		return fmt.Sprintf("%T", p)
	}
}
