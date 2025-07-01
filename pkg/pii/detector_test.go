package pii

import (
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestPIIDetector(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Reduce log noise in tests

	t.Run("SSN detection and redaction", func(t *testing.T) {
		config := &Config{
			EnabledTypes:   []PIIType{PIITypeSSN},
			RedactionChar:  "*",
			PreserveFormat: true,
		}
		
		detector, err := NewPIIDetector(logger, config)
		if err != nil {
			t.Fatalf("Failed to create PII detector: %v", err)
		}

		testCases := []struct {
			input    string
			expected string
			hasPII   bool
		}{
			{
				input:    "My SSN is 456-78-9012",
				expected: "My SSN is ***-**-9012",
				hasPII:   true,
			},
			{
				input:    "SSN: 456 78 9012",
				expected: "SSN: *** ** 9012",
				hasPII:   true,
			},
			{
				input:    "SSN: 456789012",
				expected: "SSN: *****9012",
				hasPII:   true,
			},
			{
				input:    "Invalid SSN: 000-00-0000",
				expected: "Invalid SSN: 000-00-0000", // Should not be redacted (invalid)
				hasPII:   false,
			},
			{
				input:    "No PII here",
				expected: "No PII here",
				hasPII:   false,
			},
		}

		for _, tc := range testCases {
			result := detector.DetectAndRedact(tc.input)
			
			if result.RedactedText != tc.expected {
				t.Errorf("Expected '%s', got '%s'", tc.expected, result.RedactedText)
			}
			
			if result.HasPII != tc.hasPII {
				t.Errorf("Expected HasPII=%v, got %v", tc.hasPII, result.HasPII)
			}
		}
	})

	t.Run("Credit card detection and redaction", func(t *testing.T) {
		config := &Config{
			EnabledTypes:   []PIIType{PIITypeCreditCard},
			RedactionChar:  "*",
			PreserveFormat: true,
		}
		
		detector, err := NewPIIDetector(logger, config)
		if err != nil {
			t.Fatalf("Failed to create PII detector: %v", err)
		}

		testCases := []struct {
			input    string
			expected string
			hasPII   bool
		}{
			{
				input:    "Card number: 4111-1111-1111-1111",
				expected: "Card number: ****-****-****-1111",
				hasPII:   true,
			},
			{
				input:    "AmEx: 3782 822463 10005",
				expected: "AmEx: **** ****** *0005",
				hasPII:   true,
			},
			{
				input:    "Invalid card: 1234-5678-9012-3456",
				expected: "Invalid card: 1234-5678-9012-3456", // Should not be redacted (invalid Luhn)
				hasPII:   false,
			},
		}

		for _, tc := range testCases {
			result := detector.DetectAndRedact(tc.input)
			
			if result.RedactedText != tc.expected {
				t.Errorf("Expected '%s', got '%s'", tc.expected, result.RedactedText)
			}
			
			if result.HasPII != tc.hasPII {
				t.Errorf("Expected HasPII=%v, got %v", tc.hasPII, result.HasPII)
			}
		}
	})

	t.Run("Phone number detection and redaction", func(t *testing.T) {
		config := &Config{
			EnabledTypes:   []PIIType{PIITypePhone},
			RedactionChar:  "*",
			PreserveFormat: true,
		}
		
		detector, err := NewPIIDetector(logger, config)
		if err != nil {
			t.Fatalf("Failed to create PII detector: %v", err)
		}

		testCases := []struct {
			input    string
			expected string
		}{
			{
				input:    "Call me at (555) 123-4567",
				expected: "Call me at (***) ***-4567",
			},
			{
				input:    "Phone: 555-123-4567",
				expected: "Phone: ***-***-4567",
			},
			{
				input:    "Mobile: 555.123.4567",
				expected: "Mobile: ***.***.4567",
			},
		}

		for _, tc := range testCases {
			result := detector.DetectAndRedact(tc.input)
			
			if result.RedactedText != tc.expected {
				t.Errorf("Expected '%s', got '%s'", tc.expected, result.RedactedText)
			}
			
			if !result.HasPII {
				t.Error("Expected to detect PII")
			}
		}
	})

	t.Run("Email detection and redaction", func(t *testing.T) {
		config := &Config{
			EnabledTypes:   []PIIType{PIITypeEmail},
			RedactionChar:  "*",
			PreserveFormat: true,
		}
		
		detector, err := NewPIIDetector(logger, config)
		if err != nil {
			t.Fatalf("Failed to create PII detector: %v", err)
		}

		testCases := []struct {
			input    string
			expected string
		}{
			{
				input:    "Email me at john.doe@example.com",
				expected: "Email me at j******e@example.com",
			},
			{
				input:    "Contact: test@domain.org",
				expected: "Contact: t**t@domain.org",
			},
		}

		for _, tc := range testCases {
			result := detector.DetectAndRedact(tc.input)
			
			if result.RedactedText != tc.expected {
				t.Errorf("Expected '%s', got '%s'", tc.expected, result.RedactedText)
			}
			
			if !result.HasPII {
				t.Error("Expected to detect PII")
			}
		}
	})

	t.Run("Multiple PII types in one text", func(t *testing.T) {
		config := &Config{
			EnabledTypes:   []PIIType{PIITypeSSN, PIITypeCreditCard, PIITypePhone, PIITypeEmail},
			RedactionChar:  "*",
			PreserveFormat: true,
		}
		
		detector, err := NewPIIDetector(logger, config)
		if err != nil {
			t.Fatalf("Failed to create PII detector: %v", err)
		}

		input := "My SSN is 456-78-9012, card is 4111-1111-1111-1111, phone (555) 123-4567, email john@example.com"
		result := detector.DetectAndRedact(input)

		if !result.HasPII {
			t.Error("Expected to detect PII")
		}

		if len(result.Matches) < 4 {
			t.Errorf("Expected at least 4 PII matches, got %d", len(result.Matches))
		}

		// Should not contain original sensitive data
		sensitiveData := []string{"456-78-9012", "4111-1111-1111-1111", "(555) 123-4567", "john@example.com"}
		for _, sensitive := range sensitiveData {
			if strings.Contains(result.RedactedText, sensitive) {
				t.Errorf("Original sensitive data '%s' still present in redacted text: %s", sensitive, result.RedactedText)
				break
			}
		}
	})

	t.Run("No format preservation", func(t *testing.T) {
		config := &Config{
			EnabledTypes:   []PIIType{PIITypeSSN, PIITypeCreditCard},
			RedactionChar:  "*",
			PreserveFormat: false,
		}
		
		detector, err := NewPIIDetector(logger, config)
		if err != nil {
			t.Fatalf("Failed to create PII detector: %v", err)
		}

		result := detector.DetectAndRedact("SSN: 456-78-9012, Card: 4111-1111-1111-1111")
		
		expected := "SSN: [SSN-REDACTED], Card: [CARD-REDACTED]"
		if result.RedactedText != expected {
			t.Errorf("Expected '%s', got '%s'", expected, result.RedactedText)
		}
	})
}

func TestSSNValidation(t *testing.T) {
	logger := logrus.New()
	detector, _ := NewPIIDetector(logger, nil)

	validSSNs := []string{
		"456-78-9012",
		"789-12-3456",
		"555-66-7777",
	}

	invalidSSNs := []string{
		"000-00-0000",
		"111-11-1111",
		"123-00-1234",
		"123-45-0000",
		"666-12-3456",
		"900-12-3456",
	}

	for _, ssn := range validSSNs {
		if !detector.isValidSSN(ssn) {
			t.Errorf("Expected %s to be valid", ssn)
		}
	}

	for _, ssn := range invalidSSNs {
		if detector.isValidSSN(ssn) {
			t.Errorf("Expected %s to be invalid", ssn)
		}
	}
}

func TestCreditCardValidation(t *testing.T) {
	logger := logrus.New()
	detector, _ := NewPIIDetector(logger, nil)

	// Valid credit card numbers (using test numbers)
	validCards := []string{
		"4111-1111-1111-1111", // Visa test number
		"5555-5555-5555-4444", // MasterCard test number
		"3782-822463-10005",   // AmEx test number
	}

	// Invalid credit card numbers
	invalidCards := []string{
		"1234-5678-9012-3456", // Invalid Luhn
		"0000-0000-0000-0000", // All zeros
		"1111-1111-1111-1111", // All ones
	}

	for _, card := range validCards {
		if !detector.isValidCreditCard(card) {
			t.Errorf("Expected %s to be valid", card)
		}
	}

	for _, card := range invalidCards {
		if detector.isValidCreditCard(card) {
			t.Errorf("Expected %s to be invalid", card)
		}
	}
}