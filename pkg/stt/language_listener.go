package stt

import (
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
)

// LanguageRoutingListener observes transcription metadata and routes calls to the appropriate STT provider.
type LanguageRoutingListener struct {
	logger  *logrus.Logger
	manager *ProviderManager
}

// NewLanguageRoutingListener creates a new language routing listener.
func NewLanguageRoutingListener(logger *logrus.Logger, manager *ProviderManager) *LanguageRoutingListener {
	return &LanguageRoutingListener{
		logger:  logger,
		manager: manager,
	}
}

// OnTranscription inspects metadata for detected language information and updates provider routing.
func (l *LanguageRoutingListener) OnTranscription(callUUID string, transcription string, isFinal bool, metadata map[string]interface{}) {
	if l.manager == nil || metadata == nil {
		return
	}

	language := extractLanguage(metadata)
	if language == "" {
		return
	}

	if routed := l.manager.RouteCallByLanguage(callUUID, language); routed != "" {
		l.logger.WithFields(logrus.Fields{
			"call_uuid": callUUID,
			"language":  language,
			"provider":  routed,
		}).Debug("Language routing update applied")
	}
}

func extractLanguage(metadata map[string]interface{}) string {
	possibleKeys := []string{
		"detected_language",
		"smoothed_language",
		"language",
		"optimal_language",
	}

	for _, key := range possibleKeys {
		if value, ok := metadata[key]; ok {
			if lang := normalizeLanguage(value); lang != "" {
				return lang
			}
		}
	}

	// Some providers return nested metadata e.g., {"language": {"code": "en-US"}}
	if nested, ok := metadata["language"].(map[string]interface{}); ok {
		if code, ok := nested["code"]; ok {
			if lang := normalizeLanguage(code); lang != "" {
				return lang
			}
		}
	}

	return ""
}

func normalizeLanguage(value interface{}) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}
