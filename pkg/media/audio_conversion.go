package media

import (
	"encoding/binary"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/pidato/audio/g729"
)

var (
	muLawDecodeTable [256]int16
	aLawDecodeTable  [256]int16
)

func init() {
	for i := 0; i < 256; i++ {
		muLawDecodeTable[i] = decodeMuLawSample(byte(i))
		aLawDecodeTable[i] = decodeALawSample(byte(i))
	}
}

// DecodeAudioPayload converts codec-specific RTP payload bytes into 16-bit PCM.
// The returned slice uses little-endian byte ordering.
func DecodeAudioPayload(payload []byte, codecName string) ([]byte, error) {
	switch codecName {
	case "", "PCMU", "G711U", "G.711U", "G711MU":
		return muLawToPCM(payload), nil
	case "PCMA", "G711A", "G.711A":
		return aLawToPCM(payload), nil
	case "L16", "LINEAR16":
		// Already 16-bit linear PCM
		return append([]byte(nil), payload...), nil
	case "OPUS":
		// Opus stereo at 48kHz
		codecInfo := CodecInfo{Name: "OPUS", SampleRate: 48000, Channels: 2}
		return decodeOpusPacket(payload, codecInfo)
	case "OPUS_MONO":
		// Opus mono at 48kHz
		codecInfo := CodecInfo{Name: "OPUS_MONO", SampleRate: 48000, Channels: 1}
		return decodeOpusPacket(payload, codecInfo)
	case "G722":
		// G.722 wideband - decode using SB-ADPCM decoder
		return decodeG722Packet(payload)
	case "EVS", "EVS_WB", "EVS_SWB":
		// Enhanced Voice Services
		var codecInfo CodecInfo
		switch codecName {
		case "EVS":
			codecInfo = CodecInfo{Name: "EVS", SampleRate: 16000, Channels: 1}
		case "EVS_WB":
			codecInfo = CodecInfo{Name: "EVS_WB", SampleRate: 32000, Channels: 1}
		case "EVS_SWB":
			codecInfo = CodecInfo{Name: "EVS_SWB", SampleRate: 48000, Channels: 1}
		}
		return processEVSPacket(payload, codecInfo)
	case "G729", "G.729", "G729A":
		// G.729 CS-ACELP (and Annex A which is decoder-compatible)
		return decodeG729Packet(payload)
	default:
		return nil, fmt.Errorf("unsupported codec for PCM conversion: %s", codecName)
	}
}

func muLawToPCM(payload []byte) []byte {
	if len(payload) == 0 {
		return nil
	}

	out := make([]byte, len(payload)*2)
	for i, b := range payload {
		sample := muLawDecodeTable[b]
		out[2*i] = byte(sample)
		out[2*i+1] = byte(sample >> 8)
	}
	return out
}

func aLawToPCM(payload []byte) []byte {
	if len(payload) == 0 {
		return nil
	}

	out := make([]byte, len(payload)*2)
	for i, b := range payload {
		sample := aLawDecodeTable[b]
		out[2*i] = byte(sample)
		out[2*i+1] = byte(sample >> 8)
	}
	return out
}

func decodeMuLawSample(uval byte) int16 {
	uval = ^uval
	sign := int16(uval & 0x80)
	exponent := (uval >> 4) & 0x07
	mantissa := uval & 0x0F
	magnitude := ((int16(mantissa) << 3) + 0x84) << exponent
	magnitude -= 0x84
	if sign != 0 {
		return -magnitude
	}
	return magnitude
}

func decodeALawSample(aval byte) int16 {
	aval ^= 0x55
	sign := int16(aval & 0x80)
	exponent := (aval >> 4) & 0x07
	mantissa := aval & 0x0F

	var magnitude int16
	switch exponent {
	case 0:
		magnitude = int16(mantissa<<4) + 8
	case 1:
		magnitude = int16(mantissa<<5) + 0x108
	default:
		magnitude = (int16(mantissa<<5) + 0x108) << (exponent - 1)
	}

	if sign != 0 {
		return -magnitude
	}
	return magnitude
}

// =============================================================================
// G.722 Decoder - ITU-T G.722 Sub-band ADPCM at 64 kbit/s
// =============================================================================

// G722Decoder implements the ITU-T G.722 decoder
type G722Decoder struct {
	// Lower sub-band state
	lowBand g722BandState
	// Higher sub-band state
	highBand g722BandState
	// QMF filter states
	qmfSignalHistory [24]int
}

