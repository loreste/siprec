//go:build cgo

package media

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"testing"

	libopus "github.com/pidato/audio/opus"
)

func TestOpusRejectsSynthesizedGarbage(t *testing.T) {
	_, err := DecodeAudioPayload([]byte{0xff, 0xff}, "OPUS")
	if err == nil {
		t.Fatal("malformed Opus packet must be rejected")
	}
}

func TestOpusRejectsEmptyPacket(t *testing.T) {
	if _, err := DecodeAudioPayload(nil, "OPUS_MONO"); err == nil {
		t.Fatal("empty Opus packet must be rejected")
	}
}

func TestOpusLibopusVector(t *testing.T) {
	vectors := []struct {
		name       string
		channels   int
		packetHex  string
		pcmHashHex string
	}{
		{
			name:       "mono SILK",
			channels:   1,
			packetHex:  "08822c9e0c4b960901a71ad3d594ea9330c148f659280b470d9fc01fbc0fb6e91a899c8d3ad61942ee09779611b5fcdb7b7d037ef881d62327ad7d92aa3d69790309089f304836f45c757724f3a483",
			pcmHashHex: "f5e580c10f140d062fc5567770888c2cc97b4a8bd73e13bded8e9c08f4bdd796",
		},
		{
			name:       "mono hybrid",
			channels:   1,
			packetHex:  "78833e1939f210a2d5a8b69240e057b8b366cea2d73f7da797b9e3a83f48ae8b535eae388a2c2c6f42193229b463341589248c758838f1c44ae5f2e62cdfe66dcd1fd86ef22d1388d9583f83f5610984998550ae649468d93da9589681955a9d6ff5020167b2524f1ff988359e2f48344d7e8081343fc05754d2eebd45a415b69e16495b86fcc5cb9af29c5e76",
			pcmHashHex: "308e02a09079890599a08143894a980710d76983a73a9cebb4e5b4000c354eff",
		},
		{
			name:       "stereo CELT",
			channels:   2,
			packetHex:  "fc9eaf943c83d945f71e0e6f6facec062b7c5ceec9ce12b16ba4e2c3314498efccf6333db18ae6f834fa5db1d5fa738583513fac5aa139d43701b89283eb171f324531d0f0c4fe56d1c4958da332d5273e6d63af78d945f0f4efb45beaa41b409cfc24aabc84c9251ed039d133c0e925f770f2ed6cb7821d9268937504949c7cab8f84932f889a8c8c1e8da5c34fb000632d50cee6d8aaacb6a61dca134ed402d50ea6bcce9349d3ccca7012d4a832126c135fd30776ab92461175386c83bf12d6dbd520d29e1122b745af5dc00001f86061ff99531c2db8d092132385d9ea5a59b1f04cf8429a4f062bc4db4f2582c5d4396fb82bc41a81dde54e289fa21c64bb3a6fcec8833fc1110e7d1baefe28873e9391e2b15f1a6af54894bb3e5a1d40240701c86678fe2ff482bd81180e4ec551fc70b293c125281855224308a208e953e45e642ae01057f544f20589a134d685233fdb009b76000915b492fdbdbd6abfa6",
			pcmHashHex: "f47e4a74ead0bbe32929d28a6036575648f42fad06cdb7c55851b40163287a80",
		},
	}

	for _, vector := range vectors {
		t.Run(vector.name, func(t *testing.T) {
			packet, err := hex.DecodeString(vector.packetHex)
			if err != nil {
				t.Fatalf("invalid embedded vector: %v", err)
			}
			pcm, err := DecodeAudioPayload(packet, map[int]string{1: "OPUS_MONO", 2: "OPUS"}[vector.channels])
			if err != nil {
				t.Fatalf("DecodeAudioPayload: %v", err)
			}
			hash := sha256.Sum256(pcm)
			if got := hex.EncodeToString(hash[:]); got != vector.pcmHashHex {
				t.Fatalf("PCM hash mismatch: got %s, want %s", got, vector.pcmHashHex)
			}
		})
	}
}

func TestOpusFECAndPLC(t *testing.T) {
	encoder, err := libopus.NewEncoder(48000, 1, libopus.AppVoIP)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	if err := encoder.SetInBandFEC(true); err != nil {
		t.Fatalf("SetInBandFEC: %v", err)
	}
	if err := encoder.SetPacketLossPerc(20); err != nil {
		t.Fatalf("SetPacketLossPerc: %v", err)
	}

	first := encodeOpusTestFrameWithEncoder(t, encoder, 1, 0)
	second := encodeOpusTestFrameWithEncoder(t, encoder, 1, 1)
	decoder, err := NewOpusStreamDecoder(48000, 1)
	if err != nil {
		t.Fatalf("NewOpusStreamDecoder: %v", err)
	}
	defer decoder.Close()
	if _, err := decoder.Decode(first); err != nil {
		t.Fatalf("Decode first packet: %v", err)
	}
	if _, err := decoder.DecodeFEC(second, 960); err != nil {
		t.Fatalf("DecodeFEC: %v", err)
	}
	if pcm, err := decoder.Conceal(960); err != nil || len(pcm) == 0 {
		t.Fatalf("Conceal: bytes=%d err=%v", len(pcm), err)
	}
}

func encodeOpusTestFrameWithEncoder(t *testing.T, encoder *libopus.Encoder, channels, frame int) []byte {
	t.Helper()
	pcm := make([]int16, 960*channels)
	for i := 0; i < 960; i++ {
		sample := int16(math.Sin(float64(i+frame*31)*2*math.Pi/37.0) * 12000)
		for ch := 0; ch < channels; ch++ {
			pcm[i*channels+ch] = sample
		}
	}
	encoded := make([]byte, 1500)
	n, err := encoder.Encode(pcm, encoded)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return append([]byte(nil), encoded[:n]...)
}
