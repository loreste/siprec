package stt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"siprec-server/pkg/config"
)

// DeepgramProvider implements the Provider interface for Deepgram
type DeepgramProvider struct {
	logger               *logrus.Logger
	transcriptionSvc     *TranscriptionService
	config               *config.DeepgramSTTConfig
	languageSwitcher     *LanguageSwitcher
	transitionSmoother   *LanguageTransitionSmoother
	persistenceService   *LanguagePersistenceService
	callback func(callUUID, transcription string, isFinal bool, metadata map[string]interface{})
}

// NewDeepgramProvider creates a new Deepgram provider
func NewDeepgramProvider(logger *logrus.Logger, transcriptionSvc *TranscriptionService, cfg *config.DeepgramSTTConfig) *DeepgramProvider {
	provider := &DeepgramProvider{
		logger:           logger,
		transcriptionSvc: transcriptionSvc,
		config:           cfg,
	}
	
	// Initialize language switcher if real-time switching is enabled
	if cfg.RealtimeLanguageSwitching {
		provider.languageSwitcher = NewLanguageSwitcher(logger, cfg)
		provider.transitionSmoother = NewLanguageTransitionSmoother(logger)
		provider.persistenceService = NewLanguagePersistenceService(logger)
		logger.Info("Language switcher, transition smoother, and persistence service initialized for Deepgram provider")
	}
	
	return provider
}

// Name returns the provider name
func (p *DeepgramProvider) Name() string {
	return "deepgram"
}

// Initialize initializes the Deepgram client
func (p *DeepgramProvider) Initialize() error {
	if p.config == nil {
		return fmt.Errorf("Deepgram STT configuration is required")
	}

	if !p.config.Enabled {
		p.logger.Info("Deepgram STT is disabled, skipping initialization")
		return nil
	}

	if p.config.APIKey == "" {
		return fmt.Errorf("Deepgram API key is required")
	}

	// Log basic configuration
	p.logger.WithFields(logrus.Fields{
		"api_url":         p.config.APIURL,
		"model":           p.config.Model,
		"language":        p.config.Language,
		"tier":            p.config.Tier,
		"punctuate":       p.config.Punctuate,
		"diarize":         p.config.Diarize,
		"smart_format":    p.config.SmartFormat,
	}).Info("Deepgram provider initialized successfully")
	
	// Log enhanced accent detection configuration if enabled
	if p.config.DetectLanguage {
		p.logger.WithFields(logrus.Fields{
			"supported_languages":       p.config.SupportedLanguages,
			"confidence_threshold":      p.config.LanguageConfidenceThreshold,
			"accent_aware_models":       p.config.AccentAwareModels,
			"fallback_language":         p.config.FallbackLanguage,
			"realtime_switching":        p.config.RealtimeLanguageSwitching,
			"multilang_alternatives":    p.config.MultiLanguageAlternatives,
			"max_alternatives":          p.config.MaxLanguageAlternatives,
		}).Info("Deepgram multi-language accent detection enabled")
	}
	return nil
}

// DeepgramResponse defines the structure for the Deepgram API response
type DeepgramResponse struct {
	RequestID string `json:"request_id"`
	Results   struct {
		Channels []struct {
			Alternatives []struct {
				Transcript string  `json:"transcript"`
				Confidence float64 `json:"confidence"`
				Words      []struct {
					Word       string  `json:"word"`
					Start      float64 `json:"start"`
					End        float64 `json:"end"`
					Confidence float64 `json:"confidence"`
					Speaker    int     `json:"speaker,omitempty"`
				} `json:"words"`
				Paragraphs struct {
					Transcript string `json:"transcript"`
					Paragraphs []struct {
						Sentences []struct {
							Text  string  `json:"text"`
							Start float64 `json:"start"`
							End   float64 `json:"end"`
						} `json:"sentences"`
					} `json:"paragraphs"`
				} `json:"paragraphs"`
			} `json:"alternatives"`
		} `json:"channels"`
		Utterances []struct {
			Start      float64 `json:"start"`
			End        float64 `json:"end"`
			Confidence float64 `json:"confidence"`
			Channel    int     `json:"channel"`
			Transcript string  `json:"transcript"`
			Words      []struct {
				Word       string  `json:"word"`
				Start      float64 `json:"start"`
				End        float64 `json:"end"`
				Confidence float64 `json:"confidence"`
				Speaker    int     `json:"speaker,omitempty"`
			} `json:"words"`
			Speaker int `json:"speaker,omitempty"`
		} `json:"utterances"`
	} `json:"results"`
	Metadata struct {
		RequestID      string                 `json:"request_id"`
		TransactionKey string                 `json:"transaction_key"`
		SHA256         string                 `json:"sha256"`
		Created        string                 `json:"created"`
		Duration       float64                `json:"duration"`
		Channels       int                    `json:"channels"`
		Models         []string               `json:"models"`
		ModelInfo      map[string]interface{} `json:"model_info"`
		// Language detection fields for enhanced accent detection
		Language           string  `json:"language,omitempty"`
		LanguageConfidence float64 `json:"language_confidence,omitempty"`
		DetectedLanguages  []struct {
			Language   string  `json:"language"`
			Confidence float64 `json:"confidence"`
		} `json:"detected_languages,omitempty"`
	} `json:"metadata"`
}

