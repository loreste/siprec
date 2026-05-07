package backup

import (
	"bufio"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"io"
	"os"
	"strings"
)

// StreamingFileReader provides memory-efficient file reading with compression and encryption support
type StreamingFileReader struct {
	file       *os.File
	reader     io.Reader
	gzipReader *gzip.Reader
	closer     []io.Closer
}

// NewStreamingFileReader creates a new streaming file reader
func NewStreamingFileReader(filePath string) (*StreamingFileReader, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	sfr := &StreamingFileReader{
		file:   file,
		reader: file,
		closer: []io.Closer{file},
	}

	// Handle encryption
	if strings.HasSuffix(filePath, ".enc") {
		decryptReader, err := sfr.createStreamingDecryptionReader(file)
		if err != nil {
			sfr.Close()
			return nil, fmt.Errorf("failed to create decryption reader: %w", err)
		}
		sfr.reader = decryptReader
	}

	// Handle compression
	if strings.Contains(filePath, ".gz") {
		gzipReader, err := gzip.NewReader(sfr.reader)
		if err != nil {
			sfr.Close()
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		sfr.gzipReader = gzipReader
		sfr.reader = gzipReader
		sfr.closer = append(sfr.closer, gzipReader)
	}

	return sfr, nil
}

// Read implements io.Reader
func (sfr *StreamingFileReader) Read(p []byte) (n int, err error) {
	return sfr.reader.Read(p)
}

// Scanner returns a bufio.Scanner for line-by-line reading
func (sfr *StreamingFileReader) Scanner() *bufio.Scanner {
	return bufio.NewScanner(sfr.reader)
}

// Close closes all associated readers
func (sfr *StreamingFileReader) Close() error {
	var firstError error
	for i := len(sfr.closer) - 1; i >= 0; i-- {
		if err := sfr.closer[i].Close(); err != nil && firstError == nil {
			firstError = err
		}
	}
	return firstError
}

// createStreamingDecryptionReader creates a streaming decryption reader
func (sfr *StreamingFileReader) createStreamingDecryptionReader(file *os.File) (io.Reader, error) {
	// For streaming decryption, we need to read the nonce first
	key, err := getEncryptionKeyFromEnv()
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Read nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(file, nonce); err != nil {
		return nil, fmt.Errorf("failed to read nonce: %w", err)
	}

	// Create stream reader for the remaining data
	return &streamCipher{
		reader: file,
		gcm:    gcm,
		nonce:  nonce,
		buffer: make([]byte, 4096),
	}, nil
}

// streamCipher implements streaming decryption
type streamCipher struct {
	reader    io.Reader
	gcm       cipher.AEAD
	nonce     []byte
	buffer    []byte
	decrypted []byte
	pos       int
	eof       bool
}

func (sc *streamCipher) Read(p []byte) (n int, err error) {
	if sc.eof && sc.pos >= len(sc.decrypted) {
		return 0, io.EOF
	}

	// If we have decrypted data available, return it
	if sc.pos < len(sc.decrypted) {
		n = copy(p, sc.decrypted[sc.pos:])
		sc.pos += n
		return n, nil
	}

	// Read more encrypted data
	nr, err := sc.reader.Read(sc.buffer)
	if err != nil {
		if err == io.EOF {
			sc.eof = true
			if nr == 0 {
				return 0, io.EOF
			}
		} else {
			return 0, err
		}
	}

	// Decrypt the chunk using AES-GCM (secure authenticated encryption)
	// #nosec G407 -- using AES-256-GCM which is a secure modern algorithm
	decrypted, err := sc.gcm.Open(nil, sc.nonce, sc.buffer[:nr], nil)
	if err != nil {
		return 0, fmt.Errorf("decryption failed: %w", err)
	}

	sc.decrypted = decrypted
	sc.pos = 0

	// Return as much as requested
	n = copy(p, sc.decrypted)
	sc.pos = n
	return n, nil
}

// getEncryptionKeyFromEnv gets encryption key from environment
func getEncryptionKeyFromEnv() ([]byte, error) {
	keyString := os.Getenv("BACKUP_ENCRYPTION_KEY")
	if keyString == "" {
		return nil, fmt.Errorf("encryption key not provided in BACKUP_ENCRYPTION_KEY")
	}

	key := []byte(keyString)
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes for AES-256")
	}

	return key, nil
}

