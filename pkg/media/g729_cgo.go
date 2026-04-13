//go:build cgo

package media

import (
	"encoding/binary"
	"fmt"

	"github.com/pidato/audio/g729"
)

// G729StreamDecoder wraps a stateful G.729 decoder for a single RTP stream.
// It must be used from a single goroutine (not thread-safe).
// The decoder tracks the SSRC and resets itself if the source changes mid-stream.
type G729StreamDecoder struct {
	decoder *g729.Decoder
	ssrc    uint32
	active  bool
}

// NewG729StreamDecoder creates a new per-stream G.729 decoder.
func NewG729StreamDecoder() *G729StreamDecoder {
	return &G729StreamDecoder{
		decoder: g729.NewDecoder(),
		active:  true,
	}
}

// Decode decodes a G.729 payload, resetting the decoder if the SSRC changed.
func (d *G729StreamDecoder) Decode(payload []byte, ssrc uint32) ([]byte, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("empty G.729 payload")
	}

	if d.ssrc != 0 && d.ssrc != ssrc {
		// #nosec G104 -- decoder cleanup, no meaningful action if close fails
		_ = d.decoder.Close()
		d.decoder = g729.NewDecoder()
	}
	d.ssrc = ssrc

	decoded := make([]int16, 80)

	if len(payload) == 2 {
		err := d.decoder.DecodeWithOptions(payload, false, true, false, decoded)
		if err != nil {
			return generateG729ComfortNoise(80), nil
		}
		pcmData := make([]byte, 160)
		for i, sample := range decoded {
			binary.LittleEndian.PutUint16(pcmData[i*2:], uint16(sample))
		}
		return pcmData, nil
	}

	numFrames := len(payload) / 10
	if numFrames == 0 {
		return nil, fmt.Errorf("G.729 payload too small: %d bytes", len(payload))
	}

	pcmData := make([]byte, numFrames*160)

	for frame := 0; frame < numFrames; frame++ {
		startByte := frame * 10
		frameData := payload[startByte : startByte+10]

		err := d.decoder.Decode(frameData, decoded)
		if err != nil || isG729Oscillation(decoded) {
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

// ConcealPackets generates packet-loss concealment audio for lost RTP packets.
// Each call advances the decoder state so the next real frame decodes cleanly.
// pcmBytesPerPacket should match the PCM output of a normal decode (typically
// 320 bytes for 20ms G.729 packets or 160 bytes for 10ms packets).
func (d *G729StreamDecoder) ConcealPackets(numPackets, pcmBytesPerPacket int) []byte {
	if numPackets <= 0 || d.decoder == nil || pcmBytesPerPacket <= 0 {
		return nil
	}
	framesPerPacket := pcmBytesPerPacket / 160
	if framesPerPacket <= 0 {
		framesPerPacket = 2
	}
	totalFrames := numPackets * framesPerPacket
	pcmData := make([]byte, totalFrames*160)
	decoded := make([]int16, 80)
	dummyFrame := make([]byte, 10)
	for i := 0; i < totalFrames; i++ {
		_ = d.decoder.DecodeWithOptions(dummyFrame, true, false, false, decoded)
		for j, sample := range decoded {
			binary.LittleEndian.PutUint16(pcmData[i*160+j*2:], uint16(sample))
		}
	}
	return pcmData
}

// Close releases the underlying decoder resources.
func (d *G729StreamDecoder) Close() {
	if d.decoder != nil {
		// #nosec G104 -- decoder cleanup, no meaningful action if close fails
		_ = d.decoder.Close()
		d.decoder = nil
	}
	d.active = false
}

// isG729Oscillation detects when the G.729 synthesis filter has gone unstable.
func isG729Oscillation(decoded []int16) bool {
	const railThreshold int16 = 30000
	n := len(decoded)
	if n < 16 {
		return false
	}

	railedCount := 0
	signChanges := 0
	prevPositive := decoded[0] > 0
	for i := 0; i < n; i++ {
		s := decoded[i]
		if s > railThreshold || s < -railThreshold {
			railedCount++
		}
		currPositive := s > 0
		if i > 0 && currPositive != prevPositive {
			signChanges++
		}
		prevPositive = currPositive
	}

	return railedCount > n/2 && signChanges > n/4
}

// DecodeG729WithSSRC is a stateless convenience wrapper that decodes a G.729
// payload using a fresh decoder.
func DecodeG729WithSSRC(payload []byte, ssrc uint32) ([]byte, error) {
	return decodeG729Packet(payload)
}

// decodeG729Packet decodes a G.729 payload to 16-bit PCM using bcg729 library.
// Each 10-byte frame produces 80 samples (160 bytes of PCM).
func decodeG729Packet(payload []byte) ([]byte, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("empty G.729 payload")
	}

	if len(payload) == 2 {
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

	numFrames := len(payload) / 10
	if numFrames == 0 {
		return nil, fmt.Errorf("G.729 payload too small: %d bytes", len(payload))
	}

	decoder := g729.NewDecoder()
	defer decoder.Close()

	pcmData := make([]byte, numFrames*160)
	decoded := make([]int16, 80)

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

// generateG729ComfortNoise generates comfort noise for SID frames.
func generateG729ComfortNoise(samples int) []byte {
	pcmData := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		noise := int16((i*1103515245 + 12345) & 0x7FFF)
		noise = (noise % 200) - 100
		binary.LittleEndian.PutUint16(pcmData[i*2:], uint16(noise))
	}
	return pcmData
}