// StreamToText streams audio data to Deepgram
func (p *DeepgramProvider) StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) error {
	req, err := http.NewRequestWithContext(ctx, "POST", p.config.APIURL+"/v1/listen", audioStream)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set necessary headers for the request
	req.Header.Set("Authorization", "Token "+p.config.APIKey)
	req.Header.Set("Content-Type", "audio/wav")

	// Add query parameters to the request URL from configuration
	query := req.URL.Query()
	query.Add("model", p.config.Model)
	
	// Enhanced language detection and accent handling
	if p.config.DetectLanguage && len(p.config.SupportedLanguages) > 0 {
		// Enable language detection with supported languages
		query.Add("detect_language", "true")
		query.Add("language", strings.Join(p.config.SupportedLanguages, ","))
		
		// Add accent-aware model selection if enabled
		if p.config.AccentAwareModels {
			query.Add("model", "nova-2")  // Use the most advanced model for accent detection
		}
		
		// Add confidence threshold for language detection
		if p.config.LanguageConfidenceThreshold > 0 {
			query.Add("language_confidence", fmt.Sprintf("%.2f", p.config.LanguageConfidenceThreshold))
		}
		
		// Enable alternatives if multi-language alternatives are requested
		if p.config.MultiLanguageAlternatives && p.config.MaxLanguageAlternatives > 1 {
			query.Add("alternatives", fmt.Sprintf("%d", p.config.MaxLanguageAlternatives))
		}
	} else {
		// Use single language mode with fallback
		language := p.config.Language
		if language == "" {
			language = p.config.FallbackLanguage
		}
		query.Add("language", language)
	}
	
	query.Add("tier", p.config.Tier)
	query.Add("version", p.config.Version)
	query.Add("punctuate", fmt.Sprintf("%t", p.config.Punctuate))
	query.Add("diarize", fmt.Sprintf("%t", p.config.Diarize))
	query.Add("numerals", fmt.Sprintf("%t", p.config.Numerals))
	query.Add("smart_format", fmt.Sprintf("%t", p.config.SmartFormat))
	query.Add("profanity_filter", fmt.Sprintf("%t", p.config.ProfanityFilter))

	// Add redaction if configured
	if len(p.config.Redact) > 0 {
		query.Add("redact", strings.Join(p.config.Redact, ","))
	}

	// Add keywords if configured
	if len(p.config.Keywords) > 0 {
		query.Add("keywords", strings.Join(p.config.Keywords, ","))
	}

	req.URL.RawQuery = query.Encode()

	// Send the request to Deepgram
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request to Deepgram: %w", err)
	}
	defer resp.Body.Close()

	// Check for non-200 response status
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("deepgram API returned non-200 status code: %d", resp.StatusCode)
	}

	// Parse the response body
	var deepgramResp DeepgramResponse
	if err := json.NewDecoder(resp.Body).Decode(&deepgramResp); err != nil {
		return fmt.Errorf("failed to decode Deepgram response: %w", err)
	}

	// Extract transcription if available and process it
	if len(deepgramResp.Results.Channels) > 0 && len(deepgramResp.Results.Channels[0].Alternatives) > 0 {
		alternative := deepgramResp.Results.Channels[0].Alternatives[0]
		transcript := alternative.Transcript

		if transcript != "" {
			// Create metadata
			metadata := map[string]interface{}{
				"provider":   "deepgram",
				"confidence": alternative.Confidence,
				"request_id": deepgramResp.RequestID,
				"model":      deepgramResp.Metadata.ModelInfo,
				"duration":   deepgramResp.Metadata.Duration,
				"channels":   deepgramResp.Metadata.Channels,
				"words":      alternative.Words,
				"paragraphs": alternative.Paragraphs,
			}

			// Add language detection metadata if enabled
			if p.config.DetectLanguage {
				metadata["language_detection_enabled"] = true
				metadata["supported_languages"] = p.config.SupportedLanguages
				metadata["language_confidence_threshold"] = p.config.LanguageConfidenceThreshold
				metadata["accent_aware_models"] = p.config.AccentAwareModels
				metadata["fallback_language"] = p.config.FallbackLanguage
				
				// Include detected language information if available in metadata
				if deepgramResp.Metadata.Language != "" {
					metadata["detected_language"] = deepgramResp.Metadata.Language
					metadata["language_confidence"] = deepgramResp.Metadata.LanguageConfidence
				}
				
				// Include multiple detected languages if available
				if len(deepgramResp.Metadata.DetectedLanguages) > 0 {
					metadata["detected_languages"] = deepgramResp.Metadata.DetectedLanguages
				}
			}

			// Add utterances if available
			if len(deepgramResp.Results.Utterances) > 0 {
				metadata["utterances"] = deepgramResp.Results.Utterances
			}

			// Process language switching if enabled
			if p.languageSwitcher != nil && p.config.DetectLanguage {
				// Create language detection event
				detection := LanguageDetection{
					Timestamp:      time.Now(),
					Language:       deepgramResp.Metadata.Language,
					Confidence:     deepgramResp.Metadata.LanguageConfidence,
					Provider:       "deepgram",
					SegmentID:      deepgramResp.RequestID,
					TranscriptText: transcript,
					WordCount:      len(alternative.Words),
				}
				
				// Add alternatives if available
				if len(deepgramResp.Metadata.DetectedLanguages) > 0 {
					detection.Alternatives = make([]LanguageAlternative, 0)
					for _, detectedLang := range deepgramResp.Metadata.DetectedLanguages {
						detection.Alternatives = append(detection.Alternatives, LanguageAlternative{
							Language:   detectedLang.Language,
							Confidence: detectedLang.Confidence,
						})
					}
				}
				
				// Initialize persistence profile if not exists and get optimal settings
				if p.persistenceService != nil {
					// Get optimal language settings for this call
					if optimalSettings := p.persistenceService.GetOptimalLanguageForCall(callUUID); optimalSettings != nil {
						metadata["optimal_language"] = optimalSettings.PrimaryLanguage
						metadata["optimal_confidence_threshold"] = optimalSettings.ConfidenceThreshold
						metadata["optimal_cooldown"] = optimalSettings.SwitchCooldown
					}
				}
				
				// Process the detection and check for language switch
				shouldSwitch, newLanguage, switchConfidence := p.languageSwitcher.ProcessLanguageDetection(callUUID, detection)
				
				if shouldSwitch {
					metadata["language_switched"] = true
					metadata["previous_language"] = metadata["detected_language"]
					metadata["new_language"] = newLanguage
					metadata["switch_confidence"] = switchConfidence
					
					// Initialize transition smoothing
					if p.transitionSmoother != nil {
						oldLang := metadata["detected_language"].(string)
						p.transitionSmoother.StartTransition(callUUID, oldLang, newLanguage)
					}
					
					p.logger.WithFields(logrus.Fields{
						"call_uuid":        callUUID,
						"previous_language": metadata["detected_language"],
						"new_language":     newLanguage,
						"switch_confidence": switchConfidence,
					}).Info("Language switch detected and processed")
				}
				
				// Apply transition smoothing if active
				if p.transitionSmoother != nil {
					transcriptionSegment := TranscriptionSegment{
						Timestamp:    time.Now(),
						Text:         transcript,
						Language:     deepgramResp.Metadata.Language,
						Confidence:   alternative.Confidence,
						WordCount:    len(alternative.Words),
						SegmentID:    deepgramResp.RequestID,
						Provider:     "deepgram",
						IsFinal:      true,
						Alternatives: detection.Alternatives,
					}
					
					smoothedSegment := p.transitionSmoother.AddTranscriptionSegment(callUUID, transcriptionSegment)
					
					if smoothedSegment != nil {
						// Update transcript and metadata with smoothed results
						transcript = smoothedSegment.Text
						metadata["smoothed_language"] = smoothedSegment.Language
						metadata["smoothed_confidence"] = smoothedSegment.Confidence
						metadata["original_transcript"] = transcriptionSegment.Text
						metadata["transition_applied"] = true
						
						p.logger.WithFields(logrus.Fields{
							"call_uuid":         callUUID,
							"original_text":     transcriptionSegment.Text,
							"smoothed_text":     smoothedSegment.Text,
							"original_language": transcriptionSegment.Language,
							"smoothed_language": smoothedSegment.Language,
						}).Debug("Applied transition smoothing")
					}
				}
				
				// Update language persistence with usage data
				if p.persistenceService != nil {
					usageUpdate := LanguageUsageUpdate{
						Timestamp:       time.Now(),
						Language:        deepgramResp.Metadata.Language,
						Duration:        time.Since(detection.Timestamp),
						Confidence:      deepgramResp.Metadata.LanguageConfidence,
						WordCount:       len(alternative.Words),
						SwitchReason:    "automatic_detection",
						QualityScore:    alternative.Confidence,
						ContextualScore: 0.8, // Default contextual score
						Latency:         time.Since(detection.Timestamp),
					}
					
					if shouldSwitch {
						usageUpdate.SwitchReason = "language_switch"
					}
					
					p.persistenceService.UpdateLanguageUsage(callUUID, usageUpdate)
				}
				
				// Add language switching metadata
				if session, exists := p.languageSwitcher.GetSessionInfo(callUUID); exists {
					metadata["current_language"] = session.CurrentLanguage
					metadata["language_switches"] = session.LanguageSwitchCount
					metadata["language_stability"] = session.StabilityScore
				}
			}

			p.logger.WithFields(logrus.Fields{
				"transcript": transcript,
				"call_uuid":  callUUID,
				"confidence": alternative.Confidence,
				"words":      len(alternative.Words),
			}).Info("Transcription received from Deepgram")

			// Call callback if available
			if p.callback != nil {
				p.callback(callUUID, transcript, true, metadata)
			}

			// Publish to transcription service for real-time streaming
			if p.transcriptionSvc != nil {
				p.transcriptionSvc.PublishTranscription(callUUID, transcript, true, metadata)
			}
		}
	}

	return nil
}

