package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/pion/srtp/v2"
)

type RTPForwarder struct {
	localPort      int
	conn           *net.UDPConn
	stopChan       chan struct{}
	transcriptChan chan string
	recordingFile  *os.File // Used to store the recorded media stream
}

var (
	portMutex    sync.Mutex
	usedRTPPorts = make(map[int]bool) // Keeps track of used RTP ports
)

func startRTPForwarding(ctx context.Context, forwarder *RTPForwarder, callUUID string) {
	go func() {
		var err error
		udpConn, err := net.ListenUDP("udp", &net.UDPAddr{Port: forwarder.localPort})
		if err != nil {
			log.WithError(err).WithField("port", forwarder.localPort).Error("Failed to listen on UDP port for RTP forwarding")
			return
		}
		defer udpConn.Close()
		forwarder.conn = udpConn

		var srtpSession *srtp.SessionSRTP
		if config.EnableSRTP {
			// Set up SRTP with a shared key
			keyingMaterial := make([]byte, 30) // 16 bytes for master key, 14 bytes for salt
			if _, err := rand.Read(keyingMaterial); err != nil {
				log.WithError(err).WithField("call_uuid", callUUID).Error("Failed to generate SRTP keying material")
				return
			}

			// Create SRTP session
			srtpConfig := &srtp.Config{
				Profile: srtp.ProtectionProfileAes128CmHmacSha1_80,
			}
			copy(srtpConfig.Keys.LocalMasterKey[:], keyingMaterial[:16])
			copy(srtpConfig.Keys.LocalMasterSalt[:], keyingMaterial[16:])

			srtpSession, err = srtp.NewSessionSRTP(udpConn, srtpConfig)
			if err != nil {
				log.WithError(err).WithField("call_uuid", callUUID).Error("Failed to set up SRTP session")
				return
			}
		}

		pr, pw := io.Pipe()
		audioStream := pr

		go startRecording(audioStream, callUUID)
		go streamToGoogleSpeech(ctx, audioStream, callUUID)

		buffer := make([]byte, 1500)
		for {
			select {
			case <-forwarder.stopChan:
				pw.Close()
				return
			case <-ctx.Done():
				pw.Close()
				return
			default:
				n, _, err := forwarder.conn.ReadFrom(buffer)
				if err != nil {
					log.WithError(err).WithField("call_uuid", callUUID).Error("Failed to read RTP packet")
					continue
				}

				if config.EnableSRTP && srtpSession != nil {
					// Create a read stream for SRTP
					readStream, err := srtpSession.OpenReadStream(0) // Use SSRC 0 for now, adjust if needed
					if err != nil {
						log.WithError(err).WithField("call_uuid", callUUID).Error("Failed to open SRTP read stream")
						continue
					}

					// Read and decrypt the SRTP packet
					decryptedRTP := make([]byte, 1500)
					n, err = readStream.Read(decryptedRTP)
					if err != nil {
						log.WithError(err).WithField("call_uuid", callUUID).Error("Failed to decrypt SRTP packet")
						continue
					}

					// Write the decrypted packet to the pipe
					if _, err := pw.Write(decryptedRTP[:n]); err != nil {
						log.WithError(err).WithField("call_uuid", callUUID).Error("Failed to write decrypted RTP packet to audio stream")
					}
				} else {
					// Process RTP packet without decryption
					if _, err := pw.Write(buffer[:n]); err != nil {
						log.WithError(err).WithField("call_uuid", callUUID).Error("Failed to write RTP packet to audio stream")
					}
				}
			}
		}
	}()
}

func allocateRTPPort() int {
	portMutex.Lock()
	defer portMutex.Unlock()

	for {
		// Use crypto/rand for secure port allocation
		port, err := rand.Int(rand.Reader, big.NewInt(int64(config.RTPPortMax-config.RTPPortMin)))
		if err != nil {
			log.Fatal("Failed to generate random port")
		}
		rtpPort := int(port.Int64()) + config.RTPPortMin
		if !usedRTPPorts[rtpPort] {
			usedRTPPorts[rtpPort] = true
			return rtpPort
		}
	}
}

func releaseRTPPort(port int) {
	portMutex.Lock()
	defer portMutex.Unlock()
	delete(usedRTPPorts, port)
}

func startRecording(audioStream io.Reader, callUUID string) {
	filePath := filepath.Join(config.RecordingDir, fmt.Sprintf("%s.wav", callUUID))
	file, err := os.Create(filePath)
	if err != nil {
		log.WithError(err).WithField("call_uuid", callUUID).Error("Failed to create recording file")
		return
	}
	defer file.Close()

	_, err = io.Copy(file, audioStream)
	if err != nil {
		log.WithError(err).WithField("call_uuid", callUUID).Error("Failed to write audio stream to file")
	}
}
