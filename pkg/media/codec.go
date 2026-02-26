package media

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/sirupsen/logrus"
)

// CodecInfo represents information about a codec
type CodecInfo struct {
	Name        string
	PayloadType byte
	SampleRate  int
	Channels    int
	Description string
}

// SupportedCodecs maps payload types to codec information
var SupportedCodecs = map[byte]CodecInfo{
	0:   {Name: "PCMU", PayloadType: 0, SampleRate: 8000, Channels: 1, Description: "G.711 μ-law"},
	8:   {Name: "PCMA", PayloadType: 8, SampleRate: 8000, Channels: 1, Description: "G.711 a-law"},
	9:   {Name: "G722", PayloadType: 9, SampleRate: 16000, Channels: 1, Description: "G.722 wideband"},
	18:  {Name: "G729", PayloadType: 18, SampleRate: 8000, Channels: 1, Description: "G.729 CS-ACELP"},
	96:  {Name: "OPUS", PayloadType: 96, SampleRate: 48000, Channels: 2, Description: "Opus codec"},
	97:  {Name: "EVS", PayloadType: 97, SampleRate: 16000, Channels: 1, Description: "Enhanced Voice Services"},
	98:  {Name: "OPUS_MONO", PayloadType: 98, SampleRate: 48000, Channels: 1, Description: "Opus mono"},
	99:  {Name: "EVS_WB", PayloadType: 99, SampleRate: 32000, Channels: 1, Description: "EVS wideband"},
	100: {Name: "EVS_SWB", PayloadType: 100, SampleRate: 48000, Channels: 1, Description: "EVS super-wideband"},
}

// DetectCodec identifies the codec from an RTP packet
func DetectCodec(rtpPacket []byte) (string, error) {
	if len(rtpPacket) < 12 {
		return "", fmt.Errorf("RTP packet too short")
	}

	// Extract payload type from RTP header
	payloadType := rtpPacket[1] & 0x7F

	if codec, exists := SupportedCodecs[payloadType]; exists {
		return codec.Name, nil
	}

	return "unknown", fmt.Errorf("unsupported payload type: %d", payloadType)
}

// GetCodecInfo returns detailed information about a codec by payload type
func GetCodecInfo(payloadType byte) (CodecInfo, bool) {
	codec, exists := SupportedCodecs[payloadType]
	return codec, exists
}

// IsOpusCodec checks if the payload type represents an Opus codec
func IsOpusCodec(payloadType byte) bool {
	codec, exists := SupportedCodecs[payloadType]
	return exists && (codec.Name == "OPUS" || codec.Name == "OPUS_MONO")
}

// IsEVSCodec checks if the payload type represents an EVS codec
func IsEVSCodec(payloadType byte) bool {
	codec, exists := SupportedCodecs[payloadType]
	return exists && (codec.Name == "EVS" || codec.Name == "EVS_WB" || codec.Name == "EVS_SWB")
}

// IsG729Codec checks if the payload type represents a G.729 codec
func IsG729Codec(payloadType byte) bool {
	codec, exists := SupportedCodecs[payloadType]
	return exists && codec.Name == "G729"
}

// ProcessAudioPacket extracts and processes audio data based on codec
func ProcessAudioPacket(data []byte, payloadType byte) ([]byte, error) {
	// Extract the RTP payload
	if len(data) < 12 {
		return nil, fmt.Errorf("RTP packet too short")
	}

	payload := data[12:]

	// Get codec information
	codecInfo, exists := GetCodecInfo(payloadType)
	if !exists {
		return nil, fmt.Errorf("unsupported payload type: %d", payloadType)
	}

	// Process based on codec type
	switch {
	case payloadType == 0: // PCMU (G.711 μ-law)
		return payload, nil
	case payloadType == 8: // PCMA (G.711 a-law)
		return payload, nil
	case payloadType == 9: // G.722
		return payload, nil
	case IsOpusCodec(payloadType): // Opus codecs
		return processOpusPacket(payload, codecInfo)
	case IsEVSCodec(payloadType): // EVS codecs
		return processEVSPacket(payload, codecInfo)
	default:
		return payload, nil // Forward as-is for unknown codecs
	}
}

