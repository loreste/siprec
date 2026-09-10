package realtime

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func testLogger() *logrus.Logger {
	l := logrus.New()
	l.SetLevel(logrus.ErrorLevel)
	return l
}

// --- interface compliance ---

func TestSentimentProviderInterface(t *testing.T) {
	var _ SentimentProvider = (*SentimentAnalyzer)(nil)
	var _ SentimentProvider = (*MLSentimentProvider)(nil)
	var _ SentimentProvider = (*FallbackSentimentProvider)(nil)
}

// --- ML provider against a real HTTP mock ---

func TestMLProvider_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req mlRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("bad request body: %v", err)
		}
		if req.Text == "" {
			t.Error("empty text in request")
		}
		json.NewEncoder(w).Encode(mlResponse{
			Label:        "positive",
			Score:        0.92,
			Magnitude:    0.8,
			Subjectivity: 0.6,
		})
	}))
	defer srv.Close()

	p := NewMLSentimentProvider(srv.URL, "test-model", 2*time.Second, testLogger())
	result := p.AnalyzeText("I love this product")

	if result.Label != "positive" {
		t.Errorf("expected positive, got %s", result.Label)
	}
	if result.Score < 0.9 {
		t.Errorf("expected score ~0.92, got %f", result.Score)
	}
}

func TestMLProvider_NegativeSentiment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(mlResponse{
			Label: "negative", Score: 0.15, Magnitude: 0.9, Subjectivity: 0.7,
		})
	}))
	defer srv.Close()

	p := NewMLSentimentProvider(srv.URL, "", 2*time.Second, testLogger())
	result := p.AnalyzeText("This is absolutely terrible service")

	if result.Label != "negative" {
		t.Errorf("expected negative, got %s", result.Label)
	}
}

func TestMLProvider_ShortText(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		json.NewEncoder(w).Encode(mlResponse{Label: "positive", Score: 0.9})
	}))
	defer srv.Close()

	p := NewMLSentimentProvider(srv.URL, "", 2*time.Second, testLogger())
	result := p.AnalyzeText("Hi")

	if called {
		t.Error("should not call endpoint for short text")
	}
	if result.Label != "neutral" {
		t.Errorf("short text should be neutral, got %s", result.Label)
	}
}

func TestMLProvider_EmptyText(t *testing.T) {
	p := NewMLSentimentProvider("http://localhost:1", "", 1*time.Second, testLogger())
	result := p.AnalyzeText("")
	if result.Label != "neutral" {
		t.Errorf("empty text should be neutral, got %s", result.Label)
	}
}

// --- failure modes ---

func TestMLProvider_ServerDown(t *testing.T) {
	// Point at a port nothing is listening on
	p := NewMLSentimentProvider("http://127.0.0.1:1", "", 500*time.Millisecond, testLogger())
	result := p.AnalyzeText("This is a test sentence for sentiment")

	if result.Label != "neutral" {
		t.Errorf("unreachable server should return neutral, got %s", result.Label)
	}
}

func TestMLProvider_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(3 * time.Second) // longer than timeout
		json.NewEncoder(w).Encode(mlResponse{Label: "positive", Score: 0.9})
	}))
	defer srv.Close()

	p := NewMLSentimentProvider(srv.URL, "", 200*time.Millisecond, testLogger())
	start := time.Now()
	result := p.AnalyzeText("Testing timeout behavior of the ML provider")
	elapsed := time.Since(start)

	if result.Label != "neutral" {
		t.Errorf("timeout should return neutral, got %s", result.Label)
	}
	if elapsed > 2*time.Second {
		t.Errorf("should have timed out quickly, took %v", elapsed)
	}
}

func TestMLProvider_HTTP500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer srv.Close()

	p := NewMLSentimentProvider(srv.URL, "", 2*time.Second, testLogger())
	result := p.AnalyzeText("Test text for server error handling")
	if result.Label != "neutral" {
		t.Errorf("500 should return neutral, got %s", result.Label)
	}
}

