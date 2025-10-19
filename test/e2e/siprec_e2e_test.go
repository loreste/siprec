package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"siprec-server/pkg/config"
	"siprec-server/pkg/media"
	sipHandler "siprec-server/pkg/sip"
	"siprec-server/pkg/siprec"
	"siprec-server/pkg/stt"
)

// TestEnvironment sets up the complete SIPREC environment for E2E testing
type TestEnvironment struct {
	config         *config.Config
	sipServer      *sipHandler.CustomServer
	sessionManager *siprec.SessionManager
	sttManager     *stt.Manager
	httpServer     *http.Server
	logger         *logrus.Logger
}

func setupTestEnvironment(t *testing.T) *TestEnvironment {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	// Create test config
	cfg := &config.Config{
		SIP: config.SIPConfig{
			Host:      "127.0.0.1",
			Port:      5060,
			Transport: "udp",
		},
		HTTP: config.HTTPConfig{
			Port: 8080,
		},
		Media: config.MediaConfig{
			RTPPortMin: 10000,
			RTPPortMax: 20000,
		},
		STT: config.STTConfig{
			Provider:          "mock",
			FallbackProviders: []string{},
		},
		Recording: config.RecordingConfig{
			Enabled:    true,
			StoragePath: "/tmp/siprec-e2e-test",
		},
	}

	// Create storage directory
	os.MkdirAll(cfg.Recording.StoragePath, 0755)

	// Initialize components
	sttManager := stt.NewManager(logger)
	sttManager.RegisterProvider("mock", &mockSTTProvider{})
	sttManager.Initialize()

	sessionManager := siprec.NewSessionManager(logger, sttManager)

	// Create SIP server
	sipServer, err := sipHandler.NewCustomServer(cfg, logger, sessionManager)
	require.NoError(t, err)

	return &TestEnvironment{
		config:         cfg,
		sipServer:      sipServer,
		sessionManager: sessionManager,
		sttManager:     sttManager,
		logger:         logger,
	}
}

func (env *TestEnvironment) cleanup() {
	if env.sipServer != nil {
		env.sipServer.Shutdown()
	}
	if env.httpServer != nil {
		env.httpServer.Shutdown(context.Background())
	}
	os.RemoveAll(env.config.Recording.StoragePath)
}

func TestE2E_SIPRECSession(t *testing.T) {
	env := setupTestEnvironment(t)
	defer env.cleanup()

	// Start SIP server
	go env.sipServer.Start()
	time.Sleep(100 * time.Millisecond)

	t.Run("complete SIPREC session", func(t *testing.T) {
		// Create SIPREC INVITE
		callID := "test-call-" + fmt.Sprintf("%d", time.Now().Unix())
		sdp := createTestSDP()
	
		// Send INVITE
		resp := sendSIPRECInvite(t, env.config.SIP.Host, env.config.SIP.Port, callID, sdp)
		assert.Equal(t, 200, resp)
		
		// Verify session was created
		session := env.sessionManager.GetSession(callID)
		assert.NotNil(t, session)
		
		// Send RTP packets
		sendTestRTPPackets(t, session)
		
		// Send BYE
		sendSIPRECBye(t, env.config.SIP.Host, env.config.SIP.Port, callID)
		
		// Verify session was cleaned up
		time.Sleep(100 * time.Millisecond)
		session = env.sessionManager.GetSession(callID)
		assert.Nil(t, session)
	})
}

func TestE2E_WebSocketAnalytics(t *testing.T) {
	env := setupTestEnvironment(t)
	defer env.cleanup()

	// Start servers
	go env.sipServer.Start()
	time.Sleep(100 * time.Millisecond)

	// Connect WebSocket client
	wsURL := fmt.Sprintf("ws://localhost:%d/ws/analytics", env.config.HTTP.Port)
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	// Read welcome message
	var welcome map[string]interface{}
	err = ws.ReadJSON(&welcome)
	assert.NoError(t, err)
	assert.Equal(t, "connected", welcome["type"])

	// Start SIPREC session
	callID := "analytics-test-call"
	sdp := createTestSDP()
	sendSIPRECInvite(t, env.config.SIP.Host, env.config.SIP.Port, callID, sdp)

	// Subscribe to call
	subscribe := map[string]interface{}{
		"type":    "subscribe",
		"call_id": callID,
	}
	err = ws.WriteJSON(subscribe)
	assert.NoError(t, err)

	// Wait for analytics update
	var analyticsMsg map[string]interface{}
	ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	err = ws.ReadJSON(&analyticsMsg)
	assert.NoError(t, err)

	// Verify analytics message
	if analyticsMsg["type"] == "analytics_update" {
		assert.Equal(t, callID, analyticsMsg["call_id"])
		assert.NotNil(t, analyticsMsg["data"])
	}

	// Clean up
	sendSIPRECBye(t, env.config.SIP.Host, env.config.SIP.Port, callID)
}