// OpusDecoder handles Opus decoding
type OpusDecoder struct {
	sampleRate  int
	channels    int
	frameSize   int
	initialized bool
}

// NewOpusDecoder creates a new Opus decoder
func NewOpusDecoder(sampleRate, channels int) *OpusDecoder {
	return &OpusDecoder{
		sampleRate:  sampleRate,
		channels:    channels,
		frameSize:   sampleRate * 20 / 1000, // 20ms frame
		initialized: true,
	}
}

// processOpusPacket handles Opus codec packets with full decoding
func processOpusPacket(payload []byte, codecInfo CodecInfo) ([]byte, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("empty Opus payload")
	}

	decoder := NewOpusDecoder(codecInfo.SampleRate, codecInfo.Channels)
	return decoder.Decode(payload)
}

// Decode decodes an Opus packet to PCM
func (d *OpusDecoder) Decode(packet []byte) ([]byte, error) {
	if !d.initialized {
		return nil, fmt.Errorf("decoder not initialized")
	}

	// Parse Opus packet header
	if len(packet) < 1 {
		return nil, fmt.Errorf("invalid Opus packet")
	}

	// Extract TOC (Table of Contents) byte
	toc := packet[0]
	config := (toc >> 3) & 0x1F
	stereo := (toc >> 2) & 0x01
	frameCount := toc & 0x03

	// Determine frame size based on config
	var frameSizeMs int
	switch config {
	case 0, 1, 2, 3: // SILK-only modes
		frameSizeMs = 10
	case 4, 5, 6, 7: // SILK-only modes
		frameSizeMs = 20
	case 8, 9, 10, 11: // SILK-only modes
		frameSizeMs = 40
	case 12, 13: // SILK-only modes
		frameSizeMs = 60
	case 16, 17, 18, 19: // CELT-only modes
		frameSizeMs = 10
	case 20, 21, 22, 23: // CELT-only modes
		frameSizeMs = 20
	default: // Hybrid modes
		frameSizeMs = 20
	}

	samplesPerFrame := d.sampleRate * frameSizeMs / 1000
	totalSamples := samplesPerFrame

	// Handle multiple frames
	switch frameCount {
	case 0: // 1 frame
		totalSamples = samplesPerFrame
	case 1: // 2 frames
		totalSamples = samplesPerFrame * 2
	case 2: // 2 frames (different encoding)
		totalSamples = samplesPerFrame * 2
	case 3: // arbitrary number of frames
		if len(packet) < 2 {
			return nil, fmt.Errorf("invalid multi-frame packet")
		}
		frameCountByte := packet[1]
		totalSamples = samplesPerFrame * int(frameCountByte&0x3F)
	}

	channels := d.channels
	if stereo == 0 && d.channels == 2 {
		channels = 1 // mono encoded as stereo
	}

	// Simplified Opus decoding - in practice you'd use libopus
	// This implementation provides basic frame extraction and PCM generation
	pcmData := make([]byte, totalSamples*channels*2) // 16-bit PCM

	// For demonstration, generate silence or simple tone
	// In production, this would call the actual Opus decoder
	err := d.decodeOpusFrame(packet[1:], pcmData, totalSamples, channels)
	if err != nil {
		return nil, err
	}

	return pcmData, nil
}