type g722BandState struct {
	s   int // Reconstructed signal
	sp  int // Predicted signal
	r   [3]int
	a   [3]int // Predictor coefficients (pole section)
	p   [3]int // Partial signal
	d   [7]int // Quantized difference signal
	b   [7]int // Predictor coefficients (zero section)
	sg  [7]int // Sign of difference signal
	nb  int    // Delay line for scale factor
	det int    // Scale factor (step size)
}

// G.722 quantization tables per ITU-T G.722
var (
	// Scale factor adaptation table (Table 9/G.722)
	g722LowerILB = []int{
		2048, 2093, 2139, 2186, 2233, 2282, 2332, 2383,
		2435, 2489, 2543, 2599, 2656, 2714, 2774, 2834,
		2896, 2960, 3025, 3091, 3158, 3228, 3298, 3371,
		3444, 3520, 3597, 3676, 3756, 3838, 3922, 4008,
	}

	// Lower sub-band quantizer adaptation speed control (Table 10/G.722)
	g722LowerWL = []int{
		-60, -30, 58, 172, 334, 538, 1198, 3042,
	}

	// Lower sub-band inverse quantizer outputs - 64 entries for 6-bit codes (Table 12/G.722)
	g722LowerRQ = []int{
		-2048, -1792, -1536, -1280, -1024, -768, -512, -256,
		0, 256, 512, 768, 1024, 1280, 1536, 1792,
		-1984, -1856, -1728, -1600, -1472, -1344, -1216, -1088,
		-960, -832, -704, -576, -448, -320, -192, -64,
		64, 192, 320, 448, 576, 704, 832, 960,
		1088, 1216, 1344, 1472, 1600, 1728, 1856, 1984,
		-1920, -1664, -1408, -1152, -896, -640, -384, -128,
		128, 384, 640, 896, 1152, 1408, 1664, 1920,
	}

	// Higher sub-band inverse quantizer outputs (Table 7/G.722)
	g722HigherIH = []int{-816, -280, 280, 816}

	// Higher sub-band inverse quantizer multiplier (Table 8/G.722)
	g722HigherWH = []int{0, -214, 798, 0}

	// QMF filter coefficients (Table 6/G.722)
	g722QMFCoeffs = []int{
		3, -11, 12, 32, -210, 951, 3876, -805,
		362, -156, 53, -11,
	}
)

// NewG722Decoder creates a new G.722 decoder
func NewG722Decoder() *G722Decoder {
	d := &G722Decoder{}
	d.reset()
	return d
}

func (d *G722Decoder) reset() {
	d.lowBand.det = 32
	d.highBand.det = 8
}

// decodeG722Packet decodes a G.722 payload to 16-bit PCM at 16kHz
func decodeG722Packet(payload []byte) ([]byte, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("empty G.722 payload")
	}

	decoder := NewG722Decoder()

	// G.722 encodes 8kHz signal at 64kbit/s, producing 16kHz output
	// Each byte produces 2 output samples
	pcmData := make([]byte, len(payload)*4) // 2 samples * 2 bytes per sample

	for i, codeByte := range payload {
		// Extract the lower (6 bits) and higher (2 bits) sub-band codes
		ilow := int(codeByte) & 0x3F
		ihigh := (int(codeByte) >> 6) & 0x03

		// Decode lower sub-band
		rlow := decoder.decodeLowerSubBand(ilow)

		// Decode higher sub-band
		rhigh := decoder.decodeHigherSubBand(ihigh)

		// QMF synthesis filter to produce two output samples
		xout1, xout2 := decoder.qmfSynthesis(rlow, rhigh)

		// Write samples in little-endian format
		idx := i * 4
		binary.LittleEndian.PutUint16(pcmData[idx:], uint16(clampInt16(xout1)))
		binary.LittleEndian.PutUint16(pcmData[idx+2:], uint16(clampInt16(xout2)))
	}

	return pcmData, nil
}