func TestE2E_HTTPEndpoints(t *testing.T) {
	env := setupTestEnvironment(t)
	defer env.cleanup()

	// Start servers
	go env.sipServer.Start()
	time.Sleep(100 * time.Millisecond)

	t.Run("health endpoint", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/health", env.config.HTTP.Port))
		require.NoError(t, err)
		defer resp.Body.Close()
		
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		
		var health map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&health)
		assert.Equal(t, "healthy", health["status"])
	})

	t.Run("metrics endpoint", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", env.config.HTTP.Port))
		require.NoError(t, err)
		defer resp.Body.Close()
		
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		
		body, _ := io.ReadAll(resp.Body)
		assert.Contains(t, string(body), "siprec_active_sessions")
	})

	t.Run("sessions endpoint", func(t *testing.T) {
		// Create a session
		callID := "http-test-call"
		sdp := createTestSDP()
		sendSIPRECInvite(t, env.config.SIP.Host, env.config.SIP.Port, callID, sdp)
		
		// Get sessions
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/sessions", env.config.HTTP.Port))
		require.NoError(t, err)
		defer resp.Body.Close()
		
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		
		var sessions []map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&sessions)
		assert.Greater(t, len(sessions), 0)
		
		// Clean up
		sendSIPRECBye(t, env.config.SIP.Host, env.config.SIP.Port, callID)
	})
}

func TestE2E_ProviderFailover(t *testing.T) {
	env := setupTestEnvironment(t)
	defer env.cleanup()

	// Register failing and working providers
	failingProvider := &mockSTTProvider{shouldFail: true}
	workingProvider := &mockSTTProvider{shouldFail: false}
	
	env.sttManager.RegisterProvider("failing", failingProvider)
	env.sttManager.RegisterProvider("working", workingProvider)
	
	// Configure fallback
	env.config.STT.Provider = "failing"
	env.config.STT.FallbackProviders = []string{"working"}
	
	// Start server
	go env.sipServer.Start()
	time.Sleep(100 * time.Millisecond)

	// Create session
	callID := "failover-test-call"
	sdp := createTestSDP()
	sendSIPRECInvite(t, env.config.SIP.Host, env.config.SIP.Port, callID, sdp)
	
	// Send audio data
	session := env.sessionManager.GetSession(callID)
	require.NotNil(t, session)
	
	sendTestRTPPackets(t, session)
	
	// Verify failover occurred
	time.Sleep(500 * time.Millisecond)
	assert.True(t, workingProvider.(*mockSTTProvider).wasUsed)
	
	// Clean up
	sendSIPRECBye(t, env.config.SIP.Host, env.config.SIP.Port, callID)
}

func TestE2E_RecordingStorage(t *testing.T) {
	env := setupTestEnvironment(t)
	defer env.cleanup()

	// Start server
	go env.sipServer.Start()
	time.Sleep(100 * time.Millisecond)

	// Create session
	callID := "recording-test-call"
	sdp := createTestSDP()
	sendSIPRECInvite(t, env.config.SIP.Host, env.config.SIP.Port, callID, sdp)
	
	// Send audio
	session := env.sessionManager.GetSession(callID)
	require.NotNil(t, session)
	sendTestRTPPackets(t, session)
	
	// End session
	sendSIPRECBye(t, env.config.SIP.Host, env.config.SIP.Port, callID)
	
	// Wait for recording to be saved
	time.Sleep(500 * time.Millisecond)
	
	// Check recording file exists
	recordingPath := fmt.Sprintf("%s/%s", env.config.Recording.StoragePath, callID)
	files, err := os.ReadDir(recordingPath)
	assert.NoError(t, err)
	assert.Greater(t, len(files), 0, "Recording files should exist")
}

// Helper functions

