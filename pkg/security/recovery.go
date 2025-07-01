package security

import (
	"fmt"
	"runtime/debug"
	
	"github.com/sirupsen/logrus"
)

// PanicHandler provides consistent panic recovery across goroutines
type PanicHandler struct {
	logger *logrus.Logger
}

// NewPanicHandler creates a new panic handler
func NewPanicHandler(logger *logrus.Logger) *PanicHandler {
	return &PanicHandler{logger: logger}
}

// Recover recovers from panics and logs them
func (ph *PanicHandler) Recover(context string) {
	if r := recover(); r != nil {
		// Get stack trace
		stack := debug.Stack()
		
		// Log the panic with full context
		ph.logger.WithFields(logrus.Fields{
			"panic":   fmt.Sprintf("%v", r),
			"context": context,
			"stack":   string(stack),
		}).Error("Recovered from panic")
		
		// Optionally, send to monitoring service
		// metrics.IncrementPanicCounter(context)
	}
}

// SafeGo runs a function in a goroutine with panic recovery
func (ph *PanicHandler) SafeGo(fn func(), context string) {
	go func() {
		defer ph.Recover(context)
		fn()
	}()
}

// SafeGoWithLogger runs a function with a logger in a goroutine with panic recovery
func (ph *PanicHandler) SafeGoWithLogger(fn func(*logrus.Entry), context string) {
	go func() {
		defer ph.Recover(context)
		logger := ph.logger.WithField("goroutine", context)
		fn(logger)
	}()
}

// WithRecovery wraps a function with panic recovery (for use in defer)
func WithRecovery(fn func(), logger *logrus.Logger, context string) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			logger.WithFields(logrus.Fields{
				"panic":   fmt.Sprintf("%v", r),
				"context": context,
				"stack":   string(stack),
			}).Error("Recovered from panic")
		}
	}()
	fn()
}