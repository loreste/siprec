package resources

import (
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// RTPLimiter enforces limits on concurrent RTP streams
type RTPLimiter struct {
	maxStreams    int64
	activeCount   int64
	totalCreated  int64
	totalRejected int64
	logger        *logrus.Entry

	// Rate limiting for logging (accessed atomically via Unix nano)
	lastRejectLogNano int64
	rejectCount       int64
}

// NewRTPLimiter creates a new RTP stream limiter
func NewRTPLimiter(maxStreams int, logger *logrus.Logger) *RTPLimiter {
	return &RTPLimiter{
		maxStreams: int64(maxStreams),
		logger:     logger.WithField("component", "rtp_limiter"),
	}
}

// Acquire attempts to acquire an RTP stream slot
func (rl *RTPLimiter) Acquire() bool {
	for {
		current := atomic.LoadInt64(&rl.activeCount)
		if current >= rl.maxStreams {
			rl.handleRejection()
			return false
		}

		if atomic.CompareAndSwapInt64(&rl.activeCount, current, current+1) {
			atomic.AddInt64(&rl.totalCreated, 1)
			return true
		}
		// CAS failed, retry
	}
}

// TryAcquire attempts to acquire without blocking
func (rl *RTPLimiter) TryAcquire() bool {
	current := atomic.LoadInt64(&rl.activeCount)
	if current >= rl.maxStreams {
		return false
	}
	return atomic.CompareAndSwapInt64(&rl.activeCount, current, current+1)
}

// Release releases an RTP stream slot
func (rl *RTPLimiter) Release() {
	current := atomic.AddInt64(&rl.activeCount, -1)
	if current < 0 {
		// Shouldn't happen, but protect against underflow
		atomic.StoreInt64(&rl.activeCount, 0)
	}
}

// ActiveCount returns the current number of active streams
func (rl *RTPLimiter) ActiveCount() int64 {
	return atomic.LoadInt64(&rl.activeCount)
}

// MaxStreams returns the maximum allowed streams
func (rl *RTPLimiter) MaxStreams() int64 {
	return rl.maxStreams
}

// AvailableSlots returns the number of available stream slots
func (rl *RTPLimiter) AvailableSlots() int64 {
	available := rl.maxStreams - atomic.LoadInt64(&rl.activeCount)
	if available < 0 {
		return 0
	}
	return available
}

// UsagePercent returns current usage as a percentage
func (rl *RTPLimiter) UsagePercent() float64 {
	return float64(atomic.LoadInt64(&rl.activeCount)) / float64(rl.maxStreams) * 100
}

// Stats returns limiter statistics
func (rl *RTPLimiter) Stats() (active, created, rejected int64) {
	return atomic.LoadInt64(&rl.activeCount),
		atomic.LoadInt64(&rl.totalCreated),
		atomic.LoadInt64(&rl.totalRejected)
}

func (rl *RTPLimiter) handleRejection() {
	atomic.AddInt64(&rl.totalRejected, 1)
	atomic.AddInt64(&rl.rejectCount, 1)

	// Rate-limit rejection logging to avoid log spam (atomic time comparison)
	nowNano := time.Now().UnixNano()
	lastLogNano := atomic.LoadInt64(&rl.lastRejectLogNano)
	if nowNano-lastLogNano > int64(10*time.Second) {
		// Try to update the last log time atomically
		if atomic.CompareAndSwapInt64(&rl.lastRejectLogNano, lastLogNano, nowNano) {
			count := atomic.SwapInt64(&rl.rejectCount, 0)

			rl.logger.WithFields(logrus.Fields{
				"active_streams": atomic.LoadInt64(&rl.activeCount),
				"max_streams":    rl.maxStreams,
				"rejected_count": count,
				"total_rejected": atomic.LoadInt64(&rl.totalRejected),
			}).Warn("RTP stream limit reached, rejecting new streams")
		}
	}
}

// SetMaxStreams updates the maximum allowed streams
func (rl *RTPLimiter) SetMaxStreams(max int) {
	atomic.StoreInt64(&rl.maxStreams, int64(max))
	rl.logger.WithField("max_streams", max).Info("RTP stream limit updated")
}

// Reset resets the limiter (for testing)
func (rl *RTPLimiter) Reset() {
	atomic.StoreInt64(&rl.activeCount, 0)
	atomic.StoreInt64(&rl.totalCreated, 0)
	atomic.StoreInt64(&rl.totalRejected, 0)
	atomic.StoreInt64(&rl.rejectCount, 0)
}

// WaitForSlot waits for a slot to become available with timeout
func (rl *RTPLimiter) WaitForSlot(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	backoff := time.Millisecond

	for time.Now().Before(deadline) {
		if rl.Acquire() {
			return true
		}

		time.Sleep(backoff)
		if backoff < 100*time.Millisecond {
			backoff *= 2
		}
	}

	return false
}
