package realtime

import (
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// KeywordDetector provides real-time keyword detection for compliance monitoring
type KeywordDetector struct {
	logger *logrus.Entry
	
	// Keyword categories and patterns
	complianceKeywords map[string][]KeywordPattern
	securityKeywords   map[string][]KeywordPattern
	customKeywords     map[string][]KeywordPattern
	
	// Pattern matching
	compiledPatterns   map[string]*regexp.Regexp
	
	// Context analysis
	contextWindow      int
	recentDetections   []KeywordDetection
	detectionHistory   map[string][]KeywordDetection // Per speaker
	
	// Configuration
	config             *KeywordConfig
	
	// Performance optimization
	cacheMaxSize       int
	detectionCache     map[string][]Keyword
	lastCleanup        time.Time
	
	// Thread safety
	mutex              sync.RWMutex
	
	// Statistics
	stats              *KeywordStats
}

// KeywordPattern represents a keyword detection pattern
type KeywordPattern struct {
	Pattern    string   `json:"pattern"`    // Regex pattern or exact text
	IsRegex    bool     `json:"is_regex"`   // Whether pattern is regex
	Category   string   `json:"category"`   // Category (compliance, security, etc.)
	Severity   string   `json:"severity"`   // Severity level (low, medium, high, critical)
	Weight     float64  `json:"weight"`     // Detection confidence weight
	Context    []string `json:"context"`    // Required context words
	Exclusions []string `json:"exclusions"` // Words that exclude this match
}

// KeywordDetection represents a detected keyword instance
type KeywordDetection struct {
	Keyword     Keyword   `json:"keyword"`
	Context     string    `json:"context"`     // Surrounding text
	SpeakerID   string    `json:"speaker_id"`
	Timestamp   time.Time `json:"timestamp"`
	Confidence  float64   `json:"confidence"`
}

// KeywordConfig holds configuration for keyword detection
type KeywordConfig struct {
	Language         string `json:"language" default:"en"`
	ContextWindow    int    `json:"context_window" default:"10"`
	MinConfidence    float64 `json:"min_confidence" default:"0.6"`
	CaseSensitive    bool   `json:"case_sensitive" default:"false"`
	EnableFuzzyMatch bool   `json:"enable_fuzzy_match" default:"true"`
	CacheSize        int    `json:"cache_size" default:"1000"`
	HistorySize      int    `json:"history_size" default:"500"`
}

// KeywordStats tracks keyword detection performance
type KeywordStats struct {
	mutex                 sync.RWMutex
	TotalDetections       int64                    `json:"total_detections"`
	CategoryCounts        map[string]int64         `json:"category_counts"`
	SeverityCounts        map[string]int64         `json:"severity_counts"`
	ProcessingTime        int64                    `json:"processing_time_ms"`
	CacheHits             int64                    `json:"cache_hits"`
	CacheMisses           int64                    `json:"cache_misses"`
	FalsePositiveRate     float64                  `json:"false_positive_rate"`
	AverageConfidence     float64                  `json:"average_confidence"`
	LastReset             time.Time                `json:"last_reset"`
}

// NewKeywordDetector creates a new keyword detector
func NewKeywordDetector(logger *logrus.Logger) *KeywordDetector {
	config := &KeywordConfig{
		Language:         "en",
		ContextWindow:    10,
		MinConfidence:    0.6,
		CaseSensitive:    false,
		EnableFuzzyMatch: true,
		CacheSize:        1000,
		HistorySize:      500,
	}
	
	kd := &KeywordDetector{
		logger:             logger.WithField("component", "keyword_detector"),
		config:             config,
		contextWindow:      config.ContextWindow,
		recentDetections:   make([]KeywordDetection, 0, config.HistorySize),
		detectionHistory:   make(map[string][]KeywordDetection),
		compiledPatterns:   make(map[string]*regexp.Regexp),
		cacheMaxSize:       config.CacheSize,
		detectionCache:     make(map[string][]Keyword),
		lastCleanup:        time.Now(),
		stats:              &KeywordStats{
			CategoryCounts: make(map[string]int64),
			SeverityCounts: make(map[string]int64),
			LastReset:      time.Now(),
		},
	}
	
	// Initialize keyword patterns
	kd.initializeKeywordPatterns()
	kd.compilePatterns()
	
	return kd
}

// DetectKeywords detects keywords in the given text
func (kd *KeywordDetector) DetectKeywords(text string) []Keyword {
	if len(text) < 2 {
		return nil
	}
	
	startTime := time.Now()
	defer func() {
		kd.stats.mutex.Lock()
		kd.stats.ProcessingTime += time.Since(startTime).Nanoseconds() / 1e6
		kd.stats.mutex.Unlock()
	}()
	
	// Check cache first
	if cached, exists := kd.getCachedDetection(text); exists {
		kd.stats.mutex.Lock()
		kd.stats.CacheHits++
		kd.stats.mutex.Unlock()
		return cached
	}
	
	kd.stats.mutex.Lock()
	kd.stats.CacheMisses++
	kd.stats.mutex.Unlock()
	
	// Preprocess text
	processedText := kd.preprocessText(text)
	
	// Detect keywords across all categories
	var allKeywords []Keyword
	
	// Compliance keywords
	compliance := kd.detectInCategory(processedText, text, kd.complianceKeywords, "compliance")
	allKeywords = append(allKeywords, compliance...)
	
	// Security keywords
	security := kd.detectInCategory(processedText, text, kd.securityKeywords, "security")
	allKeywords = append(allKeywords, security...)
	
	// Custom keywords
	custom := kd.detectInCategory(processedText, text, kd.customKeywords, "custom")
	allKeywords = append(allKeywords, custom...)
	
	// Filter by confidence threshold
	filteredKeywords := kd.filterByConfidence(allKeywords)
	
	// Remove duplicates and conflicts
	finalKeywords := kd.removeDuplicates(filteredKeywords)
	
	// Cache result
	kd.cacheDetection(text, finalKeywords)
	
	// Update statistics
	kd.updateStats(finalKeywords)
	
	// Add to detection history
	kd.addToHistory("", finalKeywords, text) // Speaker ID not available at this level
	
	return finalKeywords
}

// detectInCategory detects keywords within a specific category
func (kd *KeywordDetector) detectInCategory(processedText, originalText string, categoryPatterns map[string][]KeywordPattern, category string) []Keyword {
	var keywords []Keyword
	
	for _, patterns := range categoryPatterns {
		for _, pattern := range patterns {
			matches := kd.findMatches(processedText, originalText, pattern)
			for _, match := range matches {
				keyword := Keyword{
					Text:       match.text,
					Category:   category,
					Confidence: match.confidence,
					StartTime:  match.startTime,
					EndTime:    match.endTime,
					Severity:   pattern.Severity,
				}
				keywords = append(keywords, keyword)
			}
		}
	}
	
	return keywords
}

// MatchResult represents a pattern match result
type MatchResult struct {
	text       string
	confidence float64
	startTime  float64
	endTime    float64
	position   int
}

// findMatches finds all matches for a pattern in the text
func (kd *KeywordDetector) findMatches(processedText, originalText string, pattern KeywordPattern) []MatchResult {
	var matches []MatchResult
	
	searchText := processedText
	if kd.config.CaseSensitive {
		searchText = originalText
	}
	
	if pattern.IsRegex {
		// Regex pattern matching
		if compiledPattern, exists := kd.compiledPatterns[pattern.Pattern]; exists {
			regexMatches := compiledPattern.FindAllStringIndex(searchText, -1)
			for _, match := range regexMatches {
				matchText := searchText[match[0]:match[1]]
				confidence := kd.calculateMatchConfidence(matchText, originalText, pattern)
				
				if confidence >= kd.config.MinConfidence {
					matches = append(matches, MatchResult{
						text:       matchText,
						confidence: confidence,
						startTime:  0.0, // Would be calculated from position in real implementation
						endTime:    0.0,
						position:   match[0],
					})
				}
			}
		}
	} else {
		// Exact text matching
		searchPattern := pattern.Pattern
		if !kd.config.CaseSensitive {
			searchPattern = strings.ToLower(searchPattern)
		}
		
		index := 0
		for {
			pos := strings.Index(searchText[index:], searchPattern)
			if pos == -1 {
				break
			}
			
			actualPos := index + pos
			confidence := kd.calculateMatchConfidence(searchPattern, originalText, pattern)
			
			if confidence >= kd.config.MinConfidence {
				matches = append(matches, MatchResult{
					text:       searchPattern,
					confidence: confidence,
					startTime:  0.0,
					endTime:    0.0,
					position:   actualPos,
				})
			}
			
			index = actualPos + len(searchPattern)
		}
	}
	
	// Apply fuzzy matching if enabled
	if kd.config.EnableFuzzyMatch && len(matches) == 0 {
		fuzzyMatches := kd.findFuzzyMatches(searchText, pattern)
		matches = append(matches, fuzzyMatches...)
	}
	
	return matches
}

// calculateMatchConfidence calculates confidence for a pattern match
func (kd *KeywordDetector) calculateMatchConfidence(matchText, fullText string, pattern KeywordPattern) float64 {
	confidence := pattern.Weight
	
	// Context analysis
	contextScore := kd.analyzeContext(matchText, fullText, pattern)
	confidence *= contextScore
	
	// Check exclusions
	if kd.hasExclusions(fullText, pattern.Exclusions) {
		confidence *= 0.3 // Reduce confidence significantly
	}
	
	// Fuzzy match penalty
	if kd.config.EnableFuzzyMatch {
		exactMatch := strings.Contains(strings.ToLower(fullText), strings.ToLower(pattern.Pattern))
		if !exactMatch {
			confidence *= 0.8 // Slight penalty for fuzzy matches
		}
	}
	
	// Normalize to [0, 1]
	if confidence > 1.0 {
		confidence = 1.0
	}
	if confidence < 0.0 {
		confidence = 0.0
	}
	
	return confidence
}

// analyzeContext analyzes the context around a match
func (kd *KeywordDetector) analyzeContext(matchText, fullText string, pattern KeywordPattern) float64 {
	if len(pattern.Context) == 0 {
		return 1.0 // No context requirements
	}
	
	// Find the position of the match in the full text
	matchPos := strings.Index(strings.ToLower(fullText), strings.ToLower(matchText))
	if matchPos == -1 {
		return 0.5 // Default if can't find position
	}
	
	// Extract context window around the match
	start := matchPos - kd.contextWindow*10 // Approximate words
	if start < 0 {
		start = 0
	}
	end := matchPos + len(matchText) + kd.contextWindow*10
	if end > len(fullText) {
		end = len(fullText)
	}
	
	contextText := strings.ToLower(fullText[start:end])
	
	// Check for required context words
	contextFound := 0
	for _, contextWord := range pattern.Context {
		if strings.Contains(contextText, strings.ToLower(contextWord)) {
			contextFound++
		}
	}
	
	// Calculate context score
	if len(pattern.Context) > 0 {
		return float64(contextFound) / float64(len(pattern.Context))
	}
	
	return 1.0
}

// hasExclusions checks if text contains exclusion words
func (kd *KeywordDetector) hasExclusions(text string, exclusions []string) bool {
	lowerText := strings.ToLower(text)
	for _, exclusion := range exclusions {
		if strings.Contains(lowerText, strings.ToLower(exclusion)) {
			return true
		}
	}
	return false
}

// findFuzzyMatches finds approximate matches using simple edit distance
func (kd *KeywordDetector) findFuzzyMatches(text string, pattern KeywordPattern) []MatchResult {
	var matches []MatchResult
	
	if pattern.IsRegex {
		return matches // Skip fuzzy matching for regex patterns
	}
	
	words := strings.Fields(text)
	patternWords := strings.Fields(strings.ToLower(pattern.Pattern))
	
	if len(patternWords) == 0 {
		return matches
	}
	
	// Simple n-gram fuzzy matching
	for i := 0; i <= len(words)-len(patternWords); i++ {
		window := words[i : i+len(patternWords)]
		similarity := kd.calculateSimilarity(window, patternWords)
		
		if similarity > 0.7 { // 70% similarity threshold
			matchText := strings.Join(window, " ")
			confidence := similarity * pattern.Weight * 0.8 // Penalty for fuzzy match
			
			if confidence >= kd.config.MinConfidence {
				matches = append(matches, MatchResult{
					text:       matchText,
					confidence: confidence,
					startTime:  0.0,
					endTime:    0.0,
					position:   i,
				})
			}
		}
	}
	
	return matches
}

// calculateSimilarity calculates similarity between two word sequences
func (kd *KeywordDetector) calculateSimilarity(words1, words2 []string) float64 {
	if len(words1) != len(words2) {
		return 0.0
	}
	
	matches := 0
	for i := 0; i < len(words1); i++ {
		if strings.ToLower(words1[i]) == strings.ToLower(words2[i]) {
			matches++
		} else {
			// Check character-level similarity
			charSim := kd.calculateCharSimilarity(words1[i], words2[i])
			if charSim > 0.8 {
				matches++
			}
		}
	}
	
	return float64(matches) / float64(len(words1))
}

// calculateCharSimilarity calculates character-level similarity
func (kd *KeywordDetector) calculateCharSimilarity(s1, s2 string) float64 {
	if len(s1) == 0 || len(s2) == 0 {
		return 0.0
	}
	
	// Simple character overlap ratio
	s1Lower := strings.ToLower(s1)
	s2Lower := strings.ToLower(s2)
	
	if s1Lower == s2Lower {
		return 1.0
	}
	
	// Count common characters
	commonChars := 0
	s1Chars := make(map[rune]int)
	s2Chars := make(map[rune]int)
	
	for _, char := range s1Lower {
		s1Chars[char]++
	}
	for _, char := range s2Lower {
		s2Chars[char]++
	}
	
	for char, count1 := range s1Chars {
		if count2, exists := s2Chars[char]; exists {
			commonChars += min(count1, count2)
		}
	}
	
	maxLen := max(len(s1), len(s2))
	return float64(commonChars) / float64(maxLen)
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// filterByConfidence filters keywords by confidence threshold
func (kd *KeywordDetector) filterByConfidence(keywords []Keyword) []Keyword {
	var filtered []Keyword
	for _, keyword := range keywords {
		if keyword.Confidence >= kd.config.MinConfidence {
			filtered = append(filtered, keyword)
		}
	}
	return filtered
}

// removeDuplicates removes duplicate and overlapping keyword detections
func (kd *KeywordDetector) removeDuplicates(keywords []Keyword) []Keyword {
	if len(keywords) <= 1 {
		return keywords
	}
	
	var unique []Keyword
	seen := make(map[string]bool)
	
	for _, keyword := range keywords {
		// Create a key for deduplication
		key := keyword.Text + "_" + keyword.Category + "_" + keyword.Severity
		
		if !seen[key] {
			seen[key] = true
			unique = append(unique, keyword)
		}
	}
	
	return unique
}

// preprocessText preprocesses text for keyword detection
func (kd *KeywordDetector) preprocessText(text string) string {
	// Basic preprocessing
	processed := strings.TrimSpace(text)
	
	if !kd.config.CaseSensitive {
		processed = strings.ToLower(processed)
	}
	
	// Normalize whitespace
	processed = regexp.MustCompile(`\s+`).ReplaceAllString(processed, " ")
	
	return processed
}

// getCachedDetection retrieves cached keyword detection result
func (kd *KeywordDetector) getCachedDetection(text string) ([]Keyword, bool) {
	kd.mutex.RLock()
	defer kd.mutex.RUnlock()
	
	key := kd.createCacheKey(text)
	keywords, exists := kd.detectionCache[key]
	return keywords, exists
}

// cacheDetection caches keyword detection result
func (kd *KeywordDetector) cacheDetection(text string, keywords []Keyword) {
	kd.mutex.Lock()
	defer kd.mutex.Unlock()
	
	// Check cache size limit
	if len(kd.detectionCache) >= kd.cacheMaxSize {
		// Remove oldest entries (simplified LRU)
		count := 0
		target := kd.cacheMaxSize / 4
		for key := range kd.detectionCache {
			delete(kd.detectionCache, key)
			count++
			if count >= target {
				break
			}
		}
	}
	
	key := kd.createCacheKey(text)
	kd.detectionCache[key] = keywords
}

// createCacheKey creates a cache key for text
func (kd *KeywordDetector) createCacheKey(text string) string {
	return strings.ToLower(strings.TrimSpace(text))
}

// addToHistory adds detection results to history
func (kd *KeywordDetector) addToHistory(speakerID string, keywords []Keyword, context string) {
	if len(keywords) == 0 {
		return
	}
	
	kd.mutex.Lock()
	defer kd.mutex.Unlock()
	
	now := time.Now()
	
	for _, keyword := range keywords {
		detection := KeywordDetection{
			Keyword:    keyword,
			Context:    context,
			SpeakerID:  speakerID,
			Timestamp:  now,
			Confidence: keyword.Confidence,
		}
		
		// Add to recent detections
		kd.recentDetections = append(kd.recentDetections, detection)
		if len(kd.recentDetections) > kd.config.HistorySize {
			kd.recentDetections = kd.recentDetections[1:]
		}
		
		// Add to speaker-specific history if speaker ID provided
		if speakerID != "" {
			if _, exists := kd.detectionHistory[speakerID]; !exists {
				kd.detectionHistory[speakerID] = make([]KeywordDetection, 0)
			}
			
			kd.detectionHistory[speakerID] = append(kd.detectionHistory[speakerID], detection)
			if len(kd.detectionHistory[speakerID]) > kd.config.HistorySize {
				kd.detectionHistory[speakerID] = kd.detectionHistory[speakerID][1:]
			}
		}
	}
}

// updateStats updates keyword detection statistics
func (kd *KeywordDetector) updateStats(keywords []Keyword) {
	if len(keywords) == 0 {
		return
	}
	
	kd.stats.mutex.Lock()
	defer kd.stats.mutex.Unlock()
	
	totalConfidence := 0.0
	
	for _, keyword := range keywords {
		kd.stats.TotalDetections++
		kd.stats.CategoryCounts[keyword.Category]++
		kd.stats.SeverityCounts[keyword.Severity]++
		totalConfidence += keyword.Confidence
	}
	
	// Update average confidence
	if kd.stats.TotalDetections > 0 {
		kd.stats.AverageConfidence = (kd.stats.AverageConfidence*float64(kd.stats.TotalDetections-int64(len(keywords))) + totalConfidence) / float64(kd.stats.TotalDetections)
	}
}

// compilePatterns compiles all regex patterns
func (kd *KeywordDetector) compilePatterns() {
	kd.mutex.Lock()
	defer kd.mutex.Unlock()
	
	categories := []map[string][]KeywordPattern{
		kd.complianceKeywords,
		kd.securityKeywords,
		kd.customKeywords,
	}
	
	for _, category := range categories {
		for _, patterns := range category {
			for _, pattern := range patterns {
				if pattern.IsRegex {
					patternStr := pattern.Pattern
					if !kd.config.CaseSensitive {
						patternStr = "(?i)" + patternStr
					}
					
					compiled, err := regexp.Compile(patternStr)
					if err != nil {
						kd.logger.WithError(err).WithField("pattern", pattern.Pattern).Warning("Failed to compile regex pattern")
						continue
					}
					
					kd.compiledPatterns[pattern.Pattern] = compiled
				}
			}
		}
	}
}

// AddCustomKeywords adds custom keyword patterns
func (kd *KeywordDetector) AddCustomKeywords(category string, patterns []KeywordPattern) error {
	kd.mutex.Lock()
	defer kd.mutex.Unlock()
	
	if kd.customKeywords == nil {
		kd.customKeywords = make(map[string][]KeywordPattern)
	}
	
	kd.customKeywords[category] = patterns
	
	// Compile new regex patterns
	for _, pattern := range patterns {
		if pattern.IsRegex {
			flags := ""
			if !kd.config.CaseSensitive {
				flags = "(?i)"
			}
			
			compiled, err := regexp.Compile(flags + pattern.Pattern)
			if err != nil {
				return err
			}
			
			kd.compiledPatterns[pattern.Pattern] = compiled
		}
	}
	
	return nil
}

// GetSpeakerDetectionHistory returns detection history for a specific speaker
func (kd *KeywordDetector) GetSpeakerDetectionHistory(speakerID string) []KeywordDetection {
	kd.mutex.RLock()
	defer kd.mutex.RUnlock()
	
	if history, exists := kd.detectionHistory[speakerID]; exists {
		// Return a copy
		historyCopy := make([]KeywordDetection, len(history))
		copy(historyCopy, history)
		return historyCopy
	}
	
	return nil
}

// GetRecentDetections returns recent keyword detections
func (kd *KeywordDetector) GetRecentDetections() []KeywordDetection {
	kd.mutex.RLock()
	defer kd.mutex.RUnlock()
	
	detectionsCopy := make([]KeywordDetection, len(kd.recentDetections))
	copy(detectionsCopy, kd.recentDetections)
	return detectionsCopy
}

// GetStats returns keyword detection statistics
func (kd *KeywordDetector) GetStats() *KeywordStats {
	kd.stats.mutex.RLock()
	defer kd.stats.mutex.RUnlock()
	
	statsCopy := *kd.stats
	// Deep copy maps
	statsCopy.CategoryCounts = make(map[string]int64)
	statsCopy.SeverityCounts = make(map[string]int64)
	
	for k, v := range kd.stats.CategoryCounts {
		statsCopy.CategoryCounts[k] = v
	}
	for k, v := range kd.stats.SeverityCounts {
		statsCopy.SeverityCounts[k] = v
	}
	
	return &statsCopy
}

// Cleanup performs cleanup operations
func (kd *KeywordDetector) Cleanup() {
	kd.mutex.Lock()
	defer kd.mutex.Unlock()
	
	// Clear caches and history
	kd.detectionCache = make(map[string][]Keyword)
	kd.recentDetections = kd.recentDetections[:0]
	
	// Clear speaker histories
	for speakerID := range kd.detectionHistory {
		delete(kd.detectionHistory, speakerID)
	}
	
	kd.logger.Debug("Keyword detector cleaned up")
}

// initializeKeywordPatterns initializes predefined keyword patterns
func (kd *KeywordDetector) initializeKeywordPatterns() {
	// Initialize compliance keywords
	kd.complianceKeywords = map[string][]KeywordPattern{
		"financial": {
			{Pattern: "\\b(credit card|bank account|social security)\\b", IsRegex: true, Category: "compliance", Severity: "high", Weight: 0.9},
			{Pattern: "\\b(ssn|social security number)\\b", IsRegex: true, Category: "compliance", Severity: "critical", Weight: 0.95},
			{Pattern: "\\b(bank routing|account number)\\b", IsRegex: true, Category: "compliance", Severity: "high", Weight: 0.85},
			{Pattern: "\\b(payment|transaction|withdraw|deposit)\\b", IsRegex: true, Category: "compliance", Severity: "medium", Weight: 0.7},
		},
		"healthcare": {
			{Pattern: "\\b(medical record|patient id|health insurance)\\b", IsRegex: true, Category: "compliance", Severity: "high", Weight: 0.9},
			{Pattern: "\\b(hipaa|protected health information|phi)\\b", IsRegex: true, Category: "compliance", Severity: "critical", Weight: 0.95},
			{Pattern: "\\b(diagnosis|prescription|medication)\\b", IsRegex: true, Category: "compliance", Severity: "medium", Weight: 0.7},
		},
		"legal": {
			{Pattern: "\\b(confidential|attorney.client|privileged)\\b", IsRegex: true, Category: "compliance", Severity: "high", Weight: 0.85},
			{Pattern: "\\b(lawsuit|litigation|settlement)\\b", IsRegex: true, Category: "compliance", Severity: "medium", Weight: 0.75},
			{Pattern: "\\b(contract|agreement|terms)\\b", IsRegex: true, Category: "compliance", Severity: "low", Weight: 0.6},
		},
	}
	
	// Initialize security keywords
	kd.securityKeywords = map[string][]KeywordPattern{
		"authentication": {
			{Pattern: "\\b(password|pin|passcode)\\b", IsRegex: true, Category: "security", Severity: "high", Weight: 0.9},
			{Pattern: "\\b(username|login|access code)\\b", IsRegex: true, Category: "security", Severity: "medium", Weight: 0.8},
			{Pattern: "\\b(two factor|2fa|authentication)\\b", IsRegex: true, Category: "security", Severity: "medium", Weight: 0.75},
		},
		"threats": {
			{Pattern: "\\b(hack|breach|compromise|attack)\\b", IsRegex: true, Category: "security", Severity: "critical", Weight: 0.95},
			{Pattern: "\\b(malware|virus|trojan|phishing)\\b", IsRegex: true, Category: "security", Severity: "critical", Weight: 0.9},
			{Pattern: "\\b(unauthorized|suspicious|threat)\\b", IsRegex: true, Category: "security", Severity: "high", Weight: 0.85},
		},
		"data": {
			{Pattern: "\\b(encryption|decrypt|private key)\\b", IsRegex: true, Category: "security", Severity: "high", Weight: 0.85},
			{Pattern: "\\b(data leak|exposure|confidential data)\\b", IsRegex: true, Category: "security", Severity: "critical", Weight: 0.9},
			{Pattern: "\\b(secure|protect|safeguard)\\b", IsRegex: true, Category: "security", Severity: "low", Weight: 0.6},
		},
	}
	
	// Initialize custom keywords (empty by default)
	kd.customKeywords = make(map[string][]KeywordPattern)
}