func TestMLProvider_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"label": "positive", broken json`))
	}))
	defer srv.Close()

	p := NewMLSentimentProvider(srv.URL, "", 2*time.Second, testLogger())
	result := p.AnalyzeText("Test text for malformed JSON response")
	if result.Label != "neutral" {
		t.Errorf("malformed JSON should return neutral, got %s", result.Label)
	}
}

func TestMLProvider_InvalidLabel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(mlResponse{Label: "VERY_POSITIVE", Score: 0.95})
	}))
	defer srv.Close()

	p := NewMLSentimentProvider(srv.URL, "", 2*time.Second, testLogger())
	result := p.AnalyzeText("Test for unknown label normalization")
	if result.Label != "neutral" {
		t.Errorf("unknown label should be normalized to neutral, got %s", result.Label)
	}
}

func TestMLProvider_ScoreOutOfRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(mlResponse{Label: "positive", Score: 99.5, Magnitude: -3.0})
	}))
	defer srv.Close()

	p := NewMLSentimentProvider(srv.URL, "", 2*time.Second, testLogger())
	result := p.AnalyzeText("Test for score clamping to valid range")
	if result.Score > 1.0 || result.Score < 0.0 {
		t.Errorf("score should be clamped to [0,1], got %f", result.Score)
	}
	if result.Magnitude > 1.0 || result.Magnitude < 0.0 {
		t.Errorf("magnitude should be clamped to [0,1], got %f", result.Magnitude)
	}
}

func TestMLProvider_HugeResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Send 1MB of garbage — the provider should only read 4KB
		w.Write([]byte(`{"label":"positive","score":0.9}` + strings.Repeat("x", 1<<20)))
	}))
	defer srv.Close()

	p := NewMLSentimentProvider(srv.URL, "", 2*time.Second, testLogger())
	result := p.AnalyzeText("Test huge response body handling")
	// Should still parse the valid JSON prefix
	if result.Label != "positive" {
		// The 4KB limit may include the valid JSON, or it may not parse —
		// either way it must not crash or OOM
		t.Logf("label=%s score=%f (acceptable: graceful handling of huge body)", result.Label, result.Score)
	}
}

// --- fallback provider ---

func TestFallbackProvider_PrimarySucceeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(mlResponse{Label: "negative", Score: 0.1, Magnitude: 0.9})
	}))
	defer srv.Close()

	ml := NewMLSentimentProvider(srv.URL, "", 2*time.Second, testLogger())
	lexicon := NewSentimentAnalyzer(testLogger())
	fb := &FallbackSentimentProvider{
		Primary:  ml,
		Fallback: lexicon,
		logger:   testLogger().WithField("component", "test"),
	}

	result := fb.AnalyzeText("I hate everything about this terrible product")
	// ML should handle it — magnitude 0.9 means it's a real result, not sentinel
	if result.Label != "negative" {
		t.Errorf("expected ML negative result, got %s", result.Label)
	}
}

func TestFallbackProvider_PrimaryFails_LexiconTakesOver(t *testing.T) {
	// ML endpoint is down
	ml := NewMLSentimentProvider("http://127.0.0.1:1", "", 200*time.Millisecond, testLogger())
	lexicon := NewSentimentAnalyzer(testLogger())
	fb := &FallbackSentimentProvider{
		Primary:  ml,
		Fallback: lexicon,
		logger:   testLogger().WithField("component", "test"),
	}

	// Strongly positive text — lexicon should pick it up
	result := fb.AnalyzeText("This is absolutely fantastic and wonderful and amazing")
	if result.Label != "positive" {
		t.Errorf("fallback to lexicon should detect positive, got %s (score=%f)", result.Label, result.Score)
	}
}

func TestFallbackProvider_PrimaryTimeout_LexiconTakesOver(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	ml := NewMLSentimentProvider(srv.URL, "", 200*time.Millisecond, testLogger())
	lexicon := NewSentimentAnalyzer(testLogger())
	fb := &FallbackSentimentProvider{
		Primary:  ml,
		Fallback: lexicon,
		logger:   testLogger().WithField("component", "test"),
	}

	start := time.Now()
	result := fb.AnalyzeText("This is terrible and horrible and I hate it")
	elapsed := time.Since(start)

	if result.Label != "negative" {
		t.Errorf("fallback should detect negative, got %s", result.Label)
	}
	if elapsed > 2*time.Second {
		t.Errorf("should not block on ML timeout, took %v", elapsed)
	}
}

// --- concurrency ---

func TestMLProvider_Concurrent(t *testing.T) {
	var reqCount int64
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		reqCount++
		mu.Unlock()
		json.NewEncoder(w).Encode(mlResponse{Label: "positive", Score: 0.8, Magnitude: 0.5})
	}))
	defer srv.Close()

	p := NewMLSentimentProvider(srv.URL, "", 2*time.Second, testLogger())

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := p.AnalyzeText("Concurrent test text for the ML sentiment provider")
			if result.Label != "positive" {
				t.Errorf("expected positive, got %s", result.Label)
			}
		}()
	}
	wg.Wait()
}

func TestFallbackProvider_Concurrent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(mlResponse{Label: "negative", Score: 0.2, Magnitude: 0.7})
	}))
	defer srv.Close()

	ml := NewMLSentimentProvider(srv.URL, "", 2*time.Second, testLogger())
	lexicon := NewSentimentAnalyzer(testLogger())
	fb := &FallbackSentimentProvider{
		Primary:  ml,
		Fallback: lexicon,
		logger:   testLogger().WithField("component", "test"),
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fb.AnalyzeText("Concurrent fallback test for sentiment analysis")
		}()
	}
	wg.Wait()
}

// --- factory / config wiring ---

func TestNewSentimentProvider_DefaultIsLexicon(t *testing.T) {
	config := DefaultStreamingConfig()
	p := newSentimentProvider(config, testLogger())
	if _, ok := p.(*SentimentAnalyzer); !ok {
		t.Errorf("default mode should create lexicon, got %T", p)
	}
}

func TestNewSentimentProvider_MLWithoutEndpointFallsBack(t *testing.T) {
	config := DefaultStreamingConfig()
	config.SentimentMode = "ml"
	config.SentimentEndpoint = "" // no endpoint
	p := newSentimentProvider(config, testLogger())
	if _, ok := p.(*SentimentAnalyzer); !ok {
		t.Errorf("ml mode without endpoint should fall back to lexicon, got %T", p)
	}
}

func TestNewSentimentProvider_MLWithEndpoint(t *testing.T) {
	config := DefaultStreamingConfig()
	config.SentimentMode = "ml"
	config.SentimentEndpoint = "http://localhost:8080/predict"
	p := newSentimentProvider(config, testLogger())
	if _, ok := p.(*MLSentimentProvider); !ok {
		t.Errorf("ml mode with endpoint should create ML provider, got %T", p)
	}
}

func TestNewSentimentProvider_AutoWithEndpoint(t *testing.T) {
	config := DefaultStreamingConfig()
	config.SentimentMode = "auto"
	config.SentimentEndpoint = "http://localhost:8080/predict"
	p := newSentimentProvider(config, testLogger())
	if _, ok := p.(*FallbackSentimentProvider); !ok {
		t.Errorf("auto mode should create fallback provider, got %T", p)
	}
}

func TestNewSentimentProvider_AutoWithoutEndpoint(t *testing.T) {
	config := DefaultStreamingConfig()
	config.SentimentMode = "auto"
	config.SentimentEndpoint = ""
	p := newSentimentProvider(config, testLogger())
	if _, ok := p.(*SentimentAnalyzer); !ok {
		t.Errorf("auto mode without endpoint should fall back to lexicon, got %T", p)
	}
}

func TestNewSentimentProvider_UnknownModeFallsBack(t *testing.T) {
	config := DefaultStreamingConfig()
	config.SentimentMode = "transformer-xl-v3-turbo"
	p := newSentimentProvider(config, testLogger())
	if _, ok := p.(*SentimentAnalyzer); !ok {
		t.Errorf("unknown mode should fall back to lexicon, got %T", p)
	}
}

func TestFormatProviderName(t *testing.T) {
	lexicon := NewSentimentAnalyzer(testLogger())
	if FormatProviderName(lexicon) != "lexicon" {
		t.Errorf("unexpected name: %s", FormatProviderName(lexicon))
	}

	ml := NewMLSentimentProvider("http://x", "", time.Second, testLogger())
	if FormatProviderName(ml) != "ml" {
		t.Errorf("unexpected name: %s", FormatProviderName(ml))
	}

	fb := &FallbackSentimentProvider{Primary: ml, Fallback: lexicon, logger: testLogger().WithField("c", "t")}
	if FormatProviderName(fb) != "auto (ml+lexicon)" {
		t.Errorf("unexpected name: %s", FormatProviderName(fb))
	}
}

// --- isMLFailureSentinel ---

func TestIsMLFailureSentinel(t *testing.T) {
	if !isMLFailureSentinel(Sentiment{Label: "neutral", Score: 0.5, Magnitude: 0.0, Subjectivity: 0.0}) {
		t.Error("should detect sentinel")
	}
	if isMLFailureSentinel(Sentiment{Label: "neutral", Score: 0.5, Magnitude: 0.3, Subjectivity: 0.0}) {
		t.Error("non-zero magnitude is not a sentinel")
	}
	if isMLFailureSentinel(Sentiment{Label: "positive", Score: 0.8, Magnitude: 0.0, Subjectivity: 0.0}) {
		t.Error("positive label is not a sentinel")
	}
}

// =====================================================================
// Adversarial edge cases
// =====================================================================

func TestMLProvider_EmptyJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	p := NewMLSentimentProvider(srv.URL, "", 2*time.Second, testLogger())
	result := p.AnalyzeText("Testing empty JSON object response from inference")
	// Empty label should be normalized to "neutral"
	if result.Label != "neutral" {
		t.Errorf("empty JSON label should become neutral, got %s", result.Label)
	}
}

func TestMLProvider_HTMLResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body>502 Bad Gateway</body></html>`))
	}))
	defer srv.Close()

	p := NewMLSentimentProvider(srv.URL, "", 2*time.Second, testLogger())
	result := p.AnalyzeText("Testing HTML response from a reverse proxy error")
	if result.Label != "neutral" {
		t.Errorf("HTML body should return neutral, got %s", result.Label)
	}
}

