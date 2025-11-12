package realtime

import (
	"math"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// SentimentAnalyzer provides real-time sentiment analysis for transcribed text
type SentimentAnalyzer struct {
	logger *logrus.Entry

	// Lexicon-based analysis
	positiveWords map[string]float64
	negativeWords map[string]float64
	intensifiers  map[string]float64
	negators      map[string]float64

	// Pattern-based analysis
	emotionPatterns  map[string]*regexp.Regexp
	punctuationRules map[string]float64

	// Context analysis
	contextWindow    int
	recentSentiments []Sentiment
	sentimentHistory map[string][]Sentiment // Per speaker

	// Configuration
	config *SentimentConfig

	// Performance optimization
	textProcessor  *TextProcessor
	cacheMaxSize   int
	sentimentCache map[string]Sentiment
	lastCleanup    time.Time

	// Thread safety
	mutex sync.RWMutex

	// Statistics
	stats *SentimentStats
}

// SentimentConfig holds configuration for sentiment analysis
type SentimentConfig struct {
	Language           string `json:"language" default:"en"`
	ContextWindow      int    `json:"context_window" default:"5"`
	MinTextLength      int    `json:"min_text_length" default:"3"`
	EnableEmotions     bool   `json:"enable_emotions" default:"true"`
	EnableSubjectivity bool   `json:"enable_subjectivity" default:"true"`
	CacheSize          int    `json:"cache_size" default:"1000"`
	HistorySize        int    `json:"history_size" default:"100"`
}

// SentimentStats tracks sentiment analysis performance
type SentimentStats struct {
	mutex             sync.RWMutex
	TotalAnalyses     int64     `json:"total_analyses"`
	PositiveDetected  int64     `json:"positive_detected"`
	NegativeDetected  int64     `json:"negative_detected"`
	NeutralDetected   int64     `json:"neutral_detected"`
	ProcessingTime    int64     `json:"processing_time_ms"`
	CacheHits         int64     `json:"cache_hits"`
	CacheMisses       int64     `json:"cache_misses"`
	AverageConfidence float64   `json:"average_confidence"`
	LastReset         time.Time `json:"last_reset"`
}

// TextProcessor handles text preprocessing for sentiment analysis
type TextProcessor struct {
	// Text cleaning patterns
	urlPattern        *regexp.Regexp
	mentionPattern    *regexp.Regexp
	hashtagPattern    *regexp.Regexp
	punctPattern      *regexp.Regexp
	whitespacePattern *regexp.Regexp

	// Word processing
	stopWords    map[string]bool
	contractions map[string]string
}

// NewSentimentAnalyzer creates a new sentiment analyzer
func NewSentimentAnalyzer(logger *logrus.Logger) *SentimentAnalyzer {
	config := &SentimentConfig{
		Language:           "en",
		ContextWindow:      5,
		MinTextLength:      3,
		EnableEmotions:     true,
		EnableSubjectivity: true,
		CacheSize:          1000,
		HistorySize:        100,
	}

	sa := &SentimentAnalyzer{
		logger:           logger.WithField("component", "sentiment_analyzer"),
		config:           config,
		contextWindow:    config.ContextWindow,
		recentSentiments: make([]Sentiment, 0, config.HistorySize),
		sentimentHistory: make(map[string][]Sentiment),
		cacheMaxSize:     config.CacheSize,
		sentimentCache:   make(map[string]Sentiment),
		lastCleanup:      time.Now(),
		stats:            &SentimentStats{LastReset: time.Now()},
	}

	// Initialize lexicons and patterns
	sa.initializeLexicons()
	sa.initializePatterns()
	sa.textProcessor = sa.initializeTextProcessor()

	return sa
}

// AnalyzeText analyzes the sentiment of the given text
func (sa *SentimentAnalyzer) AnalyzeText(text string) Sentiment {
	if len(text) < sa.config.MinTextLength {
		return Sentiment{Label: "neutral", Score: 0.5, Magnitude: 0.0}
	}

	startTime := time.Now()
	defer func() {
		sa.stats.mutex.Lock()
		sa.stats.ProcessingTime += time.Since(startTime).Nanoseconds() / 1e6
		sa.stats.TotalAnalyses++
		sa.stats.mutex.Unlock()
	}()

	// Check cache first
	if cached, exists := sa.getCachedSentiment(text); exists {
		sa.stats.mutex.Lock()
		sa.stats.CacheHits++
		sa.stats.mutex.Unlock()
		return cached
	}

	sa.stats.mutex.Lock()
	sa.stats.CacheMisses++
	sa.stats.mutex.Unlock()

	// Preprocess text
	processedText := sa.textProcessor.ProcessText(text)

	// Perform sentiment analysis
	sentiment := sa.analyzeSentiment(processedText, text)

	// Add contextual adjustment
	sentiment = sa.adjustForContext(sentiment)

	// Cache result
	sa.cacheSentiment(text, sentiment)

	// Update statistics
	sa.updateStats(sentiment)

	// Add to history
	sa.addToHistory("", sentiment) // Speaker ID not available at this level

	return sentiment
}

// analyzeSentiment performs the core sentiment analysis
func (sa *SentimentAnalyzer) analyzeSentiment(processedText, originalText string) Sentiment {
	words := strings.Fields(processedText)
	if len(words) == 0 {
		return Sentiment{Label: "neutral", Score: 0.5, Magnitude: 0.0}
	}

	// Lexicon-based analysis
	lexiconScore := sa.calculateLexiconScore(words)

	// Pattern-based analysis
	patternScore := sa.calculatePatternScore(originalText)

	// Punctuation-based analysis
	punctuationScore := sa.calculatePunctuationScore(originalText)

	// Combine scores with weights
	combinedScore := (lexiconScore*0.6 + patternScore*0.3 + punctuationScore*0.1)

	// Calculate magnitude (intensity)
	magnitude := sa.calculateMagnitude(words, combinedScore)

	// Calculate subjectivity if enabled
	subjectivity := 0.5 // Default neutral
	if sa.config.EnableSubjectivity {
		subjectivity = sa.calculateSubjectivity(words)
	}

	// Determine label and normalize score
	label, normalizedScore := sa.normalizeScore(combinedScore)

	return Sentiment{
		Label:        label,
		Score:        normalizedScore,
		Magnitude:    magnitude,
		Subjectivity: subjectivity,
	}
}

// calculateLexiconScore calculates sentiment score based on word lexicons
func (sa *SentimentAnalyzer) calculateLexiconScore(words []string) float64 {
	score := 0.0
	wordCount := 0
	modifier := 1.0

	for i, word := range words {
		lowerWord := strings.ToLower(word)

		// Check for negators (flip sentiment for next 3 words)
		if negValue, isNegator := sa.negators[lowerWord]; isNegator {
			modifier = negValue
			continue
		}

		// Check for intensifiers
		if intValue, isIntensifier := sa.intensifiers[lowerWord]; isIntensifier {
			modifier *= intValue
			continue
		}

		// Check sentiment words
		if posValue, isPositive := sa.positiveWords[lowerWord]; isPositive {
			score += posValue * modifier
			wordCount++
		} else if negValue, isNegative := sa.negativeWords[lowerWord]; isNegative {
			score += negValue * modifier
			wordCount++
		}

		// Reset modifier after 3 words or at end of sentence
		if i > 0 && (i%3 == 0 || strings.ContainsAny(word, ".!?")) {
			modifier = 1.0
		}
	}

	if wordCount > 0 {
		return score / float64(wordCount)
	}
	return 0.0
}

// calculatePatternScore calculates sentiment based on text patterns
func (sa *SentimentAnalyzer) calculatePatternScore(text string) float64 {
	score := 0.0

	for emotion, pattern := range sa.emotionPatterns {
		if pattern.MatchString(text) {
			switch emotion {
			case "joy", "excitement", "love":
				score += 0.8
			case "anger", "sadness", "fear":
				score -= 0.8
			case "surprise":
				score += 0.3
			case "disgust":
				score -= 0.6
			}
		}
	}

	return score
}

// calculatePunctuationScore calculates sentiment based on punctuation
func (sa *SentimentAnalyzer) calculatePunctuationScore(text string) float64 {
	score := 0.0

	for punct, value := range sa.punctuationRules {
		count := strings.Count(text, punct)
		score += float64(count) * value
	}

	return score
}

// calculateMagnitude calculates the intensity/magnitude of sentiment
func (sa *SentimentAnalyzer) calculateMagnitude(words []string, score float64) float64 {
	// Base magnitude from absolute score
	magnitude := math.Abs(score)

	// Boost magnitude for intensifiers and strong words
	intensityBoost := 0.0
	for _, word := range words {
		lowerWord := strings.ToLower(word)
		if intValue, exists := sa.intensifiers[lowerWord]; exists {
			intensityBoost += math.Abs(intValue - 1.0)
		}

		// Check for strong sentiment words (high absolute values)
		if posValue, exists := sa.positiveWords[lowerWord]; exists && math.Abs(posValue) > 0.7 {
			intensityBoost += 0.2
		}
		if negValue, exists := sa.negativeWords[lowerWord]; exists && math.Abs(negValue) > 0.7 {
			intensityBoost += 0.2
		}
	}

	magnitude += intensityBoost

	// Normalize to [0, 1]
	if magnitude > 1.0 {
		magnitude = 1.0
	}

	return magnitude
}

// calculateSubjectivity calculates how subjective vs objective the text is
func (sa *SentimentAnalyzer) calculateSubjectivity(words []string) float64 {
	subjectiveCount := 0
	totalWords := len(words)

	for _, word := range words {
		lowerWord := strings.ToLower(word)

		// Count sentiment words as subjective
		if _, exists := sa.positiveWords[lowerWord]; exists {
			subjectiveCount++
		} else if _, exists := sa.negativeWords[lowerWord]; exists {
			subjectiveCount++
		}

		// Check for subjective indicators
		if sa.isSubjectiveWord(lowerWord) {
			subjectiveCount++
		}
	}

	if totalWords > 0 {
		return float64(subjectiveCount) / float64(totalWords)
	}
	return 0.5
}

// isSubjectiveWord checks if a word indicates subjectivity
func (sa *SentimentAnalyzer) isSubjectiveWord(word string) bool {
	subjectiveIndicators := []string{
		"think", "feel", "believe", "opinion", "seems", "appears",
		"probably", "maybe", "perhaps", "might", "could", "should",
		"beautiful", "ugly", "amazing", "terrible", "wonderful", "awful",
	}

	for _, indicator := range subjectiveIndicators {
		if word == indicator {
			return true
		}
	}
	return false
}

// normalizeScore converts raw score to label and normalized score
func (sa *SentimentAnalyzer) normalizeScore(score float64) (string, float64) {
	// Normalize score to [0, 1] range
	normalizedScore := (score + 1.0) / 2.0
	if normalizedScore < 0 {
		normalizedScore = 0
	} else if normalizedScore > 1 {
		normalizedScore = 1
	}

	// Determine label based on thresholds
	if normalizedScore > 0.6 {
		return "positive", normalizedScore
	} else if normalizedScore < 0.4 {
		return "negative", normalizedScore
	} else {
		return "neutral", normalizedScore
	}
}

// adjustForContext adjusts sentiment based on recent context
func (sa *SentimentAnalyzer) adjustForContext(sentiment Sentiment) Sentiment {
	sa.mutex.RLock()
	defer sa.mutex.RUnlock()

	if len(sa.recentSentiments) < 2 {
		return sentiment
	}

	// Calculate context influence
	contextScore := 0.0
	contextWeight := 0.1 // 10% influence from context

	for i, recent := range sa.recentSentiments {
		if i >= sa.contextWindow {
			break
		}

		// Weight more recent sentiments higher
		weight := float64(sa.contextWindow-i) / float64(sa.contextWindow)
		contextScore += recent.Score * weight
	}

	contextScore /= float64(len(sa.recentSentiments))

	// Adjust current sentiment
	adjustedScore := sentiment.Score*(1-contextWeight) + contextScore*contextWeight
	adjustedLabel, _ := sa.normalizeScore((adjustedScore - 0.5) * 2) // Convert back to [-1,1] then normalize

	return Sentiment{
		Label:        adjustedLabel,
		Score:        adjustedScore,
		Magnitude:    sentiment.Magnitude,
		Subjectivity: sentiment.Subjectivity,
	}
}

// getCachedSentiment retrieves cached sentiment analysis result
func (sa *SentimentAnalyzer) getCachedSentiment(text string) (Sentiment, bool) {
	sa.mutex.RLock()
	defer sa.mutex.RUnlock()

	// Create cache key
	key := sa.createCacheKey(text)
	sentiment, exists := sa.sentimentCache[key]
	return sentiment, exists
}

// cacheSentiment caches sentiment analysis result
func (sa *SentimentAnalyzer) cacheSentiment(text string, sentiment Sentiment) {
	sa.mutex.Lock()
	defer sa.mutex.Unlock()

	// Check cache size limit
	if len(sa.sentimentCache) >= sa.cacheMaxSize {
		// Remove oldest entries (simplified LRU)
		count := 0
		target := sa.cacheMaxSize / 4
		for key := range sa.sentimentCache {
			delete(sa.sentimentCache, key)
			count++
			if count >= target {
				break
			}
		}
	}

	key := sa.createCacheKey(text)
	sa.sentimentCache[key] = sentiment
}

// createCacheKey creates a cache key for text
func (sa *SentimentAnalyzer) createCacheKey(text string) string {
	// Simplified key creation - normalize text
	return strings.ToLower(strings.TrimSpace(text))
}

// addToHistory adds sentiment to history
func (sa *SentimentAnalyzer) addToHistory(speakerID string, sentiment Sentiment) {
	sa.mutex.Lock()
	defer sa.mutex.Unlock()

	// Add to recent sentiments
	sa.recentSentiments = append(sa.recentSentiments, sentiment)
	if len(sa.recentSentiments) > sa.config.HistorySize {
		sa.recentSentiments = sa.recentSentiments[1:]
	}

	// Add to speaker-specific history if speaker ID provided
	if speakerID != "" {
		if _, exists := sa.sentimentHistory[speakerID]; !exists {
			sa.sentimentHistory[speakerID] = make([]Sentiment, 0)
		}

		sa.sentimentHistory[speakerID] = append(sa.sentimentHistory[speakerID], sentiment)
		if len(sa.sentimentHistory[speakerID]) > sa.config.HistorySize {
			sa.sentimentHistory[speakerID] = sa.sentimentHistory[speakerID][1:]
		}
	}
}

// updateStats updates sentiment analysis statistics
func (sa *SentimentAnalyzer) updateStats(sentiment Sentiment) {
	sa.stats.mutex.Lock()
	defer sa.stats.mutex.Unlock()

	switch sentiment.Label {
	case "positive":
		sa.stats.PositiveDetected++
	case "negative":
		sa.stats.NegativeDetected++
	case "neutral":
		sa.stats.NeutralDetected++
	}

	// Update average confidence (using score as confidence proxy)
	if sa.stats.TotalAnalyses > 0 {
		sa.stats.AverageConfidence = (sa.stats.AverageConfidence*float64(sa.stats.TotalAnalyses-1) + sentiment.Score) / float64(sa.stats.TotalAnalyses)
	} else {
		sa.stats.AverageConfidence = sentiment.Score
	}
}

// GetSpeakerSentimentTrend returns sentiment trend for a specific speaker
func (sa *SentimentAnalyzer) GetSpeakerSentimentTrend(speakerID string) []Sentiment {
	sa.mutex.RLock()
	defer sa.mutex.RUnlock()

	if history, exists := sa.sentimentHistory[speakerID]; exists {
		// Return a copy
		trend := make([]Sentiment, len(history))
		copy(trend, history)
		return trend
	}

	return nil
}

// GetStats returns sentiment analysis statistics
func (sa *SentimentAnalyzer) GetStats() *SentimentStats {
	sa.stats.mutex.RLock()
	defer sa.stats.mutex.RUnlock()

	statsCopy := &SentimentStats{
		TotalAnalyses:     sa.stats.TotalAnalyses,
		PositiveDetected:  sa.stats.PositiveDetected,
		NegativeDetected:  sa.stats.NegativeDetected,
		NeutralDetected:   sa.stats.NeutralDetected,
		ProcessingTime:    sa.stats.ProcessingTime,
		CacheHits:         sa.stats.CacheHits,
		CacheMisses:       sa.stats.CacheMisses,
		AverageConfidence: sa.stats.AverageConfidence,
		LastReset:         sa.stats.LastReset,
	}
	return statsCopy
}

// Cleanup performs cleanup operations
func (sa *SentimentAnalyzer) Cleanup() {
	sa.mutex.Lock()
	defer sa.mutex.Unlock()

	// Clear caches and history
	sa.sentimentCache = make(map[string]Sentiment)
	sa.recentSentiments = sa.recentSentiments[:0]

	// Clear speaker histories
	for speakerID := range sa.sentimentHistory {
		delete(sa.sentimentHistory, speakerID)
	}

	sa.logger.Debug("Sentiment analyzer cleaned up")
}

// initializeLexicons initializes sentiment word lexicons
func (sa *SentimentAnalyzer) initializeLexicons() {
	// Initialize positive words (simplified set)
	sa.positiveWords = map[string]float64{
		"good": 0.7, "great": 0.8, "excellent": 0.9, "amazing": 0.9, "wonderful": 0.8,
		"fantastic": 0.9, "awesome": 0.8, "brilliant": 0.8, "perfect": 0.9, "outstanding": 0.9,
		"love": 0.8, "like": 0.6, "enjoy": 0.7, "happy": 0.8, "pleased": 0.7,
		"satisfied": 0.7, "delighted": 0.8, "thrilled": 0.9, "excited": 0.8, "positive": 0.7,
		"yes": 0.6, "success": 0.8, "win": 0.7, "victory": 0.8, "achieve": 0.7,
	}

	// Initialize negative words (simplified set)
	sa.negativeWords = map[string]float64{
		"bad": -0.7, "terrible": -0.8, "awful": -0.9, "horrible": -0.9, "disgusting": -0.8,
		"hate": -0.8, "dislike": -0.6, "angry": -0.8, "mad": -0.7, "furious": -0.9,
		"sad": -0.7, "depressed": -0.8, "disappointed": -0.7, "upset": -0.7, "frustrated": -0.7,
		"no": -0.5, "never": -0.6, "nothing": -0.5, "nobody": -0.5, "failure": -0.8,
		"lose": -0.7, "defeat": -0.7, "wrong": -0.6, "problem": -0.6, "issue": -0.5,
	}

	// Initialize intensifiers
	sa.intensifiers = map[string]float64{
		"very": 1.3, "extremely": 1.5, "really": 1.2, "quite": 1.1, "rather": 1.1,
		"absolutely": 1.4, "completely": 1.4, "totally": 1.4, "incredibly": 1.5,
		"remarkably": 1.3, "exceptionally": 1.4, "particularly": 1.2,
	}

	// Initialize negators
	sa.negators = map[string]float64{
		"not": -1.0, "no": -1.0, "never": -1.0, "nothing": -1.0, "nobody": -1.0,
		"nowhere": -1.0, "neither": -1.0, "nor": -1.0, "without": -0.8, "lack": -0.8,
		"barely": -0.7, "hardly": -0.7, "scarcely": -0.7, "seldom": -0.6,
	}
}

// initializePatterns initializes emotion detection patterns
func (sa *SentimentAnalyzer) initializePatterns() {
	sa.emotionPatterns = make(map[string]*regexp.Regexp)

	// Joy patterns
	sa.emotionPatterns["joy"] = regexp.MustCompile(`(?i)(\:D|\:\)|\:P|haha|lol|lmao|rofl|😂|😃|😄|🙂)`)

	// Sadness patterns
	sa.emotionPatterns["sadness"] = regexp.MustCompile(`(?i)(\:\(|\:'\(|😢|😭|💔|sob|cry|tears)`)

	// Anger patterns
	sa.emotionPatterns["anger"] = regexp.MustCompile(`(?i)(damn|shit|fuck|angry|mad|furious|rage|😡|🤬|grr)`)

	// Fear patterns
	sa.emotionPatterns["fear"] = regexp.MustCompile(`(?i)(scared|afraid|terrified|worried|anxious|panic|😨|😰)`)

	// Love patterns
	sa.emotionPatterns["love"] = regexp.MustCompile(`(?i)(love|adore|cherish|💖|💕|❤️|😍|🥰)`)

	// Surprise patterns
	sa.emotionPatterns["surprise"] = regexp.MustCompile(`(?i)(wow|omg|amazing|incredible|unbelievable|😮|😲)`)

	// Initialize punctuation rules
	sa.punctuationRules = map[string]float64{
		"!":   0.3,  // Exclamation adds intensity
		"!!!": 0.6,  // Multiple exclamations add more
		"?":   0.1,  // Questions slightly positive (engagement)
		"...": -0.1, // Ellipsis slightly negative (uncertainty)
		"!!":  0.4,  // Double exclamation
	}
}

// initializeTextProcessor initializes the text processor
func (sa *SentimentAnalyzer) initializeTextProcessor() *TextProcessor {
	tp := &TextProcessor{
		urlPattern:        regexp.MustCompile(`https?://[^\s]+`),
		mentionPattern:    regexp.MustCompile(`@\w+`),
		hashtagPattern:    regexp.MustCompile(`#\w+`),
		punctPattern:      regexp.MustCompile(`[^\w\s]`),
		whitespacePattern: regexp.MustCompile(`\s+`),
		stopWords:         make(map[string]bool),
		contractions:      make(map[string]string),
	}

	// Initialize stop words (simplified set)
	stopWordsList := []string{
		"the", "a", "an", "and", "or", "but", "in", "on", "at", "to", "for", "of", "with", "by",
		"is", "are", "was", "were", "be", "been", "have", "has", "had", "do", "does", "did",
		"will", "would", "could", "should", "may", "might", "can", "must", "shall",
		"i", "you", "he", "she", "it", "we", "they", "me", "him", "her", "us", "them",
		"this", "that", "these", "those", "what", "which", "who", "where", "when", "why", "how",
	}

	for _, word := range stopWordsList {
		tp.stopWords[word] = true
	}

	// Initialize contractions
	tp.contractions = map[string]string{
		"don't": "do not", "won't": "will not", "can't": "cannot", "shouldn't": "should not",
		"wouldn't": "would not", "couldn't": "could not", "isn't": "is not", "aren't": "are not",
		"wasn't": "was not", "weren't": "were not", "haven't": "have not", "hasn't": "has not",
		"hadn't": "had not", "I'm": "I am", "you're": "you are", "he's": "he is", "she's": "she is",
		"it's": "it is", "we're": "we are", "they're": "they are", "I've": "I have",
		"you've": "you have", "we've": "we have", "they've": "they have", "I'll": "I will",
		"you'll": "you will", "he'll": "he will", "she'll": "she will", "it'll": "it will",
		"we'll": "we will", "they'll": "they will", "I'd": "I would", "you'd": "you would",
		"he'd": "he would", "she'd": "she would", "we'd": "we would", "they'd": "they would",
	}

	return tp
}

// ProcessText processes and cleans text for sentiment analysis
func (tp *TextProcessor) ProcessText(text string) string {
	// Convert to lowercase
	processed := strings.ToLower(text)

	// Expand contractions
	for contraction, expansion := range tp.contractions {
		processed = strings.ReplaceAll(processed, strings.ToLower(contraction), expansion)
	}

	// Remove URLs
	processed = tp.urlPattern.ReplaceAllString(processed, "")

	// Remove mentions and hashtags but keep the text part
	processed = tp.mentionPattern.ReplaceAllString(processed, "")
	processed = tp.hashtagPattern.ReplaceAllStringFunc(processed, func(match string) string {
		return match[1:] // Remove # but keep the word
	})

	// Normalize whitespace
	processed = tp.whitespacePattern.ReplaceAllString(processed, " ")

	// Trim
	processed = strings.TrimSpace(processed)

	return processed
}