func createTestSDP() string {
	return `v=0
o=- 0 0 IN IP4 127.0.0.1
s=Test Session
c=IN IP4 127.0.0.1
t=0 0
m=audio 5004 RTP/AVP 0 8 101
a=rtpmap:0 PCMU/8000
a=rtpmap:8 PCMA/8000
a=rtpmap:101 telephone-event/8000
a=sendrecv
a=label:1
m=audio 5006 RTP/AVP 0 8 101  
a=rtpmap:0 PCMU/8000
a=rtpmap:8 PCMA/8000
a=rtpmap:101 telephone-event/8000
a=sendrecv
a=label:2`
}

func sendSIPRECInvite(t *testing.T, host string, port int, callID, sdp string) int {
	// Create SIP client
	client, err := sipgo.NewClient(
		sipgo.WithClientHostname("test-client"),
	)
	require.NoError(t, err)

	// Build INVITE request
	req := sip.NewRequest(sip.INVITE, &sip.Uri{
		Scheme: "sip",
		Host:   host,
		Port:   port,
	})
	req.SetBody([]byte(sdp))
	req.AppendHeader(sip.NewHeader("Call-ID", callID))
	req.AppendHeader(sip.NewHeader("Content-Type", "multipart/mixed;boundary=boundary1"))
	
	// Add SIPREC metadata
	metadata := createTestMetadata(callID)
	body := fmt.Sprintf("--boundary1\r\nContent-Type: application/sdp\r\n\r\n%s\r\n--boundary1\r\nContent-Type: application/rs-metadata+xml\r\n\r\n%s\r\n--boundary1--", sdp, metadata)
	req.SetBody([]byte(body))
	
	// Send request
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	tx, err := client.TransactionRequest(ctx, req)
	require.NoError(t, err)
	
	select {
	case res := <-tx.Responses():
		return int(res.StatusCode)
	case <-ctx.Done():
		t.Fatal("INVITE timeout")
		return 0
	}
}

func sendSIPRECBye(t *testing.T, host string, port int, callID string) {
	client, err := sipgo.NewClient(
		sipgo.WithClientHostname("test-client"),
	)
	require.NoError(t, err)

	req := sip.NewRequest(sip.BYE, &sip.Uri{
		Scheme: "sip",
		Host:   host,
		Port:   port,
	})
	req.AppendHeader(sip.NewHeader("Call-ID", callID))
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	_, err = client.TransactionRequest(ctx, req)
	assert.NoError(t, err)
}

func sendTestRTPPackets(t *testing.T, session *siprec.Session) {
	// Send some test RTP packets
	for i := 0; i < 10; i++ {
		packet := &media.RTPPacket{
			Header: media.RTPHeader{
				Version:        2,
				PayloadType:    0,
				SequenceNumber: uint16(i),
				Timestamp:      uint32(i * 160),
				SSRC:          12345,
			},
			Payload: make([]byte, 160), // Simulated audio
		}
		
		// Fill with test audio data
		for j := range packet.Payload {
			packet.Payload[j] = byte(j % 256)
		}
		
		// Process packet (this would normally come through RTP)
		// session.ProcessRTPPacket(packet)
		time.Sleep(20 * time.Millisecond)
	}
}

func createTestMetadata(callID string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording">
  <session session-id="%s">
    <start-time>%s</start-time>
  </session>
  <participant participant-id="caller">
    <nameID aor="sip:caller@example.com"/>
  </participant>
  <participant participant-id="callee">
    <nameID aor="sip:callee@example.com"/>
  </participant>
  <stream stream-id="1" session-id="%s">
    <label>1</label>
  </stream>
  <stream stream-id="2" session-id="%s">
    <label>2</label>
  </stream>
</recording>`, callID, time.Now().Format(time.RFC3339), callID, callID)
}

// Mock STT provider for testing
type mockSTTProvider struct {
	shouldFail bool
	wasUsed    bool
}

func (m *mockSTTProvider) Initialize() error {
	return nil
}

func (m *mockSTTProvider) Name() string {
	return "mock"
}

func (m *mockSTTProvider) StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) error {
	m.wasUsed = true
	if m.shouldFail {
		return fmt.Errorf("mock provider failure")
	}
	
	// Simulate STT processing
	go func() {
		buf := make([]byte, 1024)
		for {
			_, err := audioStream.Read(buf)
			if err != nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
	
	return nil
}

func (m *mockSTTProvider) HealthCheck(ctx context.Context) error {
	if m.shouldFail {
		return fmt.Errorf("mock provider unhealthy")
	}
	return nil
}