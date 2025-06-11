package util

import (
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/sirupsen/logrus"
)

// PanicHandler provides centralized panic recovery and logging
type PanicHandler struct {
	logger *logrus.Logger
}

// NewPanicHandler creates a new panic handler
func NewPanicHandler(logger *logrus.Logger) *PanicHandler {
	return &PanicHandler{
		logger: logger,
	}
}

// Recover recovers from panics and logs them
func (ph *PanicHandler) Recover(component string) {
	if r := recover(); r != nil {
		// Get stack trace
		stack := debug.Stack()
		
		// Get caller information
		pc, file, line, ok := runtime.Caller(2)
		var caller string
		if ok {
			fn := runtime.FuncForPC(pc)
			if fn != nil {
				caller = fmt.Sprintf("%s:%d %s", file, line, fn.Name())
			} else {
				caller = fmt.Sprintf("%s:%d", file, line)
			}
		}

		// Log the panic with full context
		ph.logger.WithFields(logrus.Fields{
			"component":   component,
			"panic_value": r,
			"caller":      caller,
			"stack_trace": string(stack),
		}).Error("Panic recovered")
	}
}

// RecoverWithCallback recovers from panics and executes a callback
func (ph *PanicHandler) RecoverWithCallback(component string, callback func(interface{})) {
	if r := recover(); r != nil {
		// Log the panic
		stack := debug.Stack()
		pc, file, line, ok := runtime.Caller(2)
		var caller string
		if ok {
			fn := runtime.FuncForPC(pc)
			if fn != nil {
				caller = fmt.Sprintf("%s:%d %s", file, line, fn.Name())
			} else {
				caller = fmt.Sprintf("%s:%d", file, line)
			}
		}

		ph.logger.WithFields(logrus.Fields{
			"component":   component,
			"panic_value": r,
			"caller":      caller,
			"stack_trace": string(stack),
		}).Error("Panic recovered")
		
		// Execute callback if provided
		if callback != nil {
			// Protect the callback itself from panics
			func() {
				defer func() {
					if cbPanic := recover(); cbPanic != nil {
						ph.logger.WithFields(logrus.Fields{
							"component":      component,
							"callback_panic": cbPanic,
						}).Error("Panic in panic recovery callback")
					}
				}()
				callback(r)
			}()
		}
	}
}

// WrapGoroutine wraps a goroutine function with panic recovery
func (ph *PanicHandler) WrapGoroutine(component string, fn func()) func() {
	return func() {
		defer ph.Recover(component)
		fn()
	}
}

// SafeGo starts a goroutine with panic recovery
func (ph *PanicHandler) SafeGo(component string, fn func()) {
	go ph.WrapGoroutine(component, fn)()
}

// WrapHTTPHandler wraps an HTTP handler with panic recovery
func (ph *PanicHandler) WrapHTTPHandler(component string, fn func()) func() {
	return func() {
		defer ph.RecoverWithCallback(component, func(panicValue interface{}) {
			// Additional HTTP-specific panic handling could go here
		})
		fn()
	}
}

// Global panic handler instance
var globalPanicHandler *PanicHandler

// SetGlobalPanicHandler sets the global panic handler
func SetGlobalPanicHandler(logger *logrus.Logger) {
	globalPanicHandler = NewPanicHandler(logger)
}

// GetGlobalPanicHandler returns the global panic handler
func GetGlobalPanicHandler() *PanicHandler {
	return globalPanicHandler
}

// Global convenience functions

// SafeRecover recovers from panics using the global handler
func SafeRecover(component string) {
	if globalPanicHandler != nil {
		globalPanicHandler.Recover(component)
	}
}

// SafeRecoverWithCallback recovers from panics with callback using the global handler
func SafeRecoverWithCallback(component string, callback func(interface{})) {
	if globalPanicHandler != nil {
		globalPanicHandler.RecoverWithCallback(component, callback)
	}
}

// SafeGoGlobal starts a goroutine with panic recovery using the global handler
func SafeGoGlobal(component string, fn func()) {
	if globalPanicHandler != nil {
		globalPanicHandler.SafeGo(component, fn)
	} else {
		// Fallback to basic recovery if no global handler
		go func() {
			defer func() {
				if r := recover(); r != nil {
					// Basic logging if no handler available
					fmt.Printf("Panic in %s: %v\n", component, r)
				}
			}()
			fn()
		}()
	}
}