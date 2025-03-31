package media

import (
	"fmt"
	"io"
	"os"
	
	"github.com/sirupsen/logrus"
)

// DetectCodec identifies the codec from an RTP packet
func DetectCodec(rtpPacket []byte) (string, error) {
	if len(rtpPacket) < 12 {
		return "", fmt.Errorf("RTP packet too short")
	}

	// Extract payload type from RTP header
	payloadType := rtpPacket[1] & 0x7F

	switch payloadType {
	case 0:
		return "PCMU", nil // G.711 μ-law
	case 8:
		return "PCMA", nil // G.711 a-law
	case 9:
		return "G722", nil // G.722
	default:
		return "unknown", fmt.Errorf("unsupported payload type: %d", payloadType)
	}
}

// ProcessAudioPacket extracts and processes audio data based on codec
func ProcessAudioPacket(data []byte, payloadType byte) ([]byte, error) {
	// Extract the RTP payload
	if len(data) < 12 {
		return nil, fmt.Errorf("RTP packet too short")
	}

	payload := data[12:]

	// Process based on payload type
	switch payloadType {
	case 0: // PCMU (G.711 μ-law)
		// G.711 μ-law is already in a format that can be written to WAV
		return payload, nil
	case 8: // PCMA (G.711 a-law)
		// G.711 a-law is already in a format that can be written to WAV
		return payload, nil
	case 9: // G.722
		// G.722 processing might require special handling
		// Depending on your needs, you might need to convert it
		return payload, nil
	default:
		return nil, fmt.Errorf("unsupported payload type: %d", payloadType)
	}
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