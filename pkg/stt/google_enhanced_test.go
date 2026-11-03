package stt

import (
	speechpb "cloud.google.com/go/speech/apiv1/speechpb"
	"context"
	"errors"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"io"
	"testing"
	"time"
)

type googleTestStream struct {
	grpc.ClientStream
	recv      func() (*speechpb.StreamingRecognizeResponse, error)
	send      func(*speechpb.StreamingRecognizeRequest) error
	closeSend func() error
}

func (s *googleTestStream) Recv() (*speechpb.StreamingRecognizeResponse, error) { return s.recv() }
func (s *googleTestStream) Send(r *speechpb.StreamingRecognizeRequest) error    { return s.send(r) }
func (s *googleTestStream) CloseSend() error                                    { return s.closeSend() }
func googleTestConnection(t *testing.T) *GoogleConnection {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	l := logrus.New()
	l.SetOutput(io.Discard)
	return &GoogleConnection{ctx: ctx, cancel: cancel, logger: l.WithField("test", true), config: &GoogleConfig{BufferSize: 16, RequestTimeout: 10 * time.Millisecond}, audioChan: make(chan []byte, 2), audioSent: make(chan struct{}), resultsDone: make(chan error, 1)}
}
func TestGoogleLiveStreamSurvivesRequestTimeout(t *testing.T) {
	c := googleTestConnection(t)
	done := make(chan error, 1)
	go func() { done <- c.waitForCompletion(c.ctx) }()
	select {
	case err := <-done:
		t.Fatalf("live stream ended: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	c.finish(nil)
	require.NoError(t, <-done)
}
func TestGoogleFinalResponseTimeout(t *testing.T) {
	c := googleTestConnection(t)
	close(c.audioSent)
	require.ErrorIs(t, c.waitForCompletion(c.ctx), context.DeadlineExceeded)
}
func TestGoogleReceiveErrorPropagates(t *testing.T) {
	c := googleTestConnection(t)
	expected := errors.New("receive failed")
	c.stream = &googleTestStream{recv: func() (*speechpb.StreamingRecognizeResponse, error) { return nil, expected }}
	go c.handleResults(nil)
	require.ErrorIs(t, c.waitForCompletion(context.Background()), expected)
}

type googleEOFReader struct{}

func (googleEOFReader) Read(b []byte) (int, error) { return copy(b, []byte{1, 2, 3, 4}), io.EOF }
func TestGoogleSendsFinalBytesBeforeHalfClose(t *testing.T) {
	c := googleTestConnection(t)
	closed := make(chan struct{})
	var audio []byte
	c.stream = &googleTestStream{
		send: func(r *speechpb.StreamingRecognizeRequest) error {
			audio = append(audio, r.GetAudioContent()...)
			return nil
		},
		closeSend: func() error { close(closed); return nil },
		recv:      func() (*speechpb.StreamingRecognizeResponse, error) { <-closed; return nil, io.EOF },
	}
	go c.handleResults(nil)
	go c.handleAudioStream(c.ctx, googleEOFReader{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, c.waitForCompletion(ctx))
	require.Equal(t, []byte{1, 2, 3, 4}, audio)
}

func TestGoogleSendErrorPropagates(t *testing.T) {
	c := googleTestConnection(t)
	expected := errors.New("send failed")
	c.stream = &googleTestStream{
		send: func(*speechpb.StreamingRecognizeRequest) error { return expected },
		recv: func() (*speechpb.StreamingRecognizeResponse, error) { <-c.ctx.Done(); return nil, c.ctx.Err() },
	}
	c.audioChan <- []byte{1, 2}
	go c.handleResults(nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.ErrorIs(t, c.waitForCompletion(ctx), expected)
}

type googleErrorReader struct{ err error }

func (r googleErrorReader) Read([]byte) (int, error) { return 0, r.err }
func TestGoogleAudioReadErrorPropagates(t *testing.T) {
	c := googleTestConnection(t)
	expected := errors.New("audio source failed")
	c.handleAudioStream(c.ctx, googleErrorReader{expected})
	require.ErrorIs(t, c.waitForCompletion(context.Background()), expected)
}
func TestGoogleCleanupCancelsWorkers(t *testing.T) {
	c := googleTestConnection(t)
	c.active = true
	c.close()
	require.ErrorIs(t, c.ctx.Err(), context.Canceled)
}
