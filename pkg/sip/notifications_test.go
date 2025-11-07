package sip

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"siprec-server/pkg/siprec"
)

func TestMetadataNotifierDeliversEvent(t *testing.T) {
	ch := make(chan NotificationEvent, 1)

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	callbackURL := "http://callback.local/notify"
	notifier := NewMetadataNotifier(logger, []string{callbackURL}, time.Second)
	notifier.client = &http.Client{
		Timeout: time.Second,
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			defer req.Body.Close()
			var event NotificationEvent
			require.NoError(t, json.NewDecoder(req.Body).Decode(&event))
			ch <- event
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
		}),
	}

	session := &siprec.RecordingSession{
		ID:                "session123",
		RecordingState:    "active",
		StateReason:       "normal",
		StateExpires:      time.Now().Add(30 * time.Second).UTC(),
		Participants:      []siprec.Participant{{ID: "participant1"}},
		MediaStreamTypes:  []string{"audio"},
		SessionGroupRoles: map[string]string{"groupA": "primary"},
		PolicyStates: map[string]siprec.PolicyAckStatus{
			"policy-1": {
				Status:       "applied",
				Acknowledged: true,
				ReportedAt:   time.Now().UTC(),
				RawTimestamp: time.Now().UTC().Format(time.RFC3339),
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	notifier.Notify(ctx, session, "call123", "metadata.accepted", map[string]interface{}{
		"test": true,
	})

	select {
	case event := <-ch:
		require.Equal(t, "metadata.accepted", event.Event)
		require.Equal(t, "call123", event.CallID)
		require.Equal(t, "session123", event.SessionID)
		require.Equal(t, "active", event.State)
		require.NotNil(t, event.Metadata)
		require.Equal(t, true, event.Metadata["test"])
	case <-time.After(time.Second):
		t.Fatal("did not receive metadata notification")
	}
}
