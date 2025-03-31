package stt

import (
	"errors"
)

// Error definitions
var (
	ErrNoProviderAvailable  = errors.New("no speech-to-text provider available")
	ErrProviderNotFound     = errors.New("requested speech-to-text provider not found")
	ErrInitializationFailed = errors.New("provider initialization failed")
	ErrTranscriptionFailed  = errors.New("transcription failed")
)
