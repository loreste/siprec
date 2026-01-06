package audio

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

// AudioFormat represents supported audio encoding formats
type AudioFormat string

const (
	FormatWAV  AudioFormat = "wav"
	FormatMP3  AudioFormat = "mp3"
	FormatOpus AudioFormat = "opus"
	FormatOGG  AudioFormat = "ogg"
	FormatMP4  AudioFormat = "mp4"  // AAC in MP4 container
	FormatM4A  AudioFormat = "m4a"  // AAC in M4A container
	FormatFLAC AudioFormat = "flac" // Lossless compression
)

// EncoderConfig holds encoding configuration
type EncoderConfig struct {
	Format      AudioFormat
	SampleRate  int
	Channels    int
	BitRate     int  // kbps for lossy formats
	Quality     int  // 1-10 for VBR encoding
	UseFFmpeg   bool // Use FFmpeg for encoding (required for MP3, Opus, MP4)
	FFmpegPath  string
	TempDir     string
}

// DefaultEncoderConfig returns default encoding configuration
func DefaultEncoderConfig() *EncoderConfig {
	return &EncoderConfig{
		Format:     FormatWAV,
		SampleRate: 8000,
		Channels:   1,
		BitRate:    128,
		Quality:    5,
		UseFFmpeg:  true,
		FFmpegPath: "ffmpeg",
		TempDir:    os.TempDir(),
	}
}

// AudioEncoder handles audio format encoding
type AudioEncoder struct {
	config *EncoderConfig
	logger *logrus.Logger
	mu     sync.Mutex
}

// NewAudioEncoder creates a new audio encoder
func NewAudioEncoder(config *EncoderConfig, logger *logrus.Logger) *AudioEncoder {
	if config == nil {
		config = DefaultEncoderConfig()
	}
	return &AudioEncoder{
		config: config,
		logger: logger,
	}
}

// GetFileExtension returns the file extension for the configured format
func (e *AudioEncoder) GetFileExtension() string {
	return "." + string(e.config.Format)
}

// EncodeFile converts a WAV file to the target format
func (e *AudioEncoder) EncodeFile(inputPath string, outputPath string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// If target format is WAV, just copy
	if e.config.Format == FormatWAV {
		return copyFile(inputPath, outputPath)
	}

	// Check if FFmpeg is available
	if !e.isFFmpegAvailable() {
		e.logger.Warn("FFmpeg not available, falling back to WAV format")
		return copyFile(inputPath, outputPath)
	}

	return e.encodeWithFFmpeg(inputPath, outputPath)
}

// EncodeStream creates a streaming encoder that writes to the output
func (e *AudioEncoder) EncodeStream(output io.WriteCloser) (io.WriteCloser, error) {
	// For WAV format, return a passthrough writer
	if e.config.Format == FormatWAV {
		return output, nil
	}

	// For other formats, we need FFmpeg piped encoding
	if !e.isFFmpegAvailable() {
		e.logger.Warn("FFmpeg not available for streaming encoding, using WAV")
		return output, nil
	}

	return e.createStreamEncoder(output)
}

// isFFmpegAvailable checks if FFmpeg is available
func (e *AudioEncoder) isFFmpegAvailable() bool {
	cmd := exec.Command(e.config.FFmpegPath, "-version")
	return cmd.Run() == nil
}

// encodeWithFFmpeg uses FFmpeg to encode audio
func (e *AudioEncoder) encodeWithFFmpeg(inputPath, outputPath string) error {
	args := []string{
		"-i", inputPath,
		"-y", // Overwrite output
	}

	// Add format-specific encoding options
	switch e.config.Format {
	case FormatMP3:
		args = append(args,
			"-codec:a", "libmp3lame",
			"-b:a", fmt.Sprintf("%dk", e.config.BitRate),
			"-q:a", fmt.Sprintf("%d", 10-e.config.Quality), // LAME uses inverse quality scale
		)
	case FormatOpus, FormatOGG:
		args = append(args,
			"-codec:a", "libopus",
			"-b:a", fmt.Sprintf("%dk", e.config.BitRate),
			"-vbr", "on",
			"-compression_level", fmt.Sprintf("%d", e.config.Quality),
		)
	case FormatMP4, FormatM4A:
		args = append(args,
			"-codec:a", "aac",
			"-b:a", fmt.Sprintf("%dk", e.config.BitRate),
		)
	case FormatFLAC:
		args = append(args,
			"-codec:a", "flac",
			"-compression_level", fmt.Sprintf("%d", e.config.Quality),
		)
	default:
		return fmt.Errorf("unsupported format: %s", e.config.Format)
	}

	// Add sample rate and channels if specified
	if e.config.SampleRate > 0 {
		args = append(args, "-ar", fmt.Sprintf("%d", e.config.SampleRate))
	}
	if e.config.Channels > 0 {
		args = append(args, "-ac", fmt.Sprintf("%d", e.config.Channels))
	}

	args = append(args, outputPath)

	cmd := exec.Command(e.config.FFmpegPath, args...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		e.logger.WithFields(logrus.Fields{
			"error":  err,
			"output": string(output),
			"input":  inputPath,
			"format": e.config.Format,
		}).Error("FFmpeg encoding failed")
		return fmt.Errorf("FFmpeg encoding failed: %w", err)
	}

	e.logger.WithFields(logrus.Fields{
		"input":  inputPath,
		"output": outputPath,
		"format": e.config.Format,
	}).Debug("Audio encoding completed")

	return nil
}