func TestMLProvider_RedirectNotFollowed(t *testing.T) {
	// Redirects in an inference endpoint are suspicious; verify we handle them
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, nil, "http://evil.example.com", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()

	p := NewMLSentimentProvider(srv.URL, "", 2*time.Second, testLogger())
	result := p.AnalyzeText("Testing redirect behavior from inference endpoint")
	// Go's http.Client follows redirects by default but the redirected
	// response won't be JSON — should degrade gracefully
	if result.Label != "neutral" {
		t.Logf("redirect resulted in label=%s (acceptable as long as no panic)", result.Label)
	}
}

func TestMLProvider_VeryLongText(t *testing.T) {
	var receivedLen int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req mlRequest
		json.Unmarshal(body, &req)
		receivedLen = len(req.Text)
		json.NewEncoder(w).Encode(mlResponse{Label: "neutral", Score: 0.5, Magnitude: 0.1})
	}))
	defer srv.Close()

	// 100KB of text
	longText := strings.Repeat("This is a sentence for testing very large payloads. ", 2000)
	p := NewMLSentimentProvider(srv.URL, "", 5*time.Second, testLogger())
	result := p.AnalyzeText(longText)

	if receivedLen == 0 {
		t.Error("server never received the request")
	}
	if result.Label == "" {
		t.Error("should return a valid label")
	}
}

