package media

import (
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/stretchr/testify/assert"
)

func TestJitterBuffer_InOrder(t *testing.T) {
	jb := NewJitterBuffer(JitterBufferConfig{
		MaxSize:    5,
		MaxDelay:   50 * time.Millisecond,
		SampleRate: 8000,
	})

	// Push packets in order
	for i := uint16(0); i < 5; i++ {
		pkt := &rtp.Packet{
			Header: rtp.Header{
				SequenceNumber: i,
				Timestamp:      uint32(i * 160),
			},
		}
		jb.Push(pkt, []byte{0, 0}, time.Now())
	}

	// Pop packets - should come out in order
	for i := uint16(0); i < 5; i++ {
		bp := jb.Pop()
		assert.NotNil(t, bp)
		assert.Equal(t, i, bp.Packet.SequenceNumber)
	}

	// Buffer should be empty
	assert.Nil(t, jb.Pop())
}

func TestJitterBuffer_OutOfOrder(t *testing.T) {
	jb := NewJitterBuffer(JitterBufferConfig{
		MaxSize:    5,
		MaxDelay:   100 * time.Millisecond,
		SampleRate: 8000,
	})

	// Push packets out of order: 0, 2, 1, 3
	packets := []uint16{0, 2, 1, 3}
	for _, seq := range packets {
		pkt := &rtp.Packet{
			Header: rtp.Header{
				SequenceNumber: seq,
				Timestamp:      uint32(seq * 160),
			},
		}
		jb.Push(pkt, []byte{0, 0}, time.Now())
	}

	// First packet should be available immediately
	bp := jb.Pop()
	assert.NotNil(t, bp)
	assert.Equal(t, uint16(0), bp.Packet.SequenceNumber)

	// Wait for reordering delay
	time.Sleep(110 * time.Millisecond)

	// Remaining packets should be in order
	expected := []uint16{1, 2, 3}
	for _, exp := range expected {
		bp = jb.Pop()
		assert.NotNil(t, bp)
		assert.Equal(t, exp, bp.Packet.SequenceNumber)
	}
}

func TestJitterBuffer_Duplicate(t *testing.T) {
	jb := NewJitterBuffer(JitterBufferConfig{
		MaxSize:    5,
		MaxDelay:   50 * time.Millisecond,
		SampleRate: 8000,
	})

	pkt := &rtp.Packet{
		Header: rtp.Header{
			SequenceNumber: 1,
			Timestamp:      160,
		},
	}

	// Push same packet twice
	jb.Push(pkt, []byte{0, 0}, time.Now())
	jb.Push(pkt, []byte{0, 0}, time.Now())

	// Should only have one packet
	assert.Equal(t, 1, jb.Len())
}

func TestJitterBuffer_Overflow(t *testing.T) {
	jb := NewJitterBuffer(JitterBufferConfig{
		MaxSize:    3,
		MaxDelay:   50 * time.Millisecond,
		SampleRate: 8000,
	})

	// Push more packets than buffer can hold
	for i := uint16(0); i < 5; i++ {
		pkt := &rtp.Packet{
			Header: rtp.Header{
				SequenceNumber: i,
				Timestamp:      uint32(i * 160),
			},
		}
		jb.Push(pkt, []byte{0, 0}, time.Now())
	}

	// Should have forced emission of oldest packets
	// Buffer should have at most maxSize packets
	assert.LessOrEqual(t, jb.Len(), 3)
}

func TestJitterBuffer_PLCCallback(t *testing.T) {
	jb := NewJitterBuffer(JitterBufferConfig{
		MaxSize:    5,
		MaxDelay:   50 * time.Millisecond,
		SampleRate: 8000,
	})

	lostCount := 0
	jb.SetPLCCallback(func(lost int) {
		lostCount = lost
	})

	// Push packet 0
	pkt0 := &rtp.Packet{
		Header: rtp.Header{
			SequenceNumber: 0,
			Timestamp:      0,
		},
	}
	jb.Push(pkt0, []byte{0, 0}, time.Now())
	jb.Pop() // Emit first packet

	// Push packet 3 (gap of 2)
	pkt3 := &rtp.Packet{
		Header: rtp.Header{
			SequenceNumber: 3,
			Timestamp:      480,
		},
	}
	jb.Push(pkt3, []byte{0, 0}, time.Now())

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)
	jb.Pop()

	// PLC callback should have been called with 2 lost packets
	assert.Equal(t, 2, lostCount)
}
