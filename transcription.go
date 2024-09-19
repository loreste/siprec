package main

import (
	"context"
	"io"

	speech "cloud.google.com/go/speech/apiv1"
	"cloud.google.com/go/speech/apiv1/speechpb"
	"github.com/sirupsen/logrus"
)

var (
	speechClient *speech.Client
)

func initSpeechClient() {
	var err error
	speechClient, err = speech.NewClient(context.Background())
	if err != nil {
		log.Fatalf("Failed to create Google Speech client: %v", err)
	}
}

func streamToGoogleSpeech(ctx context.Context, audioStream io.Reader, callUUID string) {
	stream, err := speechClient.StreamingRecognize(ctx)
	if err != nil {
		log.WithError(err).WithField("call_uuid", callUUID).Error("Failed to start Google Speech-to-Text stream")
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
		log.WithError(err).WithField("call_uuid", callUUID).Error("Failed to send streaming config")
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
				log.WithError(err).WithField("call_uuid", callUUID).Error("Failed to read audio stream")
				return
			}

			if err := stream.Send(&speechpb.StreamingRecognizeRequest{
				StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
					AudioContent: buffer[:n],
				},
			}); err != nil {
				log.WithError(err).WithField("call_uuid", callUUID).Error("Failed to send audio content to Google Speech-to-Text")
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
				log.WithError(err).WithField("call_uuid", callUUID).Error("Error receiving streaming response")
				return
			}

			for _, result := range resp.Results {
				for _, alt := range result.Alternatives {
					transcription := alt.Transcript
					log.WithFields(logrus.Fields{"call_uuid": callUUID, "transcription": transcription}).Info("Received transcription")

					// Send transcription to AMQP
					sendTranscriptionToAMQP(transcription, callUUID)
				}
			}
		}
	}()
}
