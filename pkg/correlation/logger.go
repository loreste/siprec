package correlation

import (
	"context"

	"github.com/sirupsen/logrus"
)

// LoggerFromContext returns a logrus.Entry with correlation ID fields from the context
// This is a standalone function that can be used without wrapping the logger
func LoggerFromContext(ctx context.Context, logger *logrus.Logger) *logrus.Entry {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	fields := logrus.Fields{}

	if id := FromContext(ctx); !id.IsEmpty() {
		fields["correlation_id"] = id.String()
	}

	if ip := ClientIPFromContext(ctx); ip != "" {
		fields["client_ip"] = ip
	}

	if method := MethodFromContext(ctx); method != "" {
		fields["method"] = method
	}

	if len(fields) == 0 {
		return logger.WithFields(logrus.Fields{})
	}

	return logger.WithFields(fields)
}

// WithCorrelation adds correlation ID to a logrus.Entry
func WithCorrelation(entry *logrus.Entry, id ID) *logrus.Entry {
	if id.IsEmpty() {
		return entry
	}
	return entry.WithField("correlation_id", id.String())
}

// ContextFields extracts all correlation fields from a context as a logrus.Fields map
func ContextFields(ctx context.Context) logrus.Fields {
	fields := logrus.Fields{}

	if id := FromContext(ctx); !id.IsEmpty() {
		fields["correlation_id"] = id.String()
	}

	if ip := ClientIPFromContext(ctx); ip != "" {
		fields["client_ip"] = ip
	}

	if method := MethodFromContext(ctx); method != "" {
		fields["method"] = method
	}

	return fields
}

// FieldsWithCorrelation returns a logrus.Fields map with the correlation ID included
func FieldsWithCorrelation(id ID, additionalFields logrus.Fields) logrus.Fields {
	fields := logrus.Fields{}

	if !id.IsEmpty() {
		fields["correlation_id"] = id.String()
	}

	for k, v := range additionalFields {
		fields[k] = v
	}

	return fields
}
