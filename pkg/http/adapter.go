package http

import (
	"github.com/sirupsen/logrus"
)

// SIPHandlerAdapter adapts the SIP handler to provide metrics and session information
// to the HTTP server
type SIPHandlerAdapter struct {
	logger     *logrus.Logger
	sipHandler SIPHandler // Strictly typed interface
}

// SIPHandler defines the interface required by the adapter
type SIPHandler interface {
	GetActiveCallCount() int
	GetAllSessions() ([]interface{}, error)
	GetSession(id string) (interface{}, error)
	GetSessionStatistics() map[string]interface{}
}

// NewSIPHandlerAdapter creates a new SIP handler adapter
func NewSIPHandlerAdapter(logger *logrus.Logger, sipHandler interface{}) *SIPHandlerAdapter {
	return &SIPHandlerAdapter{
		logger:     logger,
		sipHandler: sipHandler.(SIPHandler), // Ensure it implements our interface
	}
}

// GetActiveCallCount returns the number of active calls
func (a *SIPHandlerAdapter) GetActiveCallCount() int {
	return a.sipHandler.GetActiveCallCount()
}

// GetMetrics returns all metrics
func (a *SIPHandlerAdapter) GetMetrics() map[string]interface{} {
	metrics := map[string]interface{}{
		"active_calls": a.GetActiveCallCount(),
	}

	// Merge with full statistics
	stats := a.sipHandler.GetSessionStatistics()
	for k, v := range stats {
		metrics[k] = v
	}

	return metrics
}

// GetSessionByID returns session information by ID
func (a *SIPHandlerAdapter) GetSessionByID(id string) (interface{}, error) {
	return a.sipHandler.GetSession(id)
}

// GetAllSessions returns information about all active sessions
func (a *SIPHandlerAdapter) GetAllSessions() ([]interface{}, error) {
	return a.sipHandler.GetAllSessions()
}

// GetSessionStatistics returns session statistics
func (a *SIPHandlerAdapter) GetSessionStatistics() map[string]interface{} {
	return a.sipHandler.GetSessionStatistics()
}
