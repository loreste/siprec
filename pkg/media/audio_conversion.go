package media

import (
	"fmt"
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
