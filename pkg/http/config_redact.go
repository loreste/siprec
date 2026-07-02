package http

import (
	"reflect"
	"strings"

	"siprec-server/pkg/config"
)

// redactedPlaceholder is the value substituted for secret configuration values
// before they are written to an HTTP response.
const redactedPlaceholder = "[REDACTED]"

// secretFieldPatterns are case-insensitive substrings that mark a field name or
// JSON tag as containing sensitive material. Matching is intentionally broad so
// new configuration fields are redacted by default.
var secretFieldPatterns = []string{
	"password",
	"passphrase",
	"secret",
	"api_key",
	"apikey",
	"token",
	"sas",
	"credential",
	"access_key",
	"accesskey",
	"account_key",
	"accountkey",
	"private_key",
	"privatekey",
	"subscription_key",
	"subscriptionkey",
}

// isSecretFieldName reports whether a field name or JSON tag looks like it
// holds a secret value.
func isSecretFieldName(name string) bool {
	lower := strings.ToLower(name)
	for _, pattern := range secretFieldPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// redactConfigSecrets returns a deep copy of the configuration with all
// secret-looking string values replaced by redactedPlaceholder. Empty values
// are left empty so operators can still see which secrets are unset.
func redactConfigSecrets(cfg *config.Config) *config.Config {
	if cfg == nil {
		return nil
	}

	redacted := redactedCopy(reflect.ValueOf(cfg), false)
	return redacted.Interface().(*config.Config)
}

// redactedCopy walks a value with reflection and returns a deep copy in which
// every string reachable through a secret-looking field, map key, or JSON tag
// has been replaced. The original value is never mutated.
func redactedCopy(v reflect.Value, secret bool) reflect.Value {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(redactedCopy(v.Elem(), secret))
		return out

	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(redactedCopy(v.Elem(), secret))
		return out

	case reflect.Struct:
		t := v.Type()
		// Structs with unexported fields (time.Time, etc.) cannot be rebuilt
		// field by field; return them unchanged.
		for i := 0; i < t.NumField(); i++ {
			if t.Field(i).PkgPath != "" {
				return v
			}
		}
		out := reflect.New(t).Elem()
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			fieldSecret := secret || isSecretFieldName(field.Name) || isSecretFieldName(jsonTagName(field))
			out.Field(i).Set(redactedCopy(v.Field(i), fieldSecret))
		}
		return out

	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			entrySecret := secret
			if iter.Key().Kind() == reflect.String && isSecretFieldName(iter.Key().String()) {
				entrySecret = true
			}
			out.SetMapIndex(iter.Key(), redactedCopy(iter.Value(), entrySecret))
		}
		return out

	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(redactedCopy(v.Index(i), secret))
		}
		return out

	case reflect.Array:
		out := reflect.New(v.Type()).Elem()
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(redactedCopy(v.Index(i), secret))
		}
		return out

	case reflect.String:
		if secret && v.String() != "" {
			out := reflect.New(v.Type()).Elem()
			out.SetString(redactedPlaceholder)
			return out
		}
		return v

	default:
		return v
	}
}

// jsonTagName extracts the name portion of a struct field's json tag.
func jsonTagName(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag == "" || tag == "-" {
		return ""
	}
	if idx := strings.Index(tag, ","); idx >= 0 {
		tag = tag[:idx]
	}
	return tag
}

// redactReloadEvent returns a copy of a reload event with secret-looking
// old/new values and validation values redacted, so the reload endpoint never
// echoes configuration secrets.
func redactReloadEvent(event *config.ReloadEvent) *config.ReloadEvent {
	if event == nil {
		return nil
	}

	sanitized := *event
	if len(event.Changes) > 0 {
		sanitized.Changes = make([]config.ConfigChange, len(event.Changes))
		for i, change := range event.Changes {
			change.OldValue = redactValidationValue(change.Field, change.OldValue)
			change.NewValue = redactValidationValue(change.Field, change.NewValue)
			sanitized.Changes[i] = change
		}
	}
	if len(event.Errors) > 0 {
		sanitized.Errors = make([]config.ValidationError, len(event.Errors))
		for i, validationError := range event.Errors {
			validationError.Value = redactValidationValue(validationError.Field, validationError.Value)
			sanitized.Errors[i] = validationError
		}
	}
	if len(event.Warnings) > 0 {
		sanitized.Warnings = make([]config.ValidationWarning, len(event.Warnings))
		for i, validationWarning := range event.Warnings {
			validationWarning.Value = redactValidationValue(validationWarning.Field, validationWarning.Value)
			sanitized.Warnings[i] = validationWarning
		}
	}
	return &sanitized
}

// redactValidationValue redacts a validation error/warning value when the
// associated field path looks secret, so the validate endpoint never echoes
// secrets back in its response.
func redactValidationValue(field string, value interface{}) interface{} {
	if value == nil || !isSecretFieldName(field) {
		return value
	}
	if s, ok := value.(string); ok && s == "" {
		return value
	}
	return redactedPlaceholder
}
