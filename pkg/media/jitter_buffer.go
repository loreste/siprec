package media

import (
	"container/heap"
	"sync"
	"time"

	"github.com/pion/rtp"
)

// JitterBuffer reorders RTP packets by sequence number and provides
// packet loss concealment for missing packets.
type JitterBuffer struct {
	mu           sync.Mutex
	packets      packetHeap
	maxSize      int           // Maximum packets to buffer
	maxDelay     time.Duration // Maximum time to wait for reordering
	lastEmitted  uint16        // Last sequence number emitted
	hasEmitted   bool          // Whether we've emitted any packet yet
	sampleRate   int           // Audio sample rate for PLC calculation
	codecName    string        // Codec name for PLC calculation
	plcCallback  func(int)     // Callback to insert silence for lost packets
}

// BufferedPacket holds an RTP packet with arrival time
type BufferedPacket struct {
	Packet    *rtp.Packet
	Arrival   time.Time
	PCMData   []byte // Pre-decoded PCM data
	index     int    // heap index
}

// packetHeap implements heap.Interface for sequence-ordered packets
type packetHeap []*BufferedPacket

func (h packetHeap) Len() int { return len(h) }

func (h packetHeap) Less(i, j int) bool {
	// Handle sequence number wraparound
	seqI, seqJ := h[i].Packet.SequenceNumber, h[j].Packet.SequenceNumber
	diff := int16(seqI - seqJ)
	return diff < 0
}

func (h packetHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *packetHeap) Push(x interface{}) {
	n := len(*h)
	item := x.(*BufferedPacket)
	item.index = n
	*h = append(*h, item)
}

func (h *packetHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*h = old[0 : n-1]
	return item
}

// JitterBufferConfig configures the jitter buffer
type JitterBufferConfig struct {
	MaxSize    int           // Maximum packets to buffer (default: 5)
	MaxDelay   time.Duration // Maximum reordering delay (default: 60ms)
	SampleRate int           // Audio sample rate
	CodecName  string        // Codec name
}

// NewJitterBuffer creates a new jitter buffer
func NewJitterBuffer(cfg JitterBufferConfig) *JitterBuffer {
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = 5
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 60 * time.Millisecond
	}
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = 8000
	}

	jb := &JitterBuffer{
		packets:    make(packetHeap, 0, cfg.MaxSize),
		maxSize:    cfg.MaxSize,
		maxDelay:   cfg.MaxDelay,
		sampleRate: cfg.SampleRate,
		codecName:  cfg.CodecName,
	}
	heap.Init(&jb.packets)
	return jb
}

// SetPLCCallback sets the callback for packet loss concealment
func (jb *JitterBuffer) SetPLCCallback(cb func(lostPackets int)) {
	jb.mu.Lock()
	defer jb.mu.Unlock()
	jb.plcCallback = cb
}

// Push adds a packet to the jitter buffer
func (jb *JitterBuffer) Push(pkt *rtp.Packet, pcmData []byte, arrival time.Time) {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	// Check for duplicate
	for _, p := range jb.packets {
		if p.Packet.SequenceNumber == pkt.SequenceNumber {
			return // Duplicate packet
		}
	}

	bp := &BufferedPacket{
		Packet:  pkt,
		Arrival: arrival,
		PCMData: pcmData,
	}
	heap.Push(&jb.packets, bp)

	// If buffer is full, force emit the oldest
	if jb.packets.Len() > jb.maxSize {
		jb.emitOldest()
	}
}

// Pop returns the next packet in sequence order, or nil if not ready
func (jb *JitterBuffer) Pop() *BufferedPacket {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	if jb.packets.Len() == 0 {
		return nil
	}

	// Check if we have the next expected packet or if oldest has waited too long
	oldest := jb.packets[0]
	now := time.Now()

	if !jb.hasEmitted {
		// First packet - emit it
		return jb.emitOldest()
	}

	expectedSeq := jb.lastEmitted + 1
	oldestSeq := oldest.Packet.SequenceNumber

	// If we have the expected packet, emit it
	if oldestSeq == expectedSeq {
		return jb.emitOldest()
	}

	// If oldest packet has waited too long, emit with PLC for gaps
	if now.Sub(oldest.Arrival) >= jb.maxDelay {
		// Calculate lost packets
		if oldestSeq > expectedSeq {
			lost := int(oldestSeq - expectedSeq)
			if lost > 10 {
				lost = 10 // Cap at 10 for DTX handling
			}
			if jb.plcCallback != nil && lost > 0 {
				jb.plcCallback(lost)
			}
		}
		return jb.emitOldest()
	}

	return nil
}

// emitOldest removes and returns the oldest packet (must be called with lock held)
func (jb *JitterBuffer) emitOldest() *BufferedPacket {
	if jb.packets.Len() == 0 {
		return nil
	}
	pkt := heap.Pop(&jb.packets).(*BufferedPacket)
	jb.lastEmitted = pkt.Packet.SequenceNumber
	jb.hasEmitted = true
	return pkt
}

// Flush emits all remaining packets in order
func (jb *JitterBuffer) Flush() []*BufferedPacket {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	result := make([]*BufferedPacket, 0, jb.packets.Len())
	for jb.packets.Len() > 0 {
		pkt := heap.Pop(&jb.packets).(*BufferedPacket)
		result = append(result, pkt)
	}
	return result
}

// Len returns the number of buffered packets
func (jb *JitterBuffer) Len() int {
	jb.mu.Lock()
	defer jb.mu.Unlock()
	return jb.packets.Len()
}

// Clear empties the buffer
func (jb *JitterBuffer) Clear() {
	jb.mu.Lock()
	defer jb.mu.Unlock()
	jb.packets = make(packetHeap, 0, jb.maxSize)
	heap.Init(&jb.packets)
	jb.hasEmitted = false
}