// SetCallback sets the callback function for transcription results
func (p *DeepgramProvider) SetCallback(callback func(callUUID, transcription string, isFinal bool, metadata map[string]interface{})) {
	p.callback = callback
}

// EndCallSession cleans up language switching session for a completed call
func (p *DeepgramProvider) EndCallSession(callUUID string) {
	if p.languageSwitcher != nil {
		p.languageSwitcher.EndCallSession(callUUID)
		p.logger.WithField("call_uuid", callUUID).Debug("Ended language switching session")
	}
	
	if p.transitionSmoother != nil {
		p.transitionSmoother.EndCallSession(callUUID)
		p.logger.WithField("call_uuid", callUUID).Debug("Ended language transition smoothing session")
	}
	
	if p.persistenceService != nil {
		p.persistenceService.EndCallProfile(callUUID)
		p.logger.WithField("call_uuid", callUUID).Debug("Ended language persistence session")
	}
}

// StartCallSession initializes language tracking for a new call
func (p *DeepgramProvider) StartCallSession(callUUID, callerID string) {
	if p.languageSwitcher != nil {
		p.languageSwitcher.StartCallSession(callUUID)
		p.logger.WithField("call_uuid", callUUID).Debug("Started language switching session")
	}
	
	if p.persistenceService != nil {
		userPreferences := &UserLanguagePreferences{
			PreferredLanguages:     p.config.SupportedLanguages,
			FallbackLanguage:       p.config.FallbackLanguage,
			SwitchingSensitivity:   "medium",
			AutoDetectionEnabled:   p.config.DetectLanguage,
			ManualOverrideEnabled:  true,
			LearningEnabled:        true,
			PreferenceUpdatedAt:    time.Now(),
		}
		
		p.persistenceService.StartCallProfile(callUUID, callerID, userPreferences)
		p.logger.WithField("call_uuid", callUUID).Debug("Started language persistence session")
	}
}

// GetLanguageSwitchingMetrics returns current language switching metrics
func (p *DeepgramProvider) GetLanguageSwitchingMetrics() *LanguageSwitchingMetrics {
	if p.languageSwitcher != nil {
		return p.languageSwitcher.GetSwitchingMetrics()
	}
	return nil
}

// GetLanguagePersistenceService returns the language persistence service
func (p *DeepgramProvider) GetLanguagePersistenceService() *LanguagePersistenceService {
	return p.persistenceService
}
