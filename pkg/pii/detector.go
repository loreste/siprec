package pii

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/sirupsen/logrus"
)

// PIIDetector detects and redacts personally identifiable information from text
type PIIDetector struct {
	logger           *logrus.Logger
	ssnRegex         *regexp.Regexp
	creditCardRegex  *regexp.Regexp
	phoneRegex       *regexp.Regexp
	emailRegex       *regexp.Regexp
	enabledTypes     map[PIIType]bool
	redactionChar    string
	preserveFormat   bool
}

// PIIType represents different types of PII that can be detected
type PIIType string

const (
	PIITypeSSN        PIIType = "ssn"
	PIITypeCreditCard PIIType = "credit_card"
	PIITypePhone      PIIType = "phone"
	PIITypeEmail      PIIType = "email"
)

// PIIMatch represents a detected PII instance
type PIIMatch struct {
	Type     PIIType `json:"type"`
	Original string  `json:"original"`
	Redacted string  `json:"redacted"`
	Start    int     `json:"start"`
	End      int     `json:"end"`
	Context  string  `json:"context"`
}

// PIIDetectionResult contains the results of PII detection
type PIIDetectionResult struct {
	OriginalText  string     `json:"original_text"`
	RedactedText  string     `json:"redacted_text"`
	Matches       []PIIMatch `json:"matches"`
	HasPII        bool       `json:"has_pii"`
	ProcessedAt   string     `json:"processed_at"`
}

// Config holds configuration for PII detection
type Config struct {
	EnabledTypes   []PIIType `json:"enabled_types"`
	RedactionChar  string    `json:"redaction_char"`
	PreserveFormat bool      `json:"preserve_format"`
	ContextLength  int       `json:"context_length"`
}