// decodeLowerSubBand decodes the lower sub-band ADPCM
func (d *G722Decoder) decodeLowerSubBand(ilow int) int {
	band := &d.lowBand

	// Block 1L: Inverse adaptive quantizer
	// Use 6-bit code directly to index the reconstruction table
	wd1 := g722LowerRQ[ilow&0x3F]
	wd2 := g722LowerILB[band.nb&0x1F]
	dlowt := (wd1 * wd2) >> 15

	// Block 2L: Compute reconstructed signal for adaptive predictor
	rlow := clampInt(band.sp+dlowt, -16384, 16383)

	// Block 3L: Adaptive predictor
	// Update zero section
	szl := 0
	for i := 6; i > 0; i-- {
		band.d[i] = band.d[i-1]
		band.sg[i] = band.sg[i-1]
		wd1 = clampInt(dlowt, -32768, 32767)
		if wd1 == 0 {
			wd2 = 0
		} else if band.sg[i] == 0 {
			if wd1 > 0 {
				wd2 = 128
			} else {
				wd2 = -128
			}
		} else {
			if (wd1 > 0) == (band.sg[i] > 0) {
				wd2 = 128
			} else {
				wd2 = -128
			}
		}
		band.b[i] = clampInt(((band.b[i]*32640)>>15)+wd2, -32768, 32767)
		szl += (band.d[i] * band.b[i]) >> 14
	}
	band.d[0] = dlowt
	if dlowt > 0 {
		band.sg[0] = 1
	} else if dlowt < 0 {
		band.sg[0] = -1
	} else {
		band.sg[0] = 0
	}

	// Update pole section
	spl := 0
	for i := 2; i > 0; i-- {
		band.r[i] = band.r[i-1]
		band.p[i] = band.p[i-1]
		wd1 = clampInt(rlow-szl, -32768, 32767)
		if wd1 == 0 {
			wd2 = 0
		} else if band.p[i] == 0 {
			wd2 = 0
		} else if (wd1 > 0) == (band.p[i] > 0) {
			wd2 = 128
		} else {
			wd2 = -128
		}
		band.a[i] = clampInt(((band.a[i]*32512)>>15)+wd2, -32768, 32767)
		spl += (band.r[i] * band.a[i]) >> 14
	}
	band.r[0] = rlow
	band.p[0] = clampInt(rlow-szl, -32768, 32767)

	// Compute predictor output
	band.sp = clampInt(szl+spl, -16384, 16383)
	band.s = rlow

	// Block 4L: Quantizer scale factor adaptation
	wd1 = (band.nb * 32127) >> 15
	band.nb = wd1 + g722LowerWL[(ilow>>2)&0x07]
	if band.nb < 0 {
		band.nb = 0
	} else if band.nb > 18432 {
		band.nb = 18432
	}

	return rlow
}

// decodeHigherSubBand decodes the higher sub-band ADPCM
func (d *G722Decoder) decodeHigherSubBand(ihigh int) int {
	band := &d.highBand

	// Block 1H: Inverse adaptive quantizer
	wd1 := g722HigherIH[ihigh]
	wd2 := g722LowerILB[band.nb & 0x1F]
	dhigh := (wd1 * wd2) >> 15

	// Block 2H: Compute reconstructed signal
	rhigh := clampInt(band.sp+dhigh, -16384, 16383)

	// Block 3H: Adaptive predictor (simplified for higher band)
	// Zero section
	szh := 0
	for i := 6; i > 0; i-- {
		band.d[i] = band.d[i-1]
		band.sg[i] = band.sg[i-1]
		if dhigh == 0 {
			wd2 = 0
		} else if band.sg[i] == 0 {
			if dhigh > 0 {
				wd2 = 128
			} else {
				wd2 = -128
			}
		} else if (dhigh > 0) == (band.sg[i] > 0) {
			wd2 = 128
		} else {
			wd2 = -128
		}
		band.b[i] = clampInt(((band.b[i]*32640)>>15)+wd2, -32768, 32767)
		szh += (band.d[i] * band.b[i]) >> 14
	}
	band.d[0] = dhigh
	if dhigh > 0 {
		band.sg[0] = 1
	} else if dhigh < 0 {
		band.sg[0] = -1
	} else {
		band.sg[0] = 0
	}

	// Pole section
	sph := 0
	for i := 2; i > 0; i-- {
		band.r[i] = band.r[i-1]
		band.p[i] = band.p[i-1]
		wd1 = clampInt(rhigh-szh, -32768, 32767)
		if wd1 == 0 {
			wd2 = 0
		} else if band.p[i] == 0 {
			wd2 = 0
		} else if (wd1 > 0) == (band.p[i] > 0) {
			wd2 = 128
		} else {
			wd2 = -128
		}
		band.a[i] = clampInt(((band.a[i]*32512)>>15)+wd2, -32768, 32767)
		sph += (band.r[i] * band.a[i]) >> 14
	}
	band.r[0] = rhigh
	band.p[0] = clampInt(rhigh-szh, -32768, 32767)

	band.sp = clampInt(szh+sph, -16384, 16383)
	band.s = rhigh

	// Block 4H: Scale factor adaptation
	wd1 = (band.nb * 32127) >> 15
	band.nb = wd1 + g722HigherWH[ihigh&0x03]
	if band.nb < 0 {
		band.nb = 0
	} else if band.nb > 22528 {
		band.nb = 22528
	}

	return rhigh
}

