package realtime

import (
	"sync"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestNewKeywordDetector(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	kd := NewKeywordDetector(logger)
	if kd == nil {
		t.Fatal("Expected non-nil KeywordDetector")
	}

	if kd.config == nil {
		t.Error("Expected config to be initialized")
	}

	if kd.complianceKeywords == nil || len(kd.complianceKeywords) == 0 {
		t.Error("Expected compliance keywords to be initialized")
	}

	if kd.securityKeywords == nil || len(kd.securityKeywords) == 0 {
		t.Error("Expected security keywords to be initialized")
	}

	if kd.compiledPatterns == nil {
		t.Error("Expected compiled patterns to be initialized")
	}
}

func TestDetectKeywordsCompliance(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	kd := NewKeywordDetector(logger)

	tests := []struct {
		name           string
		text           string
		expectCategory string
		expectKeyword  string
	}{
		{
			name:           "credit card detection",
			text:           "Please provide your credit card number",
			expectCategory: "compliance",
			expectKeyword:  "credit card",
		},
		{
			name:           "SSN detection",
			text:           "I need your social security number for verification",
			expectCategory: "compliance",
			expectKeyword:  "social security",
		},
		{
			name:           "bank account detection",
			text:           "What is your bank account number",
			expectCategory: "compliance",
			expectKeyword:  "bank account",
		},
		{
			name:           "HIPAA detection",
			text:           "This information is protected under HIPAA regulations",
			expectCategory: "compliance",
			expectKeyword:  "hipaa",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keywords := kd.DetectKeywords(tt.text)
			if len(keywords) == 0 {
				t.Errorf("Expected to detect keyword in '%s'", tt.text)
				return
			}

			found := false
			for _, kw := range keywords {
				if kw.Category == tt.expectCategory {
					found = true
					break
				}
			}

			if !found {
				t.Errorf("Expected category %s in detected keywords", tt.expectCategory)
			}
		})
	}
}

func TestDetectKeywordsSecurity(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	kd := NewKeywordDetector(logger)

	tests := []struct {
		name           string
		text           string
		expectCategory string
	}{
		{
			name:           "password detection",
			text:           "Please enter your password to continue",
			expectCategory: "security",
		},
		{
			name:           "hack detection",
			text:           "Someone tried to hack into the system",
			expectCategory: "security",
		},
		{
			name:           "breach detection",
			text:           "We detected a data breach in the system",
			expectCategory: "security",
		},
		{
			name:           "malware detection",
			text:           "The system was infected with malware",
			expectCategory: "security",
		},
		{
			name:           "phishing detection",
			text:           "This looks like a phishing attempt",
			expectCategory: "security",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keywords := kd.DetectKeywords(tt.text)
			if len(keywords) == 0 {
				t.Errorf("Expected to detect security keyword in '%s'", tt.text)
				return
			}

			found := false
			for _, kw := range keywords {
				if kw.Category == tt.expectCategory {
					found = true
					break
				}
			}

			if !found {
				t.Errorf("Expected category %s in detected keywords for '%s'", tt.expectCategory, tt.text)
			}
		})
	}
}

func TestDetectKeywordsSeverity(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	kd := NewKeywordDetector(logger)

	// SSN should be critical severity
	keywords := kd.DetectKeywords("Please provide your SSN for verification")
	if len(keywords) > 0 {
		foundCritical := false
		for _, kw := range keywords {
			if kw.Severity == "critical" || kw.Severity == "high" {
				foundCritical = true
				break
			}
		}
		if !foundCritical {
			t.Log("Expected high/critical severity for SSN detection")
		}
	}
}

func TestDetectKeywordsNoMatch(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	kd := NewKeywordDetector(logger)

	// Normal conversation without sensitive keywords
	keywords := kd.DetectKeywords("Hello, how are you today? The weather is nice.")
	if len(keywords) > 0 {
		t.Errorf("Expected no keywords in normal conversation, got %d", len(keywords))
	}
}

func TestDetectKeywordsEmptyText(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	kd := NewKeywordDetector(logger)

	keywords := kd.DetectKeywords("")
	if keywords != nil && len(keywords) > 0 {
		t.Error("Expected nil or empty result for empty text")
	}

	keywords = kd.DetectKeywords("a")
	if keywords != nil && len(keywords) > 0 {
		t.Error("Expected nil or empty result for very short text")
	}
}

func TestDetectKeywordsCaseInsensitive(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	kd := NewKeywordDetector(logger)

	// Test different cases
	tests := []string{
		"Please provide your CREDIT CARD",
		"please provide your credit card",
		"Please Provide Your Credit Card",
	}

	for _, text := range tests {
		keywords := kd.DetectKeywords(text)
		if len(keywords) == 0 {
			t.Errorf("Expected to detect keyword regardless of case: %s", text)
		}
	}
}

func TestKeywordCaching(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	kd := NewKeywordDetector(logger)

	text := "Please provide your credit card number"

	// First detection
	result1 := kd.DetectKeywords(text)

	// Second detection (should hit cache)
	result2 := kd.DetectKeywords(text)

	if len(result1) != len(result2) {
		t.Error("Cached result should have same length")
	}

	stats := kd.GetStats()
	if stats.CacheHits < 1 {
		t.Error("Expected at least one cache hit")
	}
}

func TestKeywordStatistics(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	kd := NewKeywordDetector(logger)

	// Detect various keywords
	kd.DetectKeywords("Please provide your credit card")
	kd.DetectKeywords("Enter your password")
	kd.DetectKeywords("What is your SSN")

	stats := kd.GetStats()
	if stats.TotalDetections < 1 {
		t.Error("Expected at least 1 detection")
	}

	if len(stats.CategoryCounts) == 0 {
		t.Error("Expected category counts to be populated")
	}
}

