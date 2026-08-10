//go:build !cgo

package media

import "fmt"

// OpusStreamDecoder is unavailable in non-CGO builds because libopus is the
// standards-compliant decoder used by this project.
type OpusStreamDecoder struct{}

func NewOpusStreamDecoder(sampleRate, channels int) (*OpusStreamDecoder, error) {
	return nil, fmt.Errorf("Opus decoding requires CGO and libopus")
}

func (d *OpusStreamDecoder) Close() {}

func (d *OpusStreamDecoder) Decode(packet []byte) ([]byte, error) {
	return nil, fmt.Errorf("Opus decoding requires CGO and libopus")
}

func (d *OpusStreamDecoder) DecodeFEC(packet []byte, samples int) ([]byte, error) {
	return nil, fmt.Errorf("Opus decoding requires CGO and libopus")
}

func (d *OpusStreamDecoder) Conceal(samples int) ([]byte, error) {
	return nil, fmt.Errorf("Opus decoding requires CGO and libopus")
}

func decodeOpusPacket(payload []byte, codecInfo CodecInfo) ([]byte, error) {
	return nil, fmt.Errorf("Opus decoding requires CGO and libopus")
}