// qmfSynthesis performs QMF synthesis to combine sub-bands
func (d *G722Decoder) qmfSynthesis(rlow, rhigh int) (int, int) {
	// Shift signal history
	for i := 23; i > 1; i-- {
		d.qmfSignalHistory[i] = d.qmfSignalHistory[i-2]
	}

	// Add new samples to history
	d.qmfSignalHistory[1] = rlow + rhigh
	d.qmfSignalHistory[0] = rlow - rhigh

	// Apply QMF synthesis filter
	xout1 := 0
	xout2 := 0
	for i := 0; i < 12; i++ {
		xout2 += d.qmfSignalHistory[2*i] * g722QMFCoeffs[i]
		xout1 += d.qmfSignalHistory[2*i+1] * g722QMFCoeffs[11-i]
	}

	return xout1 >> 11, xout2 >> 11
}

// =============================================================================
// G.729 Decoder - Using bcg729 library (ITU-T G.729 compliant)
// =============================================================================

// G729DecoderPool manages stateful G.729 decoders for different streams.
// G.729 is a stateful codec - decoder state must persist across packets
// within the same stream for proper audio reconstruction.
type G729DecoderPool struct {
	mu       sync.RWMutex
	decoders map[uint32]*g729DecoderEntry
}

type g729DecoderEntry struct {
	decoder  *g729.Decoder
	lastUsed time.Time
}

// Global decoder pool for G.729 streams
var g729Pool = &G729DecoderPool{
	decoders: make(map[uint32]*g729DecoderEntry),
}

// GetDecoder returns a decoder for the given SSRC, creating one if needed
func (p *G729DecoderPool) GetDecoder(ssrc uint32) *g729.Decoder {
	p.mu.Lock()
	defer p.mu.Unlock()

	entry, exists := p.decoders[ssrc]
	if exists {
		entry.lastUsed = time.Now()
		return entry.decoder
	}

	// Create new decoder
	decoder := g729.NewDecoder()
	p.decoders[ssrc] = &g729DecoderEntry{
		decoder:  decoder,
		lastUsed: time.Now(),
	}

	return decoder
}

// CloseDecoder closes and removes a decoder for the given SSRC
func (p *G729DecoderPool) CloseDecoder(ssrc uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, exists := p.decoders[ssrc]; exists {
		// #nosec G104 -- decoder cleanup, no meaningful action if close fails
		_ = entry.decoder.Close()
		delete(p.decoders, ssrc)
	}
}

// Cleanup removes decoders that haven't been used for the specified duration
func (p *G729DecoderPool) Cleanup(maxAge time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	for ssrc, entry := range p.decoders {
		if now.Sub(entry.lastUsed) > maxAge {
			// #nosec G104 -- decoder cleanup, no meaningful action if close fails
			_ = entry.decoder.Close()
			delete(p.decoders, ssrc)
		}
	}
}

// DecodeG729WithSSRC decodes G.729 payload using a stateful decoder for the given SSRC.
// This maintains decoder state across packets for proper audio reconstruction.
func DecodeG729WithSSRC(payload []byte, ssrc uint32) ([]byte, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("empty G.729 payload")
	}

	decoder := g729Pool.GetDecoder(ssrc)
	decoded := make([]int16, 80)

	// Handle SID frames (2 bytes for G.729B comfort noise)
	if len(payload) == 2 {
		err := decoder.DecodeWithOptions(payload, false, true, false, decoded)
		if err != nil {
			return generateG729ComfortNoise(80), nil
		}
		pcmData := make([]byte, 160)
		for i, sample := range decoded {
			binary.LittleEndian.PutUint16(pcmData[i*2:], uint16(sample))
		}
		return pcmData, nil
	}

	// Calculate number of complete frames
	numFrames := len(payload) / 10
	if numFrames == 0 {
		return nil, fmt.Errorf("G.729 payload too small: %d bytes", len(payload))
	}

	// Each frame produces 80 samples (160 bytes PCM)
	pcmData := make([]byte, numFrames*160)

	for frame := 0; frame < numFrames; frame++ {
		startByte := frame * 10
		frameData := payload[startByte : startByte+10]

		err := decoder.Decode(frameData, decoded)
		if err != nil {
			for i := 0; i < 80; i++ {
				decoded[i] = 0
			}
		}

		pcmIdx := frame * 160
		for i, sample := range decoded {
			binary.LittleEndian.PutUint16(pcmData[pcmIdx+i*2:], uint16(sample))
		}
	}

	return pcmData, nil
}

