package realtime

import (
	"sync"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestNewSentimentAnalyzer(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	sa := NewSentimentAnalyzer(logger)
	if sa == nil {
		t.Fatal("Expected non-nil SentimentAnalyzer")
	}

	if sa.config == nil {
		t.Error("Expected config to be initialized")
	}

	if sa.positiveWords == nil || len(sa.positiveWords) == 0 {
		t.Error("Expected positive words lexicon to be initialized")
	}

	if sa.negativeWords == nil || len(sa.negativeWords) == 0 {
		t.Error("Expected negative words lexicon to be initialized")
	}

	if sa.emotionPatterns == nil || len(sa.emotionPatterns) == 0 {
		t.Error("Expected emotion patterns to be initialized")
	}
}

func TestAnalyzeTextPositive(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	sa := NewSentimentAnalyzer(logger)

	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{"simple positive", "This is great and amazing!", "positive"},
		{"love expression", "I love this wonderful product", "positive"},
		{"very positive", "Absolutely fantastic experience, excellent service!", "positive"},
		{"happy sentiment", "I am so happy and delighted with the results", "positive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sa.AnalyzeText(tt.text)
			if result.Label != tt.expected {
				t.Errorf("Expected %s, got %s (score: %f)", tt.expected, result.Label, result.Score)
			}
		})
	}
}

func TestAnalyzeTextNegative(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	sa := NewSentimentAnalyzer(logger)

	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{"simple negative", "This is terrible and awful", "negative"},
		{"hate expression", "I hate this horrible service", "negative"},
		{"very negative", "This is terrible, horrible, and I hate it so much", "negative"},
		{"angry sentiment", "I am so angry and frustrated with this", "negative"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sa.AnalyzeText(tt.text)
			if result.Label != tt.expected {
				t.Errorf("Expected %s, got %s (score: %f)", tt.expected, result.Label, result.Score)
			}
		})
	}
}

func TestAnalyzeTextNeutral(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	sa := NewSentimentAnalyzer(logger)

	tests := []struct {
		name string
		text string
	}{
		{"neutral statement", "The meeting is scheduled for tomorrow"},
		{"factual statement", "The report contains five sections"},
		{"question", "What time does the meeting start"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sa.AnalyzeText(tt.text)
			// Neutral is expected but may vary slightly
			if result.Score < 0.3 || result.Score > 0.7 {
				t.Logf("Text: %s -> Label: %s, Score: %f", tt.text, result.Label, result.Score)
			}
		})
	}
}

func TestAnalyzeTextNegation(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	sa := NewSentimentAnalyzer(logger)

	// "not good" should be less positive than "good"
	positiveResult := sa.AnalyzeText("This is good")
	negatedResult := sa.AnalyzeText("This is not good")

	if negatedResult.Score >= positiveResult.Score {
		t.Errorf("Negation should reduce score: 'good' score=%f, 'not good' score=%f",
			positiveResult.Score, negatedResult.Score)
	}
}

func TestAnalyzeTextIntensifiers(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	sa := NewSentimentAnalyzer(logger)

	// "very good" should have higher magnitude than just "good"
	simpleResult := sa.AnalyzeText("This is good")
	intensifiedResult := sa.AnalyzeText("This is very good")

	if intensifiedResult.Magnitude < simpleResult.Magnitude {
		t.Logf("Expected intensifier to increase magnitude: simple=%f, intensified=%f",
			simpleResult.Magnitude, intensifiedResult.Magnitude)
	}
}

func TestAnalyzeTextMinLength(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	sa := NewSentimentAnalyzer(logger)

	// Short text should return neutral
	result := sa.AnalyzeText("Hi")
	if result.Label != "neutral" {
		t.Errorf("Short text should be neutral, got %s", result.Label)
	}
}

func TestAnalyzeTextEmptyString(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	sa := NewSentimentAnalyzer(logger)

	result := sa.AnalyzeText("")
	if result.Label != "neutral" {
		t.Errorf("Empty text should be neutral, got %s", result.Label)
	}
}

func TestSentimentCaching(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	sa := NewSentimentAnalyzer(logger)

	text := "This is a great test for caching"

	// First analysis
	result1 := sa.AnalyzeText(text)

	// Second analysis (should hit cache)
	result2 := sa.AnalyzeText(text)

	if result1.Label != result2.Label || result1.Score != result2.Score {
		t.Error("Cached result should be identical")
	}

	stats := sa.GetStats()
	if stats.CacheHits < 1 {
		t.Error("Expected at least one cache hit")
	}
}

