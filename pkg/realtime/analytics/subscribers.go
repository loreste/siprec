package analytics

import (
	"encoding/json"
	"log"
)

// SnapshotLogger is a simple subscriber that logs snapshots.
type SnapshotLogger struct{}

func (s *SnapshotLogger) OnAnalytics(callID string, snapshot *AnalyticsSnapshot) {
	payload, err := json.Marshal(snapshot)
	if err != nil {
		log.Printf("analytics snapshot marshal failed: %v", err)
		return
	}
	log.Printf("analytics snapshot: %s", payload)
}