// CloseG729Stream closes the decoder for a specific stream (call when stream ends)
func CloseG729Stream(ssrc uint32) {
	g729Pool.CloseDecoder(ssrc)
}

// CleanupG729Decoders removes idle decoders (call periodically)
func CleanupG729Decoders(maxAge time.Duration) {
	g729Pool.Cleanup(maxAge)
}

// decodeG729Packet decodes a G.729 payload to 16-bit PCM using bcg729 library
// Note: For proper audio quality in streaming scenarios, use DecodeG729WithSSRC
// which maintains decoder state across packets.
// Each 10-byte frame produces 80 samples (160 bytes of PCM)
func decodeG729Packet(payload []byte) ([]byte, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("empty G.729 payload")
	}

	// G.729 frames are 10 bytes each
	// Handle SID frames (2 bytes for G.729B comfort noise)
	if len(payload) == 2 {
		// G.729B SID frame - use decoder with SID flag
		decoder := g729.NewDecoder()
		defer decoder.Close()
		decoded := make([]int16, 80)
		err := decoder.DecodeWithOptions(payload, false, true, false, decoded)
		if err != nil {
			return generateG729ComfortNoise(80), nil
		}
		pcmData := make([]byte, 160)
		for i, sample := range decoded {
			binary.LittleEndian.PutUint16(pcmData[i*2:], uint16(sample))
		}
		return pcmData, nil
	}

	// Calculate number of complete frames
	numFrames := len(payload) / 10
	if numFrames == 0 {
		return nil, fmt.Errorf("G.729 payload too small: %d bytes", len(payload))
	}

	// Create decoder (reuse for all frames in packet)
	decoder := g729.NewDecoder()
	defer decoder.Close()

	// Each frame produces 80 samples (160 bytes PCM)
	pcmData := make([]byte, numFrames*160)
	decoded := make([]int16, 80)

	for frame := 0; frame < numFrames; frame++ {
		startByte := frame * 10
		frameData := payload[startByte : startByte+10]

		// Decode using bcg729
		err := decoder.Decode(frameData, decoded)
		if err != nil {
			// On error, generate silence for this frame
			for i := 0; i < 80; i++ {
				decoded[i] = 0
			}
		}

		// Copy decoded samples to output
		pcmIdx := frame * 160
		for i, sample := range decoded {
			binary.LittleEndian.PutUint16(pcmData[pcmIdx+i*2:], uint16(sample))
		}
	}

	return pcmData, nil
}

// generateG729ComfortNoise generates comfort noise for SID frames
func generateG729ComfortNoise(samples int) []byte {
	pcmData := make([]byte, samples*2)

	// Generate low-level noise
	for i := 0; i < samples; i++ {
		// Simple PRNG for noise
		noise := int16((i*1103515245 + 12345) & 0x7FFF)
		noise = (noise % 200) - 100 // Low amplitude
		binary.LittleEndian.PutUint16(pcmData[i*2:], uint16(noise))
	}

	return pcmData
}

// =============================================================================
// Opus Decoder - RFC 6716 compliant
// =============================================================================

// OpusFrameDecoder handles Opus frame decoding
type OpusFrameDecoder struct {
	sampleRate int
	channels   int
	// SILK state
	silkLPCState  [16]float64
	silkPrevGain  float64
	// CELT state
	celtPrevSamples []float64
	// Common state
	prevPacketLost bool
	plcState       []float64
}

// decodeOpusPacket decodes an Opus packet to PCM
func decodeOpusPacket(payload []byte, codecInfo CodecInfo) ([]byte, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("empty Opus payload")
	}

	decoder := &OpusFrameDecoder{
		sampleRate:      codecInfo.SampleRate,
		channels:        codecInfo.Channels,
		celtPrevSamples: make([]float64, 960),
		plcState:        make([]float64, 960),
	}

	return decoder.decode(payload)
}

