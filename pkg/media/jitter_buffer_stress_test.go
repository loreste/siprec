package media

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/pion/rtp"
)

// TestJitterBuffer_ConcurrentAccess tests for race conditions under concurrent access
func TestJitterBuffer_ConcurrentAccess(t *testing.T) {
	jb := NewJitterBuffer(JitterBufferConfig{
		MaxSize:  10,
		MaxDelay: 100 * time.Millisecond,
	})

	var wg sync.WaitGroup
	const numWriters = 5
	const numReaders = 3
	const packetsPerWriter = 100

	// Start writers
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			baseSeq := uint16(writerID * packetsPerWriter)
			for i := 0; i < packetsPerWriter; i++ {
				pkt := &rtp.Packet{
					Header: rtp.Header{
						SequenceNumber: baseSeq + uint16(i),
						Timestamp:      uint32(i * 160),
						SSRC:           uint32(writerID),
					},
					Payload: make([]byte, 20),
				}
				jb.Push(pkt, nil, time.Now())
				// Small sleep to simulate real packet arrival
				time.Sleep(time.Microsecond * 10)
			}
		}(w)
	}

	// Start readers
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < packetsPerWriter*numWriters/numReaders; i++ {
				_ = jb.Pop()
				time.Sleep(time.Microsecond * 15)
			}
		}()
	}

	wg.Wait()

	// Drain remaining
	remaining := jb.Flush()
	t.Logf("Remaining packets after concurrent test: %d", len(remaining))
}

// TestJitterBuffer_MemoryLeak checks for memory leaks by running many iterations
func TestJitterBuffer_MemoryLeak(t *testing.T) {
	// Force GC and get baseline memory
	runtime.GC()
	var baselineStats runtime.MemStats
	runtime.ReadMemStats(&baselineStats)

	const iterations = 1000
	const packetsPerIteration = 100

	for iter := 0; iter < iterations; iter++ {
		jb := NewJitterBuffer(JitterBufferConfig{
			MaxSize:  20,
			MaxDelay: 50 * time.Millisecond,
		})

		// Push packets
		for i := 0; i < packetsPerIteration; i++ {
			pkt := &rtp.Packet{
				Header: rtp.Header{
					SequenceNumber: uint16(i),
					Timestamp:      uint32(i * 160),
					SSRC:           12345,
				},
				Payload: make([]byte, 20),
			}
			jb.Push(pkt, nil, time.Now())
		}

		// Pop all packets
		for {
			if jb.Pop() == nil {
				break
			}
		}

		// Clear the buffer
		jb.Clear()
	}

	// Force GC and check memory
	runtime.GC()
	var finalStats runtime.MemStats
	runtime.ReadMemStats(&finalStats)

	// Calculate memory growth
	memGrowth := int64(finalStats.Alloc) - int64(baselineStats.Alloc)

	// Allow some growth but flag excessive growth (> 10MB would indicate a leak)
	const maxAllowedGrowth = 10 * 1024 * 1024
	if memGrowth > maxAllowedGrowth {
		t.Errorf("Potential memory leak: memory grew by %d bytes after %d iterations", memGrowth, iterations)
	} else {
		t.Logf("Memory growth: %d bytes after %d iterations (within acceptable range)", memGrowth, iterations)
	}
}

// TestJitterBuffer_RapidPushPop tests rapid push/pop cycles
func TestJitterBuffer_RapidPushPop(t *testing.T) {
	jb := NewJitterBuffer(JitterBufferConfig{
		MaxSize:  5,
		MaxDelay: 20 * time.Millisecond,
	})

	const cycles = 10000
	for i := 0; i < cycles; i++ {
		pkt := &rtp.Packet{
			Header: rtp.Header{
				SequenceNumber: uint16(i),
				Timestamp:      uint32(i * 160),
				SSRC:           12345,
			},
			Payload: make([]byte, 20),
		}
		jb.Push(pkt, nil, time.Now())

		// Pop immediately
		_ = jb.Pop()
	}

	// Should be empty or nearly empty
	if jb.Len() > 5 {
		t.Errorf("Buffer should be nearly empty, got %d packets", jb.Len())
	}
}

// TestBufferedPipe_ConcurrentAccess tests the buffered pipe for race conditions
func TestBufferedPipe_ConcurrentAccess(t *testing.T) {
	reader, writer := NewBufferedPipe(4096)

	var wg sync.WaitGroup
	const numWrites = 1000
	const chunkSize = 320

	// Writer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer writer.Close()
		for i := 0; i < numWrites; i++ {
			data := make([]byte, chunkSize)
			for j := range data {
				data[j] = byte(i)
			}
			_, err := writer.Write(data)
			if err != nil {
				t.Errorf("Write error: %v", err)
				return
			}
		}
	}()

	// Reader goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, chunkSize)
		totalRead := 0
		for {
			n, err := reader.Read(buf)
			if err != nil {
				break
			}
			totalRead += n
		}
		t.Logf("Total bytes read: %d", totalRead)
	}()

	wg.Wait()
}

// TestBufferedPipe_MemoryLeak checks buffered pipe for memory leaks
func TestBufferedPipe_MemoryLeak(t *testing.T) {
	runtime.GC()
	var baselineStats runtime.MemStats
	runtime.ReadMemStats(&baselineStats)

	const iterations = 500
	const dataSize = 512 // Smaller than buffer to avoid overflow

	for iter := 0; iter < iterations; iter++ {
		reader, writer := NewBufferedPipe(4096)

		var wg sync.WaitGroup

		// Writer goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()
			data := make([]byte, dataSize)
			for i := 0; i < 5; i++ {
				_, _ = writer.Write(data)
			}
			_ = writer.Close()
		}()

		// Reader goroutine - read until EOF
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, dataSize)
			for {
				_, err := reader.Read(buf)
				if err != nil {
					break
				}
			}
			_ = reader.Close()
		}()

		wg.Wait()
	}

	runtime.GC()
	var finalStats runtime.MemStats
	runtime.ReadMemStats(&finalStats)

	memGrowth := int64(finalStats.Alloc) - int64(baselineStats.Alloc)
	const maxAllowedGrowth = 10 * 1024 * 1024
	if memGrowth > maxAllowedGrowth {
		t.Errorf("Potential memory leak: memory grew by %d bytes", memGrowth)
	} else {
		t.Logf("Memory growth: %d bytes (within acceptable range)", memGrowth)
	}
}

// TestWAVCombiner_ConcurrentAccess tests WAV combining for race conditions
func TestWAVCombiner_ConcurrentAccess(t *testing.T) {
	// This test runs multiple WAV combine operations concurrently
	var wg sync.WaitGroup
	const numConcurrent = 5

	for i := 0; i < numConcurrent; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			dir := t.TempDir()
			leg0 := dir + "/leg0.wav"
			leg1 := dir + "/leg1.wav"
			output := dir + "/combined.wav"

			// Create test WAV files
			writeTestWAV(t, leg0, []int16{100, 200, 300})
			writeTestWAV(t, leg1, []int16{1000, 2000, 3000})

			// Combine
			legs := []LegTiming{
				{Path: leg0, SampleRate: 8000},
				{Path: leg1, SampleRate: 8000},
			}
			err := CombineWAVRecordingsAligned(output, legs)
			if err != nil {
				t.Errorf("Combine %d failed: %v", id, err)
			}
		}(i)
	}

	wg.Wait()
}