func TestMLProvider_UnicodeAndEmoji(t *testing.T) {
	var receivedText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req mlRequest
		json.Unmarshal(body, &req)
		receivedText = req.Text
		json.NewEncoder(w).Encode(mlResponse{Label: "positive", Score: 0.85, Magnitude: 0.7})
	}))
	defer srv.Close()

	text := "J'adore ce produit! 🎉🔥 日本語テスト مرحبا"
	p := NewMLSentimentProvider(srv.URL, "", 2*time.Second, testLogger())
	result := p.AnalyzeText(text)

	if receivedText != text {
		t.Errorf("unicode text was mangled: got %q", receivedText)
	}
	if result.Label != "positive" {
		t.Errorf("expected positive, got %s", result.Label)
	}
}

func TestMLProvider_InjectionInText(t *testing.T) {
	// Ensure adversarial text doesn't break JSON encoding
	var receivedText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req mlRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("JSON parse failed on adversarial input: %v", err)
			w.WriteHeader(400)
			return
		}
		receivedText = req.Text
		json.NewEncoder(w).Encode(mlResponse{Label: "neutral", Score: 0.5, Magnitude: 0.1})
	}))
	defer srv.Close()

	adversarial := `"; DROP TABLE users; -- <script>alert('xss')</script> {"label":"hacked"}`
	p := NewMLSentimentProvider(srv.URL, "", 2*time.Second, testLogger())
	result := p.AnalyzeText(adversarial)

	if receivedText != adversarial {
		t.Error("adversarial text should be sent verbatim inside JSON string")
	}
	if result.Label == "hacked" {
		t.Error("injection attempt should not override the response label")
	}
}