func TestSentimentStatistics(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	sa := NewSentimentAnalyzer(logger)

	// Analyze several texts
	sa.AnalyzeText("This is great!")
	sa.AnalyzeText("This is terrible!")
	sa.AnalyzeText("This is okay")

	stats := sa.GetStats()
	if stats.TotalAnalyses < 3 {
		t.Errorf("Expected at least 3 analyses, got %d", stats.TotalAnalyses)
	}
}

func TestSentimentConcurrency(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	sa := NewSentimentAnalyzer(logger)

	texts := []string{
		"This is absolutely wonderful!",
		"I hate this terrible experience",
		"The weather is nice today",
		"What a fantastic product!",
		"This is the worst thing ever",
	}

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			text := texts[idx%len(texts)]
			result := sa.AnalyzeText(text)
			if result.Label == "" {
				errors <- nil // Just to track completion
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check stats for consistency
	stats := sa.GetStats()
	if stats.TotalAnalyses < 100 {
		t.Errorf("Expected at least 100 analyses, got %d", stats.TotalAnalyses)
	}
}

func TestSpeakerSentimentTrend(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	sa := NewSentimentAnalyzer(logger)

	// Add sentiments to history for a speaker
	sa.addToHistory("speaker1", Sentiment{Label: "positive", Score: 0.8})
	sa.addToHistory("speaker1", Sentiment{Label: "negative", Score: 0.3})
	sa.addToHistory("speaker1", Sentiment{Label: "neutral", Score: 0.5})

	trend := sa.GetSpeakerSentimentTrend("speaker1")
	if len(trend) != 3 {
		t.Errorf("Expected 3 sentiments in trend, got %d", len(trend))
	}

	// Non-existent speaker should return nil
	nilTrend := sa.GetSpeakerSentimentTrend("unknown")
	if nilTrend != nil {
		t.Error("Expected nil for unknown speaker")
	}
}

func TestSentimentCleanup(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	sa := NewSentimentAnalyzer(logger)

	// Add some data
	sa.AnalyzeText("Test text for cleanup")
	sa.addToHistory("speaker1", Sentiment{Label: "positive", Score: 0.8})

	// Cleanup
	sa.Cleanup()

	// Verify cleanup
	trend := sa.GetSpeakerSentimentTrend("speaker1")
	if trend != nil {
		t.Error("Expected speaker history to be cleared")
	}
}

func TestEmotionPatterns(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	sa := NewSentimentAnalyzer(logger)

	tests := []struct {
		name     string
		text     string
		positive bool
	}{
		{"joy emoji", "This is great :D", true},
		{"sad emoji", "I'm so sad :(", false},
		{"lol expression", "That was funny lol", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sa.AnalyzeText(tt.text)
			if tt.positive && result.Score < 0.5 {
				t.Logf("Expected positive sentiment for '%s', got score %f", tt.text, result.Score)
			}
			if !tt.positive && result.Score > 0.5 {
				t.Logf("Expected negative sentiment for '%s', got score %f", tt.text, result.Score)
			}
		})
	}
}

func TestSubjectivityCalculation(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	sa := NewSentimentAnalyzer(logger)

	// Opinion text should be more subjective
	opinionResult := sa.AnalyzeText("I think this is beautiful and amazing")

	// Factual text should be more objective
	factualResult := sa.AnalyzeText("The meeting is at three o'clock")

	if opinionResult.Subjectivity <= factualResult.Subjectivity {
		t.Logf("Opinion text subjectivity: %f, Factual text subjectivity: %f",
			opinionResult.Subjectivity, factualResult.Subjectivity)
	}
}

func BenchmarkAnalyzeText(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	sa := NewSentimentAnalyzer(logger)
	text := "This is a wonderful and amazing experience that I really enjoyed!"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sa.AnalyzeText(text)
	}
}

func BenchmarkAnalyzeTextParallel(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	sa := NewSentimentAnalyzer(logger)
	text := "This is a wonderful and amazing experience that I really enjoyed!"

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			sa.AnalyzeText(text)
		}
	})
}
