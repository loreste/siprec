//go:build cgo

package media

/*
#cgo pkg-config: opus
#include <opus.h>
*/
import "C"

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"unsafe"
)

const maxOpusSamplesPerChannel = 5760 // 120 ms at 48 kHz, RFC 6716 maximum

// OpusStreamDecoder is a stateful wrapper around the reference libopus
// decoder. It must be used by one RTP stream goroutine at a time.
type OpusStreamDecoder struct {
	decoder    *C.OpusDecoder
	sampleRate int
	channels   int
}

// NewOpusStreamDecoder creates a libopus decoder for a supported RTP format.
func NewOpusStreamDecoder(sampleRate, channels int) (*OpusStreamDecoder, error) {
	if !isValidOpusSampleRate(sampleRate) {
		return nil, fmt.Errorf("unsupported Opus sample rate: %d", sampleRate)
	}
	if channels != 1 && channels != 2 {
		return nil, fmt.Errorf("unsupported Opus channel count: %d", channels)
	}

	var opusErr C.int
	decoder := C.opus_decoder_create(C.opus_int32(sampleRate), C.int(channels), &opusErr)
	if decoder == nil || opusErr != C.OPUS_OK {
		return nil, fmt.Errorf("libopus decoder initialization failed: %s", C.GoString(C.opus_strerror(opusErr)))
	}

	result := &OpusStreamDecoder{decoder: decoder, sampleRate: sampleRate, channels: channels}
	runtime.SetFinalizer(result, (*OpusStreamDecoder).Close)
	return result, nil
}

// Close releases libopus state.
func (d *OpusStreamDecoder) Close() {
	if d == nil || d.decoder == nil {
		return
	}
	C.opus_decoder_destroy(d.decoder)
	d.decoder = nil
	runtime.SetFinalizer(d, nil)
}

// Decode decodes one complete Opus packet, preserving SILK/CELT state across
// calls and rejecting malformed packets through libopus.
func (d *OpusStreamDecoder) Decode(packet []byte) ([]byte, error) {
	return d.decode(packet, maxOpusSamplesPerChannel, false)
}

// DecodeFEC decodes in-band forward error correction from the packet after a
// missing packet. samples is the number of samples per channel that were lost.
func (d *OpusStreamDecoder) DecodeFEC(packet []byte, samples int) ([]byte, error) {
	if samples <= 0 || samples > maxOpusSamplesPerChannel {
		return nil, fmt.Errorf("invalid Opus FEC sample count: %d", samples)
	}
	return d.decode(packet, samples, true)
}

// Conceal synthesizes packet-loss concealment for the requested duration.
func (d *OpusStreamDecoder) Conceal(samples int) ([]byte, error) {
	if samples <= 0 || samples > maxOpusSamplesPerChannel {
		return nil, fmt.Errorf("invalid Opus PLC sample count: %d", samples)
	}
	return d.decode(nil, samples, false)
}

func (d *OpusStreamDecoder) decode(packet []byte, samples int, fec bool) ([]byte, error) {
	if d == nil || d.decoder == nil {
		return nil, fmt.Errorf("Opus decoder is closed")
	}
	pcm := make([]int16, samples*d.channels)
	var data *C.uchar
	if len(packet) > 0 {
		data = (*C.uchar)(unsafe.Pointer(&packet[0]))
	}
	fecFlag := C.int(0)
	if fec {
		fecFlag = 1
	}

	decoded := C.opus_decode(
		d.decoder,
		data,
		C.opus_int32(len(packet)),
		(*C.opus_int16)(unsafe.Pointer(&pcm[0])),
		C.int(samples),
		fecFlag,
	)
	if decoded < 0 {
		return nil, fmt.Errorf("libopus decode failed: %s", C.GoString(C.opus_strerror(decoded)))
	}

	pcm = pcm[:int(decoded)*d.channels]
	result := make([]byte, len(pcm)*2)
	for i, sample := range pcm {
		binary.LittleEndian.PutUint16(result[i*2:], uint16(sample))
	}
	return result, nil
}

func decodeOpusPacket(payload []byte, codecInfo CodecInfo) ([]byte, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("empty Opus payload")
	}
	decoder, err := NewOpusStreamDecoder(codecInfo.SampleRate, codecInfo.Channels)
	if err != nil {
		return nil, err
	}
	defer decoder.Close()
	return decoder.Decode(payload)
}

func isValidOpusSampleRate(sampleRate int) bool {
	switch sampleRate {
	case 8000, 12000, 16000, 24000, 48000:
		return true
	default:
		return false
	}
}
