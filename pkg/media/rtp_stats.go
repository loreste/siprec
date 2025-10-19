package media

import (
	"math"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

type rtpStreamStats struct {
	mu sync.Mutex

	clockRate float64

	baseSeq       uint32
	maxSeq        uint32
	cycles        uint32
	expectedPrior uint32
	receivedPrior uint32

	received uint32

	lastArrival   time.Time
	lastTimestamp uint32
	lastTransit   float64
	jitter        float64

	initialized bool
}

func newRTPStreamStats() *rtpStreamStats {
	return &rtpStreamStats{
		clockRate: 8000, // default telephony clock
	}
}

func (s *rtpStreamStats) SetClockRate(rate int) {
	if rate <= 0 {
		return
	}
	s.mu.Lock()
	s.clockRate = float64(rate)
	s.mu.Unlock()
}

func (s *rtpStreamStats) Update(pkt *rtp.Packet, arrival time.Time) {
	if pkt == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	seq := uint32(pkt.SequenceNumber)

	if !s.initialized {
		s.baseSeq = seq
		s.maxSeq = seq
		s.received = 1
		s.initialized = true
		s.lastArrival = arrival
		s.lastTimestamp = pkt.Timestamp
		s.lastTransit = 0
		s.jitter = 0
		return
	}

	// Detect sequence wrap-around
	if seq < uint32(s.maxSeq&0xFFFF) && (s.maxSeq&0xFFFF)-seq > 0x8000 {
		s.cycles += 1 << 16
	}

	extendedSeq := s.cycles | uint32(seq)
	if extendedSeq > s.maxSeq {
		s.maxSeq = extendedSeq
	}

	s.received++

	// Interarrival jitter per RFC3550 section 6.4.1
	if !s.lastArrival.IsZero() && s.clockRate > 0 {
		arrivalDiff := arrival.Sub(s.lastArrival).Seconds()
		rtpTimestampDiff := int32(pkt.Timestamp - s.lastTimestamp)
		transit := arrivalDiff - float64(rtpTimestampDiff)/s.clockRate
		if transit < 0 {
			transit = -transit
		}
		s.jitter += (transit - s.jitter) / 16
		s.lastTransit = transit
	}

	s.lastArrival = arrival
	s.lastTimestamp = pkt.Timestamp
}

func (s *rtpStreamStats) buildReceptionReport(ssrc uint32) *rtcp.ReceptionReport {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.initialized {
		return nil
	}

	expected := (s.maxSeq - s.baseSeq) + 1
	lost := int64(expected) - int64(s.received)
	if lost < 0 {
		lost = 0
	}

	expectedInterval := expected - s.expectedPrior
	receivedInterval := s.received - s.receivedPrior
	lostInterval := expectedInterval - receivedInterval

	var fraction uint8
	if expectedInterval != 0 && lostInterval > 0 {
		fraction = uint8((lostInterval << 8) / expectedInterval)
	}

	s.expectedPrior = expected
	s.receivedPrior = s.received

	totalLost := uint32(lost)
	if totalLost > 0xFFFFFF {
		totalLost = 0xFFFFFF
	}

	report := &rtcp.ReceptionReport{
		SSRC:               ssrc,
		FractionLost:       fraction,
		TotalLost:          totalLost,
		LastSequenceNumber: s.maxSeq,
		Jitter:             uint32(math.Round(s.jitter * s.clockRate)),
		LastSenderReport:   0,
		Delay:              0,
	}

	return report
}

func (s *rtpStreamStats) Snapshot() (packetLoss float64, jitter float64, totalPackets uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.initialized {
		return 0, 0, 0
	}

	expected := (s.maxSeq - s.baseSeq) + 1
	if expected == 0 {
		expected = 1
	}
	lost := float64(expected-s.received) / float64(expected)
	if lost < 0 {
		lost = 0
	}

	return lost, s.jitter, s.received
}