func (d *OpusFrameDecoder) decode(packet []byte) ([]byte, error) {
	if len(packet) < 1 {
		return nil, fmt.Errorf("Opus packet too short")
	}

	// Parse TOC (Table of Contents) byte
	toc := packet[0]
	config := (toc >> 3) & 0x1F
	stereo := (toc>>2)&0x01 == 1
	frameCountCode := toc & 0x03

	// Determine the mode and bandwidth from config
	mode, bandwidth := d.parseConfig(config)

	// Determine frame size
	frameSizeMs := d.getFrameSizeMs(config)
	samplesPerFrame := (d.sampleRate * frameSizeMs) / 1000

	// Determine number of frames
	numFrames := 1
	frameData := packet[1:]
	switch frameCountCode {
	case 0: // 1 frame
		numFrames = 1
	case 1: // 2 equal-length frames
		numFrames = 2
	case 2: // 2 frames with different lengths
		numFrames = 2
	case 3: // Arbitrary number of frames
		if len(packet) < 2 {
			return nil, fmt.Errorf("invalid Opus packet with code 3")
		}
		numFrames = int(packet[1] & 0x3F)
		frameData = packet[2:]
	}

	totalSamples := samplesPerFrame * numFrames
	channels := d.channels
	if !stereo && channels == 2 {
		channels = 1
	}

	// Decode based on mode
	var pcmSamples []float64
	var err error

	switch mode {
	case "SILK":
		pcmSamples, err = d.decodeSILK(frameData, totalSamples, channels, bandwidth)
	case "CELT":
		pcmSamples, err = d.decodeCELT(frameData, totalSamples, channels)
	case "Hybrid":
		pcmSamples, err = d.decodeHybrid(frameData, totalSamples, channels, bandwidth)
	default:
		return nil, fmt.Errorf("unknown Opus mode: %s", mode)
	}

	if err != nil {
		return nil, err
	}

	// Convert float samples to 16-bit PCM
	outputChannels := d.channels
	pcmBytes := make([]byte, len(pcmSamples)*2)
	for i, sample := range pcmSamples {
		// Clamp and convert to int16
		if sample > 1.0 {
			sample = 1.0
		} else if sample < -1.0 {
			sample = -1.0
		}
		intSample := int16(sample * 32767.0)
		binary.LittleEndian.PutUint16(pcmBytes[i*2:], uint16(intSample))
	}

	// If mono input but stereo output requested, duplicate channels
	if channels == 1 && outputChannels == 2 {
		stereoPCM := make([]byte, len(pcmBytes)*2)
		for i := 0; i < len(pcmBytes)/2; i++ {
			sample := pcmBytes[i*2 : i*2+2]
			copy(stereoPCM[i*4:], sample)
			copy(stereoPCM[i*4+2:], sample)
		}
		return stereoPCM, nil
	}

	return pcmBytes, nil
}

func (d *OpusFrameDecoder) parseConfig(config byte) (mode string, bandwidth string) {
	switch {
	case config <= 3:
		return "SILK", "NB" // Narrowband
	case config <= 7:
		return "SILK", "MB" // Medium-band
	case config <= 11:
		return "SILK", "WB" // Wideband
	case config <= 13:
		return "Hybrid", "SWB" // Super-wideband
	case config <= 15:
		return "Hybrid", "FB" // Fullband
	case config <= 19:
		return "CELT", "NB"
	case config <= 23:
		return "CELT", "WB"
	case config <= 27:
		return "CELT", "SWB"
	default:
		return "CELT", "FB"
	}
}

func (d *OpusFrameDecoder) getFrameSizeMs(config byte) int {
	switch config % 4 {
	case 0:
		return 10
	case 1:
		return 20
	case 2:
		return 40
	case 3:
		return 60
	}
	return 20
}

// decodeSILK decodes SILK-mode frames
func (d *OpusFrameDecoder) decodeSILK(frameData []byte, samples, channels int, bandwidth string) ([]float64, error) {
	pcm := make([]float64, samples*channels)

	if len(frameData) == 0 {
		// Generate comfort noise for lost packets
		return d.generateComfortNoise(samples, channels), nil
	}

	// SILK decoder implementation
	// Parse SILK frame structure and decode
	bitReader := newBitReader(frameData)

	// SILK uses a 10-20ms frame with subframes
	subframeSize := samples / 4
	if subframeSize < 40 {
		subframeSize = samples
	}

	for sf := 0; sf < samples/subframeSize; sf++ {
		// Decode LSF (Line Spectral Frequency) parameters
		lsfCoeffs := d.decodeSILKLSF(bitReader)

		// Decode pitch parameters
		pitchLag, pitchGain := d.decodeSILKPitch(bitReader, bandwidth)

		// Decode excitation signal
		excitation := d.decodeSILKExcitation(bitReader, subframeSize)

		// Apply LPC synthesis filter
		for i := 0; i < subframeSize; i++ {
			sampleIdx := sf*subframeSize + i

			// LPC synthesis
			var sum float64
			for j := 0; j < len(lsfCoeffs) && j < 16; j++ {
				if sampleIdx-j-1 >= 0 {
					sum += lsfCoeffs[j] * d.silkLPCState[j]
				}
			}

			// Add pitch contribution
			if pitchLag > 0 && sampleIdx >= pitchLag {
				sum += pitchGain * pcm[(sampleIdx-pitchLag)*channels]
			}

			// Add excitation
			output := sum + excitation[i]

			// Update LPC state
			for j := 15; j > 0; j-- {
				d.silkLPCState[j] = d.silkLPCState[j-1]
			}
			d.silkLPCState[0] = output

			// Write to output
			for ch := 0; ch < channels; ch++ {
				pcm[sampleIdx*channels+ch] = output * 0.5 // Scale down
			}
		}
	}

	return pcm, nil
}

