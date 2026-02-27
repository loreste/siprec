package media

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"
)

func TestPausableWriter(t *testing.T) {
	t.Run("writes when not paused", func(t *testing.T) {
		buf := &bytes.Buffer{}
		pw := NewPausableWriter(buf)
		
		data := []byte("test data")
		n, err := pw.Write(data)
		
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != len(data) {
			t.Fatalf("expected %d bytes written, got %d", len(data), n)
		}
		if buf.String() != "test data" {
			t.Fatalf("expected 'test data', got '%s'", buf.String())
		}
	})

	t.Run("drops data when paused", func(t *testing.T) {
		buf := &bytes.Buffer{}
		pw := NewPausableWriter(buf)
		
		// Pause the writer
		pw.Pause()
		
		data := []byte("test data")
		n, err := pw.Write(data)
		
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != len(data) {
			t.Fatalf("expected %d bytes 'written' (dropped), got %d", len(data), n)
		}
		if buf.Len() != 0 {
			t.Fatalf("expected empty buffer when paused, got '%s'", buf.String())
		}
	})

	t.Run("resumes writing after pause", func(t *testing.T) {
		buf := &bytes.Buffer{}
		pw := NewPausableWriter(buf)

		// Write initial data
		_, _ = pw.Write([]byte("before pause "))

		// Pause and try to write
		pw.Pause()
		_, _ = pw.Write([]byte("during pause "))

		// Resume and write again
		pw.Resume()
		_, _ = pw.Write([]byte("after resume"))
		
		expected := "before pause after resume"
		if buf.String() != expected {
			t.Fatalf("expected '%s', got '%s'", expected, buf.String())
		}
	})

	t.Run("concurrent pause/resume safety", func(t *testing.T) {
		buf := &bytes.Buffer{}
		pw := NewPausableWriter(buf)
		
		var wg sync.WaitGroup
		
		// Writer goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				pw.Write([]byte("x"))
				time.Sleep(time.Microsecond)
			}
		}()
		
		// Pause/resume goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				pw.Pause()
				time.Sleep(time.Microsecond)
				pw.Resume()
				time.Sleep(time.Microsecond)
			}
		}()
		
		wg.Wait()
		
		// Just check that we didn't panic
		if !pw.IsPaused() && buf.Len() == 0 {
			t.Fatal("expected some data to be written")
		}
	})
}

func TestPausableReader(t *testing.T) {
	t.Run("reads when not paused", func(t *testing.T) {
		source := bytes.NewReader([]byte("test data"))
		pr := NewPausableReader(source)
		
		buf := make([]byte, 9)
		n, err := pr.Read(buf)
		
		if err != nil && err != io.EOF {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != 9 {
			t.Fatalf("expected 9 bytes read, got %d", n)
		}
		if string(buf) != "test data" {
			t.Fatalf("expected 'test data', got '%s'", string(buf))
		}
	})

	t.Run("blocks when paused", func(t *testing.T) {
		source := bytes.NewReader([]byte("test data"))
		pr := NewPausableReader(source)
		
		// Pause the reader
		pr.Pause()
		
		// Try to read in a goroutine
		done := make(chan bool)
		go func() {
			buf := make([]byte, 9)
			_, _ = pr.Read(buf)
			done <- true
		}()
		
		// Should timeout waiting for read
		select {
		case <-done:
			t.Fatal("read should have blocked when paused")
		case <-time.After(100 * time.Millisecond):
			// Expected - read is blocked
		}
		
		// Resume and check that read completes
		pr.Resume()
		select {
		case <-done:
			// Expected - read completed
		case <-time.After(100 * time.Millisecond):
			t.Fatal("read should have completed after resume")
		}
	})

	t.Run("concurrent operations", func(t *testing.T) {
		source := bytes.NewReader(make([]byte, 1000))
		pr := NewPausableReader(source)
		
		var wg sync.WaitGroup
		
		// Reader goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 10)
			for i := 0; i < 50; i++ {
				_, _ = pr.Read(buf)
			}
		}()
		
		// Pause/resume goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				pr.Pause()
				time.Sleep(time.Microsecond * 10)
				pr.Resume()
				time.Sleep(time.Microsecond * 10)
			}
		}()
		
		// Use a timeout to ensure test doesn't hang
		done := make(chan bool)
		go func() {
			wg.Wait()
			done <- true
		}()
		
		select {
		case <-done:
			// Test completed successfully
		case <-time.After(5 * time.Second):
			t.Fatal("test timed out - possible deadlock")
		}
	})
}