package main

import (
	"context"
	"io"
	"os"
	"strings"

	speech "cloud.google.com/go/speech/apiv1"
	"cloud.google.com/go/speech/apiv1/speechpb"
	"github.com/sirupsen/logrus"
)

var (
	speechClient *speech.Client
)

// Initialize the speech-to-text clients based on selected providers
func initSpeechClient() {
	var err error
	if strings.ToLower(os.Getenv("SPEECH_VENDOR")) == "google" {
		speechClient, err = speech.NewClient(context.Background())
		if err != nil {
			logrus.Fatalf("Failed to create Google Speech client: %v", err)
		}
	}
	// Initialize Deepgram client
	if strings.ToLower(os.Getenv("SPEECH_VENDOR")) == "deepgram" {
		err = initDeepgramClient()
		if err != nil {
			logrus.Fatalf("Failed to initialize Deepgram client: %v", err)
		}
	}
	// Initialize OpenAI client
	if strings.ToLower(os.Getenv("SPEECH_VENDOR")) == "openai" {
		err = initOpenAIClient()
		if err != nil {
			logrus.Fatalf("Failed to initialize OpenAI client: %v", err)
		}
	}
}

// streamToGoogleSpeech handles streaming to Google Speech-to-Text
func streamToGoogleSpeech(ctx context.Context, audioStream io.Reader, callUUID string) {
	if speechClient == nil {
		logrus.Error("Google Speech-to-Text client not initialized")
		return
	}

	stream, err := speechClient.StreamingRecognize(ctx)
	if err != nil {
		logrus.WithError(err).WithField("call_uuid", callUUID).Error("Failed to start Google Speech-to-Text stream")
		return
	}

	streamingConfig := &speechpb.StreamingRecognitionConfig{
		Config: &speechpb.RecognitionConfig{
			Encoding:        speechpb.RecognitionConfig_LINEAR16,
			SampleRateHertz: 8000,
			LanguageCode:    "en-US",
		},
		InterimResults: true,
	}

	if err := stream.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{
			StreamingConfig: streamingConfig,
		},
	}); err != nil {
		logrus.WithError(err).WithField("call_uuid", callUUID).Error("Failed to send streaming config")
		return
	}

	go func() {
		buffer := make([]byte, 1024)
		for {
			n, err := audioStream.Read(buffer)
			if err == io.EOF {
				break
			}
			if err != nil {
				logrus.WithError(err).WithField("call_uuid", callUUID).Error("Failed to read audio stream")
				return
			}

			if err := stream.Send(&speechpb.StreamingRecognizeRequest{
				StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
					AudioContent: buffer[:n],
				},
			}); err != nil {
				logrus.WithError(err).WithField("call_uuid", callUUID).Error("Failed to send audio content to Google Speech-to-Text")
				return
			}
		}
		stream.CloseSend()
	}()

	go func() {
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				logrus.WithError(err).WithField("call_uuid", callUUID).Error("Error receiving streaming response")
				return
			}

			for _, result := range resp.Results {
				for _, alt := range result.Alternatives {
					transcription := alt.Transcript
					logrus.WithFields(logrus.Fields{"call_uuid": callUUID, "transcription": transcription}).Info("Received transcription")

					// Send transcription to AMQP
					sendTranscriptionToAMQP(transcription, callUUID)
				}
			}
		}
	}()
}

// streamToSpeechVendor routes the audio stream to the appropriate speech-to-text vendor
func streamToSpeechVendor(ctx context.Context, audioStream io.Reader, callUUID string) error {
	switch strings.ToLower(os.Getenv("SPEECH_VENDOR")) {
	case "google":
		streamToGoogleSpeech(ctx, audioStream, callUUID)
	case "deepgram":
		return streamToDeepgram(ctx, audioStream, callUUID)
	case "openai":
		return streamToOpenAI(ctx, audioStream, callUUID)
	default:
		logrus.Errorf("Unsupported speech-to-text vendor: %s", os.Getenv("SPEECH_VENDOR"))
	}
	return nil
}