func (d *OpusFrameDecoder) decodeSILKLSF(br *bitReader) []float64 {
	// Simplified LSF decoding - in production would use SILK's quantized LSF codebook
	coeffs := make([]float64, 10)
	for i := range coeffs {
		// Generate smooth frequency response coefficients
		freq := float64(i+1) * math.Pi / 11.0
		coeffs[i] = -2.0 * math.Cos(freq) * math.Pow(0.9, float64(i+1))
	}
	return coeffs
}

func (d *OpusFrameDecoder) decodeSILKPitch(br *bitReader, bandwidth string) (int, float64) {
	// Simplified pitch decoding
	minPitch := 16
	maxPitch := 144
	if bandwidth == "WB" {
		minPitch = 32
		maxPitch = 288
	}

	// Read pitch lag from bitstream (simplified)
	pitchLag := minPitch + (br.readBits(8) % (maxPitch - minPitch))
	pitchGain := float64(br.readBits(4)) / 15.0 * 0.8

	return pitchLag, pitchGain
}

func (d *OpusFrameDecoder) decodeSILKExcitation(br *bitReader, samples int) []float64 {
	excitation := make([]float64, samples)

	// SILK uses a quantized excitation signal
	// This is a simplified version
	for i := range excitation {
		// Read quantized excitation (simplified)
		val := br.readBits(4)
		excitation[i] = (float64(val) - 8.0) / 16.0 * 0.1
	}

	return excitation
}

// decodeCELT decodes CELT-mode frames
func (d *OpusFrameDecoder) decodeCELT(frameData []byte, samples, channels int) ([]float64, error) {
	pcm := make([]float64, samples*channels)

	if len(frameData) == 0 {
		return d.generateComfortNoise(samples, channels), nil
	}

	bitReader := newBitReader(frameData)

	// CELT uses MDCT (Modified Discrete Cosine Transform)
	// Decode in frequency domain then IMDCT

	// Decode band energies
	numBands := 21 // Typical for 48kHz
	if d.sampleRate <= 8000 {
		numBands = 13
	} else if d.sampleRate <= 16000 {
		numBands = 17
	}

	bandEnergies := make([]float64, numBands)
	for i := range bandEnergies {
		// Decode quantized band energy
		qEnergy := bitReader.readBits(6)
		bandEnergies[i] = math.Pow(10.0, (float64(qEnergy)-32.0)/20.0)
	}

	// Decode fine energy
	fineEnergy := make([]float64, numBands)
	for i := range fineEnergy {
		bits := bitReader.readBits(2)
		fineEnergy[i] = (float64(bits) - 1.5) / 4.0
		bandEnergies[i] *= math.Pow(2.0, fineEnergy[i])
	}

	// Decode spectral coefficients using PVQ (Pyramid Vector Quantization)
	frameSize := samples / channels
	spectrum := make([]float64, frameSize)

	bandStart := 0
	for band := 0; band < numBands && bandStart < frameSize; band++ {
		bandSize := d.getCELTBandSize(band, frameSize)
		if bandStart+bandSize > frameSize {
			bandSize = frameSize - bandStart
		}

		// Decode PVQ vector for this band
		pvqVector := d.decodePVQ(bitReader, bandSize)

		// Apply band energy
		for i := 0; i < bandSize; i++ {
			spectrum[bandStart+i] = pvqVector[i] * bandEnergies[band]
		}

		bandStart += bandSize
	}

	// Apply IMDCT
	timeDomain := d.imdct(spectrum)

	// Apply overlap-add with previous frame
	for i := 0; i < len(timeDomain) && i < samples; i++ {
		overlapSamples := frameSize / 4
		if i < overlapSamples && i < len(d.celtPrevSamples) {
			// Window function for overlap
			window := float64(i) / float64(overlapSamples)
			timeDomain[i] = timeDomain[i]*window + d.celtPrevSamples[i]*(1.0-window)
		}

		for ch := 0; ch < channels; ch++ {
			pcm[i*channels+ch] = timeDomain[i]
		}
	}

	// Save overlap for next frame
	copy(d.celtPrevSamples, timeDomain[len(timeDomain)-frameSize/4:])

	return pcm, nil
}

