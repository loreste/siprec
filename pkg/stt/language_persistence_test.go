package stt

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

func newPersistenceTestService(t *testing.T, storage string) *LanguagePersistenceService {
	t.Helper()

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	service := NewLanguagePersistenceService(logger)
	config := service.persistenceConfig
	config.PersistenceStorage = storage
	if storage == "file" {
		config.FileDirectory = t.TempDir()
	}
	service.SetPersistenceConfig(config)
	return service
}

func runTestCall(service *LanguagePersistenceService, callUUID, callerID, language string) {
	service.StartCallProfile(callUUID, callerID, nil)
	service.UpdateLanguageUsage(callUUID, LanguageUsageUpdate{
		Timestamp:    time.Now(),
		Language:     language,
		Duration:     10 * time.Second,
		Confidence:   0.92,
		WordCount:    25,
		QualityScore: 0.85,
	})
	service.EndCallProfile(callUUID)
}

func TestFileProfilePersistenceRoundTrip(t *testing.T) {
	service := newPersistenceTestService(t, "file")

	runTestCall(service, "call-file-1", "caller-1", "es-ES")

	// Profile is removed from memory at call end, so this must hit the file backend.
	profile, err := service.LoadCallProfile("call-file-1")
	if err != nil {
		t.Fatalf("LoadCallProfile failed: %v", err)
	}
	if profile.CallUUID != "call-file-1" {
		t.Errorf("unexpected call UUID: %q", profile.CallUUID)
	}
	if len(profile.PreferredLanguages) == 0 || profile.PreferredLanguages[0].Language != "es-ES" {
		t.Errorf("expected es-ES preference, got %+v", profile.PreferredLanguages)
	}

	// Profile files must be written with 0600 permissions.
	path := filepath.Join(service.fileDirectory, "call-file-1.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected profile file to exist: %v", err)
	}
	if perms := info.Mode().Perm(); perms != 0o600 {
		t.Errorf("expected 0600 permissions, got %o", perms)
	}

	// No leftover temp files from the atomic write.
	entries, err := os.ReadDir(service.fileDirectory)
	if err != nil {
		t.Fatalf("failed to list profile directory: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Errorf("leftover temporary file found: %s", entry.Name())
		}
	}
}

func TestFileProfileHistoricalPreferences(t *testing.T) {
	service := newPersistenceTestService(t, "file")

	runTestCall(service, "call-file-2", "repeat-caller", "fr-FR")

	// A new call from the same caller should be seeded with learned preferences.
	profile := service.StartCallProfile("call-file-3", "repeat-caller", nil)

	found := false
	for _, pref := range profile.PreferredLanguages {
		if pref.Language == "fr-FR" && pref.PreferenceSource == "learned" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected learned fr-FR preference from historical profile, got %+v", profile.PreferredLanguages)
	}
}

func TestFileProfileKeySanitization(t *testing.T) {
	service := newPersistenceTestService(t, "file")

	runTestCall(service, "../../evil/key", "", "en-US")

	// The sanitized file must stay inside the configured directory.
	entries, err := os.ReadDir(service.fileDirectory)
	if err != nil {
		t.Fatalf("failed to list profile directory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one profile file, got %d", len(entries))
	}
	if name := entries[0].Name(); strings.ContainsAny(name, "/\\") || strings.HasPrefix(name, ".") {
		t.Errorf("unsafe profile file name: %q", name)
	}

	profile, err := service.LoadCallProfile("../../evil/key")
	if err != nil {
		t.Fatalf("LoadCallProfile failed for sanitized key: %v", err)
	}
	if profile.CallUUID != "../../evil/key" {
		t.Errorf("unexpected call UUID: %q", profile.CallUUID)
	}
}

func TestRedisProfilePersistenceRoundTrip(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { client.Close() })

	service := newPersistenceTestService(t, "redis")
	service.ConfigureRedis(client, "test:lang_profiles", 2*time.Second)

	runTestCall(service, "call-redis-1", "caller-redis", "de-DE")

	// Stored as JSON under the prefixed key with the retention TTL applied.
	key := "test:lang_profiles:call:call-redis-1"
	if !server.Exists(key) {
		t.Fatalf("expected Redis key %s to exist", key)
	}
	if ttl := server.TTL(key); ttl <= 0 {
		t.Errorf("expected a positive TTL on profile key, got %v", ttl)
	}

	profile, err := service.LoadCallProfile("call-redis-1")
	if err != nil {
		t.Fatalf("LoadCallProfile failed: %v", err)
	}
	if profile.CallUUID != "call-redis-1" {
		t.Errorf("unexpected call UUID: %q", profile.CallUUID)
	}
	if len(profile.PreferredLanguages) == 0 || profile.PreferredLanguages[0].Language != "de-DE" {
		t.Errorf("expected de-DE preference, got %+v", profile.PreferredLanguages)
	}

	// Historical preferences load through the caller index.
	next := service.StartCallProfile("call-redis-2", "caller-redis", nil)
	found := false
	for _, pref := range next.PreferredLanguages {
		if pref.Language == "de-DE" && pref.PreferenceSource == "learned" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected learned de-DE preference from Redis history, got %+v", next.PreferredLanguages)
	}
}

func TestLoadCallProfileNotFound(t *testing.T) {
	service := newPersistenceTestService(t, "file")

	if _, err := service.LoadCallProfile("missing-call"); err == nil {
		t.Fatal("expected an error for a missing profile")
	}
}

func TestSanitizeProfileKey(t *testing.T) {
	cases := map[string]string{
		"abc-123":     "abc-123",
		"../../etc":   "_.._etc",
		"":            "profile",
		"a b/c":       "a_b_c",
		"...":         "profile",
		"tel:+15551":  "tel__15551",
		"UPPER.lower": "UPPER.lower",
	}

	for input, want := range cases {
		if got := sanitizeProfileKey(input); got != want {
			t.Errorf("sanitizeProfileKey(%q) = %q, want %q", input, got, want)
		}
	}
}
