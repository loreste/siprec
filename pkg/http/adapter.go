package http

import (
	"github.com/sirupsen/logrus"
)

// SIPHandlerAdapter adapts the SIP handler to provide metrics and session information
// to the HTTP server
type SIPHandlerAdapter struct {
	logger   *logrus.Logger
	sipHandler interface{} // Replace with actual SIP handler interface
}

// NewSIPHandlerAdapter creates a new SIP handler adapter
func NewSIPHandlerAdapter(logger *logrus.Logger, sipHandler interface{}) *SIPHandlerAdapter {
	return &SIPHandlerAdapter{
		logger:     logger,
		sipHandler: sipHandler,
	}
}

// GetActiveCallCount returns the number of active calls
func (a *SIPHandlerAdapter) GetActiveCallCount() int {
	// Call the actual SIP handler method
	// This is a simplified implementation
	if handler, ok := a.sipHandler.(interface{ GetActiveCallCount() int }); ok {
		return handler.GetActiveCallCount()
	}
	
	a.logger.Warn("SIP handler does not implement GetActiveCallCount")
	return 0
}

// GetMetrics returns all metrics
func (a *SIPHandlerAdapter) GetMetrics() map[string]interface{} {
	// This is a simplified implementation
	// In a real implementation, we would get metrics from the SIP handler
	metrics := map[string]interface{}{
		"active_calls": a.GetActiveCallCount(),
	}
	
	// Add more metrics as needed
	if handler, ok := a.sipHandler.(interface{ GetPeakCallCount() int }); ok {
		metrics["peak_calls"] = handler.GetPeakCallCount()
	}
	
	if handler, ok := a.sipHandler.(interface{ GetTotalCallCount() int }); ok {
		metrics["total_calls"] = handler.GetTotalCallCount()
	}
	
	return metrics
}

// GetSessionByID returns session information by ID
func (a *SIPHandlerAdapter) GetSessionByID(id string) (interface{}, error) {
	// Call the SIP handler to get the session
	if handler, ok := a.sipHandler.(interface{ GetSession(id string) (interface{}, error) }); ok {
		return handler.GetSession(id)
	}
	
	a.logger.Warn("SIP handler does not implement GetSession")
	return nil, pkg_errors.NewNotImplemented("GetSession not implemented")
}

// GetAllSessions returns information about all active sessions
func (a *SIPHandlerAdapter) GetAllSessions() ([]interface{}, error) {
	// Call the SIP handler to get all sessions
	if handler, ok := a.sipHandler.(interface{ GetAllSessions() ([]interface{}, error) }); ok {
		return handler.GetAllSessions()
	}
	
	a.logger.Warn("SIP handler does not implement GetAllSessions")
	return nil, pkg_errors.NewNotImplemented("GetAllSessions not implemented")
}

// GetSessionStatistics returns session statistics
func (a *SIPHandlerAdapter) GetSessionStatistics() map[string]interface{} {
	// Call the SIP handler to get session statistics
	if handler, ok := a.sipHandler.(interface{ GetSessionStatistics() map[string]interface{} }); ok {
		return handler.GetSessionStatistics()
	}
	
	// Fallback to basic statistics
	return map[string]interface{}{
		"active_calls":  a.GetActiveCallCount(),
		"metrics_available": false,
	}
}