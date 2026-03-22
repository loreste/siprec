package media

import (
	"runtime"
	"testing"
	"time"

	"github.com/pion/rtp"
)

// TestJitterBuffer_NoGoroutineLeak ensures jitter buffer doesn't leak goroutines
func TestJitterBuffer_NoGoroutineLeak(t *testing.T) {
	// Get baseline goroutine count
	runtime.GC()
	baselineGoroutines := runtime.NumGoroutine()

	const iterations = 100

	for i := 0; i < iterations; i++ {
		jb := NewJitterBuffer(JitterBufferConfig{
			MaxSize:  10,
			MaxDelay: 50 * time.Millisecond,
		})

		// Push some packets
		for j := 0; j < 20; j++ {
			pkt := &rtp.Packet{
				Header: rtp.Header{
					SequenceNumber: uint16(j),
					Timestamp:      uint32(j * 160),
					SSRC:           12345,
				},
				Payload: make([]byte, 20),
			}
			jb.Push(pkt, nil, time.Now())
		}

		// Pop all
		for jb.Pop() != nil {
		}

		// Clear
		jb.Clear()
	}

	// Allow goroutines to settle
	time.Sleep(100 * time.Millisecond)
	runtime.GC()

	finalGoroutines := runtime.NumGoroutine()
	goroutineDiff := finalGoroutines - baselineGoroutines

	// Allow small variance (e.g., GC goroutines)
	if goroutineDiff > 5 {
		t.Errorf("Potential goroutine leak: started with %d, ended with %d (diff: %d)",
			baselineGoroutines, finalGoroutines, goroutineDiff)
	} else {
		t.Logf("Goroutine count: baseline=%d, final=%d, diff=%d",
			baselineGoroutines, finalGoroutines, goroutineDiff)
	}
}

// TestBufferedPipe_NoGoroutineLeak ensures buffered pipe doesn't leak goroutines
func TestBufferedPipe_NoGoroutineLeak(t *testing.T) {
	runtime.GC()
	baselineGoroutines := runtime.NumGoroutine()

	const iterations = 100

	for i := 0; i < iterations; i++ {
		reader, writer := NewBufferedPipe(4096)

		// Write some data
		data := make([]byte, 512)
		for j := 0; j < 5; j++ {
			_, _ = writer.Write(data)
		}

		// Close writer first
		_ = writer.Close()

		// Read until EOF
		buf := make([]byte, 512)
		for {
			_, err := reader.Read(buf)
			if err != nil {
				break
			}
		}

		// Close reader
		_ = reader.Close()
	}

	time.Sleep(100 * time.Millisecond)
	runtime.GC()

	finalGoroutines := runtime.NumGoroutine()
	goroutineDiff := finalGoroutines - baselineGoroutines

	if goroutineDiff > 5 {
		t.Errorf("Potential goroutine leak: started with %d, ended with %d (diff: %d)",
			baselineGoroutines, finalGoroutines, goroutineDiff)
	} else {
		t.Logf("Goroutine count: baseline=%d, final=%d, diff=%d",
			baselineGoroutines, finalGoroutines, goroutineDiff)
	}
}

// TestLegTiming_NoGoroutineLeak ensures WAV combining doesn't leak goroutines
func TestLegTiming_NoGoroutineLeak(t *testing.T) {
	runtime.GC()
	baselineGoroutines := runtime.NumGoroutine()

	const iterations = 20

	for i := 0; i < iterations; i++ {
		dir := t.TempDir()
		leg0 := dir + "/leg0.wav"
		leg1 := dir + "/leg1.wav"
		output := dir + "/combined.wav"

		writeTestWAV(t, leg0, []int16{100, 200, 300})
		writeTestWAV(t, leg1, []int16{1000, 2000, 3000})

		legs := []LegTiming{
			{Path: leg0, SampleRate: 8000, FirstRTPTime: time.Now()},
			{Path: leg1, SampleRate: 8000, FirstRTPTime: time.Now().Add(10 * time.Millisecond)},
		}

		_ = CombineWAVRecordingsAligned(output, legs)
	}

	time.Sleep(100 * time.Millisecond)
	runtime.GC()

	finalGoroutines := runtime.NumGoroutine()
	goroutineDiff := finalGoroutines - baselineGoroutines

	if goroutineDiff > 5 {
		t.Errorf("Potential goroutine leak: started with %d, ended with %d (diff: %d)",
			baselineGoroutines, finalGoroutines, goroutineDiff)
	} else {
		t.Logf("Goroutine count: baseline=%d, final=%d, diff=%d",
			baselineGoroutines, finalGoroutines, goroutineDiff)
	}
}