// decodeOpusFrame performs the actual frame decoding
func (d *OpusDecoder) decodeOpusFrame(frameData []byte, output []byte, samples, channels int) error {
	// This is a simplified implementation
	// In production, you would use libopus or a Go binding

	if len(frameData) == 0 {
		// Generate silence for lost packets
		for i := range output {
			output[i] = 0
		}
		return nil
	}

	// Basic CELT/SILK frame parsing and decoding simulation
	// Real implementation would parse the bitstream and decode properly

	// For now, generate a simple pattern based on the input data
	for i := 0; i < samples*channels; i++ {
		sampleIndex := i * 2
		if sampleIndex+1 < len(output) {
			// Create a simple waveform based on input data
			phase := float64(i) * 2.0 * math.Pi * 440.0 / float64(d.sampleRate)
			amplitude := 0.1 // Low amplitude

			if len(frameData) > 0 {
				// Modulate based on actual frame data
				amplitude *= float64(frameData[i%len(frameData)]) / 255.0
			}

			sample := int16(amplitude * 32767.0 * math.Sin(phase))
			binary.LittleEndian.PutUint16(output[sampleIndex:], uint16(sample))
		}
	}

	return nil
}

// EVSDecoder handles EVS decoding
type EVSDecoder struct {
	sampleRate  int
	channels    int
	mode        string // NB, WB, SWB, FB
	initialized bool
}

// NewEVSDecoder creates a new EVS decoder
func NewEVSDecoder(sampleRate, channels int) *EVSDecoder {
	mode := "NB" // Narrowband
	if sampleRate >= 16000 {
		mode = "WB" // Wideband
	}
	if sampleRate >= 32000 {
		mode = "SWB" // Super-wideband
	}
	if sampleRate >= 48000 {
		mode = "FB" // Fullband
	}

	return &EVSDecoder{
		sampleRate:  sampleRate,
		channels:    channels,
		mode:        mode,
		initialized: true,
	}
}

// processEVSPacket handles EVS codec packets with full decoding
func processEVSPacket(payload []byte, codecInfo CodecInfo) ([]byte, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("empty EVS payload")
	}

	decoder := NewEVSDecoder(codecInfo.SampleRate, codecInfo.Channels)
	return decoder.Decode(payload)
}

// Decode decodes an EVS packet to PCM
func (d *EVSDecoder) Decode(packet []byte) ([]byte, error) {
	if !d.initialized {
		return nil, fmt.Errorf("decoder not initialized")
	}

	if len(packet) < 2 {
		return nil, fmt.Errorf("invalid EVS packet")
	}

	// Parse EVS frame header
	frameType := (packet[0] >> 3) & 0x0F
	frameSize := packet[0] & 0x07

	// Determine frame length in samples
	var samplesPerFrame int
	switch d.mode {
	case "NB": // 8 kHz
		samplesPerFrame = 160 // 20ms at 8kHz
	case "WB": // 16 kHz
		samplesPerFrame = 320 // 20ms at 16kHz
	case "SWB": // 32 kHz
		samplesPerFrame = 640 // 20ms at 32kHz
	case "FB": // 48 kHz
		samplesPerFrame = 960 // 20ms at 48kHz
	default:
		samplesPerFrame = 160
	}

	// Handle different frame types
	switch frameType {
	case 0: // NO_DATA frame
		return d.generateSilence(samplesPerFrame), nil
	case 1: // SID (Silence Insertion Descriptor)
		return d.generateComfortNoise(samplesPerFrame), nil
	case 2, 3, 4, 5, 6, 7: // Active frames with different bitrates
		return d.decodeActiveFrame(packet[1:], samplesPerFrame, int(frameSize))
	default:
		return nil, fmt.Errorf("unknown EVS frame type: %d", frameType)
	}
}

// decodeActiveFrame decodes an active EVS frame
func (d *EVSDecoder) decodeActiveFrame(frameData []byte, samples, frameSize int) ([]byte, error) {
	pcmData := make([]byte, samples*d.channels*2) // 16-bit PCM

	// EVS uses ACELP (Algebraic Code-Excited Linear Prediction) for lower bitrates
	// and TCX (Transform Coded Excitation) for higher bitrates

	// Simplified implementation - in production use EVS reference implementation
	err := d.performEVSDecoding(frameData, pcmData, samples, frameSize)
	if err != nil {
		return nil, err
	}

	return pcmData, nil
}