// createStreamEncoder creates a piped FFmpeg encoder for streaming
func (e *AudioEncoder) createStreamEncoder(output io.WriteCloser) (io.WriteCloser, error) {
	args := []string{
		"-f", "wav",  // Input format
		"-i", "pipe:0", // Read from stdin
		"-y",
	}

	// Add format-specific encoding options
	switch e.config.Format {
	case FormatMP3:
		args = append(args,
			"-codec:a", "libmp3lame",
			"-b:a", fmt.Sprintf("%dk", e.config.BitRate),
			"-f", "mp3",
		)
	case FormatOpus, FormatOGG:
		args = append(args,
			"-codec:a", "libopus",
			"-b:a", fmt.Sprintf("%dk", e.config.BitRate),
			"-f", "opus",
		)
	case FormatMP4, FormatM4A:
		args = append(args,
			"-codec:a", "aac",
			"-b:a", fmt.Sprintf("%dk", e.config.BitRate),
			"-f", "mp4",
			"-movflags", "frag_keyframe+empty_moov", // Enable streaming MP4
		)
	default:
		return nil, fmt.Errorf("streaming not supported for format: %s", e.config.Format)
	}

	args = append(args, "pipe:1") // Write to stdout

	cmd := exec.Command(e.config.FFmpegPath, args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	cmd.Stdout = output

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start FFmpeg: %w", err)
	}

	return &ffmpegStreamEncoder{
		cmd:    cmd,
		stdin:  stdin,
		output: output,
		logger: e.logger,
	}, nil
}

// ConvertToFormat converts a file to the specified format
func (e *AudioEncoder) ConvertToFormat(inputPath string, targetFormat AudioFormat) (string, error) {
	// Determine output path
	ext := filepath.Ext(inputPath)
	outputPath := strings.TrimSuffix(inputPath, ext) + "." + string(targetFormat)

	// Create temp encoder with target format
	tempConfig := *e.config
	tempConfig.Format = targetFormat
	tempEncoder := &AudioEncoder{
		config: &tempConfig,
		logger: e.logger,
	}

	if err := tempEncoder.EncodeFile(inputPath, outputPath); err != nil {
		return "", err
	}

	return outputPath, nil
}

// GetSupportedFormats returns list of supported formats
func GetSupportedFormats() []AudioFormat {
	return []AudioFormat{
		FormatWAV,
		FormatMP3,
		FormatOpus,
		FormatOGG,
		FormatMP4,
		FormatM4A,
		FormatFLAC,
	}
}

// IsFormatSupported checks if a format string is supported
func IsFormatSupported(format string) bool {
	f := AudioFormat(strings.ToLower(format))
	for _, supported := range GetSupportedFormats() {
		if f == supported {
			return true
		}
	}
	return false
}

// ParseFormat parses a format string into AudioFormat
func ParseFormat(format string) AudioFormat {
	f := AudioFormat(strings.ToLower(format))
	if IsFormatSupported(string(f)) {
		return f
	}
	return FormatWAV // Default to WAV
}

// ffmpegStreamEncoder wraps FFmpeg for streaming encoding
type ffmpegStreamEncoder struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	output io.WriteCloser
	logger *logrus.Logger
	closed bool
	mu     sync.Mutex
}

func (f *ffmpegStreamEncoder) Write(p []byte) (n int, err error) {
	return f.stdin.Write(p)
}

func (f *ffmpegStreamEncoder) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return nil
	}
	f.closed = true

	// Close stdin to signal EOF to FFmpeg
	if err := f.stdin.Close(); err != nil {
		f.logger.WithError(err).Warn("Error closing FFmpeg stdin")
	}

	// Wait for FFmpeg to finish
	if err := f.cmd.Wait(); err != nil {
		f.logger.WithError(err).Warn("FFmpeg process exited with error")
	}

	// Close output
	return f.output.Close()
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	if src == dst {
		return nil
	}

	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// BatchEncoder handles batch encoding of multiple files
type BatchEncoder struct {
	encoder    *AudioEncoder
	logger     *logrus.Logger
	workers    int
	inputDir   string
	outputDir  string
	deleteOriginal bool
}

// NewBatchEncoder creates a new batch encoder
func NewBatchEncoder(encoder *AudioEncoder, workers int, logger *logrus.Logger) *BatchEncoder {
	if workers <= 0 {
		workers = 4
	}
	return &BatchEncoder{
		encoder: encoder,
		logger:  logger,
		workers: workers,
	}
}

// EncodeDirectory encodes all WAV files in a directory
func (b *BatchEncoder) EncodeDirectory(inputDir, outputDir string, deleteOriginal bool) error {
	b.inputDir = inputDir
	b.outputDir = outputDir
	b.deleteOriginal = deleteOriginal

	// Create output directory if needed
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Find all WAV files
	files, err := filepath.Glob(filepath.Join(inputDir, "*.wav"))
	if err != nil {
		return err
	}

	// Process files with worker pool
	jobs := make(chan string, len(files))
	results := make(chan error, len(files))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < b.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for inputFile := range jobs {
				baseName := filepath.Base(inputFile)
				ext := filepath.Ext(baseName)
				outputFile := filepath.Join(outputDir, strings.TrimSuffix(baseName, ext)+b.encoder.GetFileExtension())

				if err := b.encoder.EncodeFile(inputFile, outputFile); err != nil {
					results <- err
					continue
				}

				if deleteOriginal {
					os.Remove(inputFile)
				}

				results <- nil
			}
		}()
	}

	// Send jobs
	go func() {
		for _, file := range files {
			jobs <- file
		}
		close(jobs)
	}()

	// Wait for completion
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	var errors []error
	for err := range results {
		if err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("batch encoding completed with %d errors", len(errors))
	}

	b.logger.WithFields(logrus.Fields{
		"input_dir":  inputDir,
		"output_dir": outputDir,
		"files":      len(files),
		"format":     b.encoder.config.Format,
	}).Info("Batch encoding completed")

	return nil
}