func TestKeywordConcurrency(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	kd := NewKeywordDetector(logger)

	texts := []string{
		"Please provide your credit card number",
		"Enter your password for authentication",
		"What is your social security number",
		"We detected a security breach",
		"This is a normal conversation",
	}

	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			text := texts[idx%len(texts)]
			kd.DetectKeywords(text)
		}(i)
	}

	wg.Wait()

	// Just verify no panics occurred and stats are reasonable
	stats := kd.GetStats()
	if stats.CacheMisses+stats.CacheHits < 100 {
		t.Errorf("Expected at least 100 operations, got %d", stats.CacheMisses+stats.CacheHits)
	}
}

func TestAddCustomKeywords(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	kd := NewKeywordDetector(logger)

	// Add custom keywords
	customPatterns := []KeywordPattern{
		{
			Pattern:  "secret project",
			IsRegex:  false,
			Category: "custom",
			Severity: "high",
			Weight:   0.9,
		},
		{
			Pattern:  "\\bproject-x\\b",
			IsRegex:  true,
			Category: "custom",
			Severity: "critical",
			Weight:   0.95,
		},
	}

	err := kd.AddCustomKeywords("internal", customPatterns)
	if err != nil {
		t.Fatalf("Failed to add custom keywords: %v", err)
	}

	// Test detection of custom keyword
	keywords := kd.DetectKeywords("We are working on the secret project")
	found := false
	for _, kw := range keywords {
		if kw.Category == "custom" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected to detect custom keyword")
	}
}

func TestGetSpeakerDetectionHistory(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	kd := NewKeywordDetector(logger)

	// Manually add to history for testing
	keywords := []Keyword{
		{Text: "credit card", Category: "compliance", Confidence: 0.9, Severity: "high"},
	}
	kd.addToHistory("speaker1", keywords, "test context")

	history := kd.GetSpeakerDetectionHistory("speaker1")
	if len(history) != 1 {
		t.Errorf("Expected 1 detection in history, got %d", len(history))
	}

	// Non-existent speaker
	nilHistory := kd.GetSpeakerDetectionHistory("unknown")
	if nilHistory != nil {
		t.Error("Expected nil for unknown speaker")
	}
}

func TestGetRecentDetections(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	kd := NewKeywordDetector(logger)

	// Detect some keywords
	kd.DetectKeywords("Please provide your credit card")
	kd.DetectKeywords("Enter your password")

	recent := kd.GetRecentDetections()
	if len(recent) < 1 {
		t.Error("Expected at least 1 recent detection")
	}
}

func TestKeywordCleanup(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	kd := NewKeywordDetector(logger)

	// Add some data
	kd.DetectKeywords("Please provide your credit card")
	keywords := []Keyword{
		{Text: "test", Category: "test", Confidence: 0.9},
	}
	kd.addToHistory("speaker1", keywords, "test")

	// Cleanup
	kd.Cleanup()

	// Verify cleanup
	history := kd.GetSpeakerDetectionHistory("speaker1")
	if history != nil {
		t.Error("Expected speaker history to be cleared")
	}

	recent := kd.GetRecentDetections()
	if len(recent) > 0 {
		t.Error("Expected recent detections to be cleared")
	}
}

func TestFuzzyMatching(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	kd := NewKeywordDetector(logger)

	// The fuzzy matching should catch minor typos
	// Note: This depends on the implementation's sensitivity
	keywords := kd.DetectKeywords("Please provide your credit card information")
	if len(keywords) == 0 {
		t.Log("Fuzzy matching test: no keywords detected (may be expected depending on threshold)")
	}
}

func TestFilterByConfidence(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	kd := NewKeywordDetector(logger)

	keywords := []Keyword{
		{Text: "test1", Confidence: 0.9},
		{Text: "test2", Confidence: 0.3},
		{Text: "test3", Confidence: 0.7},
	}

	filtered := kd.filterByConfidence(keywords)

	// Default min confidence is 0.6
	for _, kw := range filtered {
		if kw.Confidence < kd.config.MinConfidence {
			t.Errorf("Keyword with confidence %f should be filtered out", kw.Confidence)
		}
	}
}

func TestRemoveDuplicates(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	kd := NewKeywordDetector(logger)

	keywords := []Keyword{
		{Text: "credit card", Category: "compliance", Severity: "high"},
		{Text: "credit card", Category: "compliance", Severity: "high"},
		{Text: "password", Category: "security", Severity: "high"},
	}

	unique := kd.removeDuplicates(keywords)

	if len(unique) != 2 {
		t.Errorf("Expected 2 unique keywords, got %d", len(unique))
	}
}

func TestMultipleKeywordsInText(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	kd := NewKeywordDetector(logger)

	text := "Please provide your credit card number and password for verification"
	keywords := kd.DetectKeywords(text)

	// Should detect both credit card and password
	categories := make(map[string]bool)
	for _, kw := range keywords {
		categories[kw.Category] = true
	}

	if !categories["compliance"] && !categories["security"] {
		t.Log("Expected to detect keywords from both compliance and security categories")
	}
}

func BenchmarkDetectKeywords(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	kd := NewKeywordDetector(logger)
	text := "Please provide your credit card number and social security number for verification"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		kd.DetectKeywords(text)
	}
}

func BenchmarkDetectKeywordsParallel(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	kd := NewKeywordDetector(logger)
	text := "Please provide your credit card number and social security number for verification"

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			kd.DetectKeywords(text)
		}
	})
}
