package analytics

import (
	"context"
	"sync"

	"github.com/sirupsen/logrus"
)

// Subscriber receives analytics snapshots in real time.
type Subscriber interface {
	OnAnalytics(callID string, snapshot *AnalyticsSnapshot)
}

// SnapshotWriter persists analytics snapshots to external storage.
type SnapshotWriter interface {
	Save(ctx context.Context, snapshot *AnalyticsSnapshot) error
}

// Dispatcher orchestrates analytics processing for transcription events.
type Dispatcher struct {
	logger    *logrus.Logger
	pipeline  *Pipeline
	listeners []Subscriber
	mu        sync.RWMutex
	writer    SnapshotWriter
}

// NewDispatcher creates a dispatcher with the provided pipeline.
func NewDispatcher(logger *logrus.Logger, pipeline *Pipeline) *Dispatcher {
	return &Dispatcher{
		logger:    logger,
		pipeline:  pipeline,
		listeners: make([]Subscriber, 0),
	}
}

// SetSnapshotWriter registers a snapshot writer for durable persistence.
func (d *Dispatcher) SetSnapshotWriter(writer SnapshotWriter) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writer = writer
}

// AddSubscriber registers an analytics subscriber.
func (d *Dispatcher) AddSubscriber(sub Subscriber) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.listeners = append(d.listeners, sub)
}

// RemoveSubscriber removes an analytics subscriber.
func (d *Dispatcher) RemoveSubscriber(sub Subscriber) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for i, s := range d.listeners {
		if s == sub {
			d.listeners[i] = d.listeners[len(d.listeners)-1]
			d.listeners = d.listeners[:len(d.listeners)-1]
			return
		}
	}
}

// HandleTranscript processes a transcription event and notifies subscribers.
func (d *Dispatcher) HandleTranscript(ctx context.Context, event *TranscriptEvent) {
	snapshot, err := d.pipeline.Process(ctx, event)
	if err != nil {
		d.logger.WithError(err).WithField("call_id", event.CallID).Error("Failed to process analytics event")
		return
	}
	if snapshot == nil {
		return
	}

	d.mu.RLock()
	writer := d.writer
	listeners := append([]Subscriber(nil), d.listeners...)
	d.mu.RUnlock()

	if writer != nil {
		if err := writer.Save(ctx, snapshot); err != nil {
			d.logger.WithError(err).WithField("call_id", event.CallID).Warn("Failed to persist analytics snapshot")
		}
	}

	for _, sub := range listeners {
		sub.OnAnalytics(event.CallID, snapshot)
	}
}

// HandleAudioMetrics processes audio quality updates and acoustic events.
func (d *Dispatcher) HandleAudioMetrics(ctx context.Context, callID string, metrics *AudioMetrics, events []AcousticEvent) {
	snapshot, err := d.pipeline.ProcessAudioMetrics(callID, metrics, events)
	if err != nil {
		d.logger.WithError(err).WithField("call_id", callID).Error("Failed to process audio metrics")
		return
	}
	if snapshot == nil {
		return
	}

	d.mu.RLock()
	writer := d.writer
	listeners := append([]Subscriber(nil), d.listeners...)
	d.mu.RUnlock()

	if writer != nil {
		if err := writer.Save(ctx, snapshot); err != nil {
			d.logger.WithError(err).WithField("call_id", callID).Warn("Failed to persist audio metrics snapshot")
		}
	}

	for _, sub := range listeners {
		sub.OnAnalytics(callID, snapshot)
	}
}

// CompleteCall finalizes analytics for a call and notifies subscribers with final snapshot.
func (d *Dispatcher) CompleteCall(ctx context.Context, callID string) {
	snapshot, err := d.pipeline.CompleteCall(callID)
	if err != nil {
		d.logger.WithError(err).WithField("call_id", callID).Error("Failed to finalize analytics state")
		return
	}
	if snapshot == nil {
		return
	}

	d.mu.RLock()
	writer := d.writer
	listeners := append([]Subscriber(nil), d.listeners...)
	d.mu.RUnlock()

	if writer != nil {
		if err := writer.Save(ctx, snapshot); err != nil {
			d.logger.WithError(err).WithField("call_id", callID).Warn("Failed to persist final analytics snapshot")
		}
	}

	for _, sub := range listeners {
		sub.OnAnalytics(callID, snapshot)
	}
}