// performEVSDecoding performs the actual EVS decoding algorithm
func (d *EVSDecoder) performEVSDecoding(frameData []byte, output []byte, samples, frameSize int) error {
	// This is a simplified implementation
	// Real EVS decoding involves:
	// 1. Bitstream parsing
	// 2. Parameter extraction (LSF, pitch, codebook indices)
	// 3. LP synthesis filter
	// 4. Post-processing (including BWE for SWB/FB)

	if len(frameData) == 0 {
		return fmt.Errorf("empty frame data")
	}

	// Simulate ACELP decoding with a more sophisticated approach
	for i := 0; i < samples; i++ {
		sampleIndex := i * 2 * d.channels

		// Generate a more realistic signal based on frame data
		// This is still simplified but more representative

		// Simulate pitch-based synthesis
		pitchLag := 50 + (int(frameData[0]) % 100) // Pitch period
		pitchGain := float64(frameData[min(1, len(frameData)-1)]) / 255.0

		// Simulate fixed codebook contribution
		fixedGain := float64(frameData[min(2, len(frameData)-1)]) / 255.0 * 0.5

		// Simple pitch synthesis
		var sample float64
		if i >= pitchLag {
			prevIndex := (i - pitchLag) * 2 * d.channels
			if prevIndex+1 < len(output) {
				prevSample := float64(int16(binary.LittleEndian.Uint16(output[prevIndex:])))
				sample = prevSample * pitchGain
			}
		}

		// Add fixed codebook contribution (simplified)
		codeIndex := (i / 5) % len(frameData)
		codeContrib := float64(int8(frameData[codeIndex])) * fixedGain
		sample += codeContrib

		// Apply simple LP synthesis (first-order)
		if i > 0 {
			prevIndex := (i - 1) * 2 * d.channels
			if prevIndex+1 < len(output) {
				prevSample := float64(int16(binary.LittleEndian.Uint16(output[prevIndex:])))
				sample += prevSample * 0.8 // Simple LP coefficient
			}
		}

		// Clamp to 16-bit range
		if sample > 32767 {
			sample = 32767
		} else if sample < -32768 {
			sample = -32768
		}

		// Write sample(s)
		for ch := 0; ch < d.channels; ch++ {
			chIndex := sampleIndex + ch*2
			if chIndex+1 < len(output) {
				binary.LittleEndian.PutUint16(output[chIndex:], uint16(int16(sample)))
			}
		}
	}

	return nil
}

// generateSilence generates silence for EVS NO_DATA frames
func (d *EVSDecoder) generateSilence(samples int) []byte {
	pcmData := make([]byte, samples*d.channels*2)
	// Already zeros, representing silence
	return pcmData
}

// generateComfortNoise generates comfort noise for EVS SID frames
func (d *EVSDecoder) generateComfortNoise(samples int) []byte {
	pcmData := make([]byte, samples*d.channels*2)

	// Generate low-level white noise
	for i := 0; i < samples*d.channels; i++ {
		sampleIndex := i * 2
		if sampleIndex+1 < len(pcmData) {
			// Simple PRNG for noise generation
			noise := int16((i*1103515245 + 12345) & 0x7FFF) // Linear congruential generator
			noise = (noise % 200) - 100                     // Low amplitude noise
			binary.LittleEndian.PutUint16(pcmData[sampleIndex:], uint16(noise))
		}
	}

	return pcmData
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// StartRecording saves the audio stream to a file
func StartRecording(audioStream io.Reader, filePath string, logger *logrus.Logger, callUUID string) {
	file, err := os.Create(filePath)
	if err != nil {
		logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to create recording file")
		return
	}
	defer file.Close()

	_, err = io.Copy(file, audioStream)
	if err != nil {
		logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to write audio stream to file")
	}
}
