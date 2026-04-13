//go:build !cgo

package media

import (
	"encoding/binary"
	"fmt"
)

// G729StreamDecoder is a stub for non-CGO builds where the bcg729 library
// is unavailable. All decode operations return an error.
type G729StreamDecoder struct {
	ssrc   uint32
	active bool
}

// NewG729StreamDecoder creates a new stub G.729 decoder.
func NewG729StreamDecoder() *G729StreamDecoder {
	return &G729StreamDecoder{active: true}
}

// Decode returns an error because G.729 requires CGO.
func (d *G729StreamDecoder) Decode(payload []byte, ssrc uint32) ([]byte, error) {
	return nil, fmt.Errorf("G.729 decoding requires CGO (build with CGO_ENABLED=1)")
}

// ConcealPackets returns nil because G.729 requires CGO.
func (d *G729StreamDecoder) ConcealPackets(numPackets, pcmBytesPerPacket int) []byte {
	return nil
}

// Close is a no-op in non-CGO builds.
func (d *G729StreamDecoder) Close() {
	d.active = false
}

// DecodeG729WithSSRC returns an error because G.729 requires CGO.
func DecodeG729WithSSRC(payload []byte, ssrc uint32) ([]byte, error) {
	return nil, fmt.Errorf("G.729 decoding requires CGO (build with CGO_ENABLED=1)")
}

// decodeG729Packet returns an error because G.729 requires CGO.
func decodeG729Packet(payload []byte) ([]byte, error) {
	return nil, fmt.Errorf("G.729 decoding requires CGO (build with CGO_ENABLED=1)")
}

// generateG729ComfortNoise generates comfort noise (does not require CGO).
func generateG729ComfortNoise(samples int) []byte {
	pcmData := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		noise := int16((i*1103515245 + 12345) & 0x7FFF)
		noise = (noise % 200) - 100
		binary.LittleEndian.PutUint16(pcmData[i*2:], uint16(noise))
	}
	return pcmData
}