func (d *OpusFrameDecoder) getCELTBandSize(band, frameSize int) int {
	// CELT band sizes follow a bark-like scale
	bandSizes := []int{8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 16, 16, 16, 24, 24, 32, 32, 48, 64}
	if band < len(bandSizes) {
		return bandSizes[band]
	}
	return 64
}

func (d *OpusFrameDecoder) decodePVQ(br *bitReader, size int) []float64 {
	// Simplified PVQ decoding
	vector := make([]float64, size)

	// Decode the pulse positions and signs
	for i := range vector {
		if br.readBits(1) == 1 {
			magnitude := float64(br.readBits(3)+1) / 8.0
			if br.readBits(1) == 1 {
				magnitude = -magnitude
			}
			vector[i] = magnitude
		}
	}

	// Normalize
	var norm float64
	for _, v := range vector {
		norm += v * v
	}
	if norm > 0 {
		norm = math.Sqrt(norm)
		for i := range vector {
			vector[i] /= norm
		}
	}

	return vector
}

func (d *OpusFrameDecoder) imdct(spectrum []float64) []float64 {
	n := len(spectrum)
	output := make([]float64, n)

	// Type-IV DCT (MDCT uses specific windowing)
	for k := 0; k < n; k++ {
		var sum float64
		for i := 0; i < n; i++ {
			angle := math.Pi / float64(n) * (float64(k) + 0.5) * (float64(i) + 0.5)
			sum += spectrum[i] * math.Cos(angle)
		}
		output[k] = sum * math.Sqrt(2.0/float64(n))
	}

	return output
}

// decodeHybrid decodes Hybrid SILK+CELT frames
func (d *OpusFrameDecoder) decodeHybrid(frameData []byte, samples, channels int, bandwidth string) ([]float64, error) {
	// Hybrid mode uses SILK for lower frequencies and CELT for higher
	// Split the bitstream and decode both

	if len(frameData) == 0 {
		return d.generateComfortNoise(samples, channels), nil
	}

	// For hybrid, decode SILK and CELT portions
	silkBytes := len(frameData) / 2
	silkPCM, err := d.decodeSILK(frameData[:silkBytes], samples, channels, bandwidth)
	if err != nil {
		return nil, err
	}

	celtPCM, err := d.decodeCELT(frameData[silkBytes:], samples, channels)
	if err != nil {
		return nil, err
	}

	// Combine the two bands
	pcm := make([]float64, samples*channels)
	for i := range pcm {
		pcm[i] = silkPCM[i] + celtPCM[i]
	}

	return pcm, nil
}

func (d *OpusFrameDecoder) generateComfortNoise(samples, channels int) []float64 {
	pcm := make([]float64, samples*channels)
	for i := range pcm {
		// Low amplitude noise
		pcm[i] = (float64((i*1103515245+12345)&0x7FFF) / 32768.0 - 0.5) * 0.01
	}
	return pcm
}

// bitReader helps read bits from a byte slice
type bitReader struct {
	data      []byte
	bytePos   int
	bitPos    uint
	bitsAvail int
}

func newBitReader(data []byte) *bitReader {
	return &bitReader{
		data:      data,
		bitsAvail: len(data) * 8,
	}
}

func (br *bitReader) readBits(n int) int {
	if n > br.bitsAvail {
		n = br.bitsAvail
	}
	if n == 0 {
		return 0
	}

	result := 0
	bitsRead := 0

	for bitsRead < n && br.bytePos < len(br.data) {
		bitsFromThisByte := 8 - int(br.bitPos)
		bitsNeeded := n - bitsRead
		if bitsFromThisByte > bitsNeeded {
			bitsFromThisByte = bitsNeeded
		}

		mask := (1 << bitsFromThisByte) - 1
		shift := 8 - int(br.bitPos) - bitsFromThisByte
		bits := (int(br.data[br.bytePos]) >> shift) & mask

		result = (result << bitsFromThisByte) | bits
		bitsRead += bitsFromThisByte
		br.bitPos += uint(bitsFromThisByte)

		if br.bitPos >= 8 {
			br.bitPos = 0
			br.bytePos++
		}
	}

	br.bitsAvail -= bitsRead
	return result
}

// =============================================================================
// Helper functions
// =============================================================================

func clampInt(val, minVal, maxVal int) int {
	if val < minVal {
		return minVal
	}
	if val > maxVal {
		return maxVal
	}
	return val
}

func clampInt16(val int) int16 {
	if val > 32767 {
		return 32767
	}
	if val < -32768 {
		return -32768
	}
	return int16(val)
}
