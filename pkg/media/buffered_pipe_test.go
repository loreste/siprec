package media

import (
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBufferedPipe_BasicReadWrite(t *testing.T) {
	reader, writer := NewBufferedPipe(1024)
	defer reader.Close()
	defer writer.Close()

	// Write some data
	data := []byte("hello world")
	n, err := writer.Write(data)
	require.NoError(t, err)
	assert.Equal(t, len(data), n)

	// Read it back
	buf := make([]byte, 100)
	n, err = reader.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, len(data), n)
	assert.Equal(t, data, buf[:n])
}

func TestBufferedPipe_ConcurrentReadWrite(t *testing.T) {
	reader, writer := NewBufferedPipe(4096)

	var wg sync.WaitGroup
	totalBytes := 10000
	chunkSize := 100

	// Writer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer writer.Close()

		written := 0
		chunk := make([]byte, chunkSize)
		for i := range chunk {
			chunk[i] = byte(i % 256)
		}

		for written < totalBytes {
			n, err := writer.Write(chunk)
			if err != nil {
				return
			}
			written += n
		}
	}()

	// Reader goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()

		read := 0
		buf := make([]byte, chunkSize*2)

		for read < totalBytes {
			n, err := reader.Read(buf)
			if err == io.EOF {
				break
			}
			if err != nil {
				return
			}
			read += n
		}
	}()

	wg.Wait()
}

func TestBufferedPipe_NonBlocking(t *testing.T) {
	// Small buffer to force overflow
	reader, writer := NewBufferedPipe(100)
	defer reader.Close()

	// Write more than buffer can hold - should not block
	done := make(chan bool)
	go func() {
		data := make([]byte, 500)
		for i := range data {
			data[i] = byte(i)
		}
		_, err := writer.Write(data)
		assert.NoError(t, err)
		done <- true
	}()

	select {
	case <-done:
		// Write completed without blocking
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Write blocked when buffer was full")
	}
}

func TestBufferedPipe_CloseWriter(t *testing.T) {
	reader, writer := NewBufferedPipe(1024)

	// Write some data
	data := []byte("test data")
	_, err := writer.Write(data)
	require.NoError(t, err)

	// Close writer
	writer.Close()

	// Read remaining data
	buf := make([]byte, 100)
	n, err := reader.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, len(data), n)

	// Next read should return EOF
	_, err = reader.Read(buf)
	assert.Equal(t, io.EOF, err)
}

func TestBufferedPipe_CloseReader(t *testing.T) {
	reader, writer := NewBufferedPipe(1024)

	// Close reader
	reader.Close()

	// Write should fail
	data := []byte("test")
	_, err := writer.Write(data)
	assert.Equal(t, io.ErrClosedPipe, err)
}

func TestBufferedPipe_Wraparound(t *testing.T) {
	// Small buffer to test wraparound
	reader, writer := NewBufferedPipe(16)
	defer reader.Close()
	defer writer.Close()

	// Write and read multiple times to exercise wraparound
	for i := 0; i < 10; i++ {
		data := []byte{byte(i), byte(i + 1), byte(i + 2)}
		_, err := writer.Write(data)
		require.NoError(t, err)

		buf := make([]byte, 10)
		n, err := reader.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, data, buf[:n])
	}
}
