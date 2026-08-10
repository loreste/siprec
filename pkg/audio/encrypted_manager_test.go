package audio

import "testing"

func TestSafeFileComponentRemovesPathTraversal(t *testing.T) {
	tests := map[string]string{
		"../../etc/passwd":      "passwd",
		"..\\..\\windows\\file": "file",
		"normal-session_123":    "normal-session_123",
		"...":                   "session",
		"":                      "session",
	}

	for input, expected := range tests {
		if got := safeFileComponent(input); got != expected {
			t.Fatalf("safeFileComponent(%q) = %q, want %q", input, got, expected)
		}
	}
}