// NewPIIDetector creates a new PII detector with the given configuration
func NewPIIDetector(logger *logrus.Logger, config *Config) (*PIIDetector, error) {
	if config == nil {
		config = &Config{
			EnabledTypes:  []PIIType{PIITypeSSN, PIITypeCreditCard, PIITypePhone, PIITypeEmail},
			RedactionChar: "*",
			PreserveFormat: true,
		}
	}

	detector := &PIIDetector{
		logger:        logger,
		redactionChar: config.RedactionChar,
		preserveFormat: config.PreserveFormat,
		enabledTypes:  make(map[PIIType]bool),
	}

	// Set enabled types
	for _, piiType := range config.EnabledTypes {
		detector.enabledTypes[piiType] = true
	}

	// Compile regex patterns
	var err error

	// SSN patterns: 123-45-6789, 123 45 6789, 123456789
	detector.ssnRegex, err = regexp.Compile(`\b(?:\d{3}[-\s]?\d{2}[-\s]?\d{4})\b`)
	if err != nil {
		return nil, err
	}

	// Credit card patterns: supports major card types
	// Visa: 4xxx-xxxx-xxxx-xxxx (16 digits)
	// MasterCard: 5xxx-xxxx-xxxx-xxxx (16 digits)
	// AmEx: 3xxx-xxxxxx-xxxxx (15 digits)
	// Discover: 6xxx-xxxx-xxxx-xxxx (16 digits)
	detector.creditCardRegex, err = regexp.Compile(`\b(?:4\d{3}[-\s]?\d{4}[-\s]?\d{4}[-\s]?\d{4}|5\d{3}[-\s]?\d{4}[-\s]?\d{4}[-\s]?\d{4}|3\d{3}[-\s]?\d{6}[-\s]?\d{5}|6\d{3}[-\s]?\d{4}[-\s]?\d{4}[-\s]?\d{4})\b`)
	if err != nil {
		return nil, err
	}

	// Phone patterns: (123) 456-7890, 123-456-7890, 123.456.7890, 1234567890
	detector.phoneRegex, err = regexp.Compile(`\b(?:\+?1[-.\s]?)?\(?([0-9]{3})\)?[-.\s]?([0-9]{3})[-.\s]?([0-9]{4})\b`)
	if err != nil {
		return nil, err
	}

	// Email patterns: basic email validation
	detector.emailRegex, err = regexp.Compile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`)
	if err != nil {
		return nil, err
	}

	return detector, nil
}

// DetectAndRedact detects PII in text and returns redacted version with detection results
func (d *PIIDetector) DetectAndRedact(text string) *PIIDetectionResult {
	result := &PIIDetectionResult{
		OriginalText: text,
		RedactedText: text,
		Matches:      make([]PIIMatch, 0),
		HasPII:       false,
	}

	// Process each enabled PII type
	if d.enabledTypes[PIITypeSSN] {
		d.detectSSN(result)
	}
	if d.enabledTypes[PIITypeCreditCard] {
		d.detectCreditCard(result)
	}
	if d.enabledTypes[PIITypePhone] {
		d.detectPhone(result)
	}
	if d.enabledTypes[PIITypeEmail] {
		d.detectEmail(result)
	}

	result.HasPII = len(result.Matches) > 0

	if result.HasPII {
		d.logger.WithFields(logrus.Fields{
			"pii_matches": len(result.Matches),
			"text_length": len(text),
		}).Info("PII detected and redacted")
	}

	return result
}

// detectSSN detects Social Security Numbers
func (d *PIIDetector) detectSSN(result *PIIDetectionResult) {
	matches := d.ssnRegex.FindAllStringSubmatch(result.RedactedText, -1)
	matchIndices := d.ssnRegex.FindAllStringIndex(result.RedactedText, -1)

	for i, match := range matches {
		if len(match) > 0 && d.isValidSSN(match[0]) {
			indices := matchIndices[i]
			original := match[0]
			redacted := d.redactSSN(original)
			
			piiMatch := PIIMatch{
				Type:     PIITypeSSN,
				Original: original,
				Redacted: redacted,
				Start:    indices[0],
				End:      indices[1],
				Context:  d.getContext(result.OriginalText, indices[0], indices[1]),
			}
			
			result.Matches = append(result.Matches, piiMatch)
			result.RedactedText = strings.Replace(result.RedactedText, original, redacted, 1)
		}
	}
}

// detectCreditCard detects credit card numbers
func (d *PIIDetector) detectCreditCard(result *PIIDetectionResult) {
	matches := d.creditCardRegex.FindAllStringSubmatch(result.RedactedText, -1)
	matchIndices := d.creditCardRegex.FindAllStringIndex(result.RedactedText, -1)

	for i, match := range matches {
		if len(match) > 0 && d.isValidCreditCard(match[0]) {
			indices := matchIndices[i]
			original := match[0]
			redacted := d.redactCreditCard(original)
			
			piiMatch := PIIMatch{
				Type:     PIITypeCreditCard,
				Original: original,
				Redacted: redacted,
				Start:    indices[0],
				End:      indices[1],
				Context:  d.getContext(result.OriginalText, indices[0], indices[1]),
			}
			
			result.Matches = append(result.Matches, piiMatch)
			result.RedactedText = strings.Replace(result.RedactedText, original, redacted, 1)
		}
	}
}

// detectPhone detects phone numbers
func (d *PIIDetector) detectPhone(result *PIIDetectionResult) {
	matches := d.phoneRegex.FindAllStringSubmatch(result.RedactedText, -1)
	matchIndices := d.phoneRegex.FindAllStringIndex(result.RedactedText, -1)

	for i, match := range matches {
		if len(match) > 0 {
			indices := matchIndices[i]
			original := match[0]
			redacted := d.redactPhone(original)
			
			piiMatch := PIIMatch{
				Type:     PIITypePhone,
				Original: original,
				Redacted: redacted,
				Start:    indices[0],
				End:      indices[1],
				Context:  d.getContext(result.OriginalText, indices[0], indices[1]),
			}
			
			result.Matches = append(result.Matches, piiMatch)
			result.RedactedText = strings.Replace(result.RedactedText, original, redacted, 1)
		}
	}
}

// detectEmail detects email addresses
func (d *PIIDetector) detectEmail(result *PIIDetectionResult) {
	matches := d.emailRegex.FindAllStringSubmatch(result.RedactedText, -1)
	matchIndices := d.emailRegex.FindAllStringIndex(result.RedactedText, -1)

	for i, match := range matches {
		if len(match) > 0 {
			indices := matchIndices[i]
			original := match[0]
			redacted := d.redactEmail(original)
			
			piiMatch := PIIMatch{
				Type:     PIITypeEmail,
				Original: original,
				Redacted: redacted,
				Start:    indices[0],
				End:      indices[1],
				Context:  d.getContext(result.OriginalText, indices[0], indices[1]),
			}
			
			result.Matches = append(result.Matches, piiMatch)
			result.RedactedText = strings.Replace(result.RedactedText, original, redacted, 1)
		}
	}
}

// isValidSSN performs basic validation on SSN format
func (d *PIIDetector) isValidSSN(ssn string) bool {
	// Remove separators and check length
	digits := strings.ReplaceAll(strings.ReplaceAll(ssn, "-", ""), " ", "")
	if len(digits) != 9 {
		return false
	}

	// Check for invalid patterns
	invalidPatterns := []string{
		"000000000", "111111111", "222222222", "333333333", "444444444",
		"555555555", "666666666", "777777777", "888888888", "999999999",
		"123456789", "987654321",
	}

	for _, invalid := range invalidPatterns {
		if digits == invalid {
			return false
		}
	}

	// Check for valid area numbers (first 3 digits)
	areaNumber := digits[:3]
	if areaNumber == "000" || areaNumber == "666" || areaNumber[0] == '9' {
		return false
	}

	// Check for valid group number (middle 2 digits)
	groupNumber := digits[3:5]
	if groupNumber == "00" {
		return false
	}

	// Check for valid serial number (last 4 digits)
	serialNumber := digits[5:9]
	return serialNumber != "0000"
}

// isValidCreditCard performs Luhn algorithm validation on credit card numbers
func (d *PIIDetector) isValidCreditCard(cardNumber string) bool {
	// Remove separators
	digits := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(cardNumber, "-", ""), " ", ""), ".", "")
	
	// Check length
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}

	// Check for all zeros or all same digits
	firstDigit := digits[0]
	allSame := true
	for i := 1; i < len(digits); i++ {
		if digits[i] != firstDigit {
			allSame = false
			break
		}
	}
	if allSame {
		return false
	}

	// Luhn algorithm
	sum := 0
	alternate := false
	
	for i := len(digits) - 1; i >= 0; i-- {
		digit := int(digits[i] - '0')
		
		if alternate {
			digit *= 2
			if digit > 9 {
				digit = (digit % 10) + 1
			}
		}
		
		sum += digit
		alternate = !alternate
	}
	
	return sum%10 == 0
}

// redactSSN creates redacted version of SSN
func (d *PIIDetector) redactSSN(ssn string) string {
	if d.preserveFormat {
		// Keep separators but redact digits: XXX-XX-1234 (show last 4)
		result := ""
		digitCount := 0
		for _, char := range ssn {
			if unicode.IsDigit(char) {
				digitCount++
				if digitCount > 5 { // Show last 4 digits
					result += string(char)
				} else {
					result += d.redactionChar
				}
			} else {
				result += string(char)
			}
		}
		return result
	}
	return "[SSN-REDACTED]"
}

// redactCreditCard creates redacted version of credit card
func (d *PIIDetector) redactCreditCard(cardNumber string) string {
	if d.preserveFormat {
		// Keep separators but redact digits: XXXX-XXXX-XXXX-1234 (show last 4)
		result := ""
		digitCount := 0
		totalDigits := 0
		
		// Count total digits first
		for _, char := range cardNumber {
			if unicode.IsDigit(char) {
				totalDigits++
			}
		}
		
		for _, char := range cardNumber {
			if unicode.IsDigit(char) {
				digitCount++
				if digitCount > totalDigits-4 { // Show last 4 digits
					result += string(char)
				} else {
					result += d.redactionChar
				}
			} else {
				result += string(char)
			}
		}
		return result
	}
	return "[CARD-REDACTED]"
}

// redactPhone creates redacted version of phone number
func (d *PIIDetector) redactPhone(phone string) string {
	if d.preserveFormat {
		// Keep format but redact: (XXX) XXX-1234 (show last 4)
		result := ""
		digitCount := 0
		for _, char := range phone {
			if unicode.IsDigit(char) {
				digitCount++
				if digitCount > 6 { // Show last 4 digits
					result += string(char)
				} else {
					result += d.redactionChar
				}
			} else {
				result += string(char)
			}
		}
		return result
	}
	return "[PHONE-REDACTED]"
}

// redactEmail creates redacted version of email
func (d *PIIDetector) redactEmail(email string) string {
	if d.preserveFormat {
		// Keep domain but redact local part: xxx@domain.com
		parts := strings.Split(email, "@")
		if len(parts) == 2 {
			localPart := parts[0]
			domain := parts[1]
			
			if len(localPart) <= 2 {
				return strings.Repeat(d.redactionChar, len(localPart)) + "@" + domain
			}
			
			// Show first and last character, redact middle
			redactedLocal := string(localPart[0]) + strings.Repeat(d.redactionChar, len(localPart)-2) + string(localPart[len(localPart)-1])
			return redactedLocal + "@" + domain
		}
	}
	return "[EMAIL-REDACTED]"
}

// getContext extracts context around PII match
func (d *PIIDetector) getContext(text string, start, end int) string {
	contextLength := 20 // characters before and after
	
	contextStart := start - contextLength
	if contextStart < 0 {
		contextStart = 0
	}
	
	contextEnd := end + contextLength
	if contextEnd > len(text) {
		contextEnd = len(text)
	}
	
	return text[contextStart:contextEnd]
}

// GetStats returns statistics about PII detection
func (d *PIIDetector) GetStats() map[string]interface{} {
	stats := map[string]interface{}{
		"enabled_types":   []string{},
		"redaction_char":  d.redactionChar,
		"preserve_format": d.preserveFormat,
	}
	
	enabledTypes := make([]string, 0)
	for piiType, enabled := range d.enabledTypes {
		if enabled {
			enabledTypes = append(enabledTypes, string(piiType))
		}
	}
	stats["enabled_types"] = enabledTypes
	
	return stats
}