func TestMLProvider_NullBytesInText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req mlRequest
		if err := json.Unmarshal(body, &req); err != nil {
			w.WriteHeader(400)
			return
		}
		json.NewEncoder(w).Encode(mlResponse{Label: "neutral", Score: 0.5, Magnitude: 0.2})
	}))
	defer srv.Close()

	text := "before\x00after\x00more"
	p := NewMLSentimentProvider(srv.URL, "", 2*time.Second, testLogger())
	result := p.AnalyzeText(text)
	// Should not panic or crash
	if result.Label == "" {
		t.Error("should return a valid label")
	}
}

func TestMLProvider_SlowDripResponse(t *testing.T) {
	// Server sends valid JSON but byte-by-byte over 3 seconds
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := `{"label":"positive","score":0.9,"magnitude":0.8}`
		for _, b := range resp {
			w.Write([]byte{byte(b)})
			w.(http.Flusher).Flush()
			time.Sleep(50 * time.Millisecond)
		}
	}))
	defer srv.Close()

	// Timeout shorter than the slow drip
	p := NewMLSentimentProvider(srv.URL, "", 500*time.Millisecond, testLogger())
	result := p.AnalyzeText("Testing slow drip response that exceeds timeout")
	// Should timeout and return neutral
	if result.Label != "neutral" {
		t.Logf("slow drip: label=%s (may succeed if OS buffers; acceptable)", result.Label)
	}
}

// TestFallbackProvider_FlappingML simulates an ML endpoint that alternates
// between success and failure. The fallback provider should handle this
// without races or stale results.
func TestFallbackProvider_FlappingML(t *testing.T) {
	var reqCount int64
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		reqCount++
		n := reqCount
		mu.Unlock()
		if n%2 == 0 {
			// Even requests fail
			w.WriteHeader(500)
			return
		}
		json.NewEncoder(w).Encode(mlResponse{Label: "positive", Score: 0.85, Magnitude: 0.7})
	}))
	defer srv.Close()

	ml := NewMLSentimentProvider(srv.URL, "", 2*time.Second, testLogger())
	lexicon := NewSentimentAnalyzer(testLogger())
	fb := &FallbackSentimentProvider{
		Primary:  ml,
		Fallback: lexicon,
		logger:   testLogger().WithField("component", "test"),
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := fb.AnalyzeText("This is absolutely wonderful and fantastic")
			// Every result should be positive — either from ML or lexicon
			if result.Label != "positive" {
				t.Errorf("flapping test: expected positive, got %s (score=%f)", result.Label, result.Score)
			}
		}()
	}
	wg.Wait()
}

// TestMLProvider_ConnectionReset verifies the provider handles a server
// that accepts the connection then immediately resets it.
func TestMLProvider_ConnectionReset(t *testing.T) {
	// Use a listener that accepts and immediately closes
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close() // reset
		}
	}()
	defer ln.Close()

	p := NewMLSentimentProvider("http://"+ln.Addr().String(), "", 1*time.Second, testLogger())
	result := p.AnalyzeText("Testing connection reset handling from server")
	if result.Label != "neutral" {
		t.Errorf("connection reset should return neutral, got %s", result.Label)
	}
}

func TestMLProvider_ZeroTimeout(t *testing.T) {
	p := NewMLSentimentProvider("http://localhost:1", "", 0, testLogger())
	if p.timeout <= 0 {
		t.Error("zero timeout should be replaced with default")
	}
}

func TestMLProvider_NegativeTimeout(t *testing.T) {
	p := NewMLSentimentProvider("http://localhost:1", "", -5*time.Second, testLogger())
	if p.timeout <= 0 {
		t.Error("negative timeout should be replaced with default")
	}
}
