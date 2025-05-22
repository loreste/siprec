package media

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// QualityMetrics tracks real-time audio quality metrics
type QualityMetrics struct {
	// Packet-level metrics
	PacketsReceived   uint64
	PacketsLost       uint64
	PacketsOutOfOrder uint64
	PacketsDuplicated uint64

	// Timing metrics
	Jitter        time.Duration
	MaxJitter     time.Duration
	MinJitter     time.Duration
	AverageJitter time.Duration

	// Delay metrics
	RoundTripTime time.Duration
	MaxRTT        time.Duration
	MinRTT        time.Duration
	AverageRTT    time.Duration

	// Quality scores
	MOSScore float64 // Mean Opinion Score (1.0-5.0)
	RFactor  float64 // R-Factor for MOS calculation

	// Codec-specific metrics
	CodecType  string
	SampleRate int
	BitRate    int

	// Internal tracking
	lastSequenceNumber uint16
	lastTimestamp      uint32
	lastPacketTime     time.Time
	jitterSamples      []time.Duration
	rttSamples         []time.Duration

	// Thread safety
	mutex sync.RWMutex

	// Statistics window
	maxSamples int

	// Quality assessment
	qualityAssessment *QualityAssessment
}

// QualityAssessment provides detailed quality analysis
type QualityAssessment struct {
	OverallRating   string   // Excellent, Good, Fair, Poor, Bad
	Issues          []string // List of detected quality issues
	Recommendations []string // Recommendations for improvement
	Confidence      float64  // Confidence in the assessment (0.0-1.0)
	LastUpdated     time.Time
}

// NewQualityMetrics creates a new quality metrics tracker
func NewQualityMetrics(codecType string, sampleRate int) *QualityMetrics {
	return &QualityMetrics{
		CodecType:         codecType,
		SampleRate:        sampleRate,
		maxSamples:        100, // Keep last 100 samples for running averages
		jitterSamples:     make([]time.Duration, 0, 100),
		rttSamples:        make([]time.Duration, 0, 100),
		qualityAssessment: &QualityAssessment{},
		MinJitter:         time.Duration(math.MaxInt64),
		MinRTT:            time.Duration(math.MaxInt64),
	}
}

// ProcessRTPPacket processes an incoming RTP packet and updates metrics
func (qm *QualityMetrics) ProcessRTPPacket(packet []byte, receiveTime time.Time) error {
	qm.mutex.Lock()
	defer qm.mutex.Unlock()

	if len(packet) < 12 {
		return fmt.Errorf("invalid RTP packet size")
	}

	// Extract RTP header fields
	sequenceNumber := uint16(packet[2])<<8 | uint16(packet[3])
	timestamp := uint32(packet[4])<<24 | uint32(packet[5])<<16 | uint32(packet[6])<<8 | uint32(packet[7])

	qm.PacketsReceived++

	// Detect packet loss and out-of-order packets
	if qm.PacketsReceived > 1 {
		expectedSeq := qm.lastSequenceNumber + 1

		if sequenceNumber < qm.lastSequenceNumber {
			// Potential out-of-order or duplicate packet
			seqDiff := qm.lastSequenceNumber - sequenceNumber
			if seqDiff < 1000 { // Reasonable threshold for out-of-order
				qm.PacketsOutOfOrder++
			} else {
				// Likely sequence number wraparound with loss
				qm.PacketsLost += uint64(65536 - int(seqDiff) - 1)
			}
		} else if sequenceNumber == qm.lastSequenceNumber {
			// Duplicate packet
			qm.PacketsDuplicated++
		} else if sequenceNumber > expectedSeq {
			// Packet loss detected
			qm.PacketsLost += uint64(sequenceNumber - expectedSeq)
		}
	}

	// Calculate jitter
	if !qm.lastPacketTime.IsZero() && qm.PacketsReceived > 1 {
		// Interarrival jitter calculation (RFC 3550)
		transitTime := receiveTime.Sub(qm.lastPacketTime)
		timestampDiff := time.Duration(timestamp-qm.lastTimestamp) * time.Second / time.Duration(qm.SampleRate)

		jitter := transitTime - timestampDiff
		if jitter < 0 {
			jitter = -jitter
		}

		qm.updateJitter(jitter)
	}

	// Update tracking variables
	qm.lastSequenceNumber = sequenceNumber
	qm.lastTimestamp = timestamp
	qm.lastPacketTime = receiveTime

	// Update quality scores
	qm.calculateMOS()
	qm.updateQualityAssessment()

	return nil
}

// updateJitter updates jitter statistics
func (qm *QualityMetrics) updateJitter(currentJitter time.Duration) {
	// Add to samples
	qm.jitterSamples = append(qm.jitterSamples, currentJitter)
	if len(qm.jitterSamples) > qm.maxSamples {
		qm.jitterSamples = qm.jitterSamples[1:]
	}

	// Update current jitter (RFC 3550 algorithm)
	if qm.Jitter == 0 {
		qm.Jitter = currentJitter
	} else {
		// J(i) = J(i-1) + (|D(i-1,i)| - J(i-1))/16
		qm.Jitter = qm.Jitter + (currentJitter-qm.Jitter)/16
	}

	// Update min/max
	if currentJitter < qm.MinJitter {
		qm.MinJitter = currentJitter
	}
	if currentJitter > qm.MaxJitter {
		qm.MaxJitter = currentJitter
	}

	// Calculate average
	var total time.Duration
	for _, sample := range qm.jitterSamples {
		total += sample
	}
	qm.AverageJitter = total / time.Duration(len(qm.jitterSamples))
}

// ProcessRTCPPacket processes RTCP packets for RTT calculation
func (qm *QualityMetrics) ProcessRTCPPacket(packet []byte, receiveTime time.Time) error {
	qm.mutex.Lock()
	defer qm.mutex.Unlock()

	if len(packet) < 8 {
		return fmt.Errorf("invalid RTCP packet size")
	}

	// Parse RTCP packet type
	packetType := packet[1]

	switch packetType {
	case 200: // Sender Report (SR)
		qm.processSenderReport(packet, receiveTime)
	case 201: // Receiver Report (RR)
		qm.processReceiverReport(packet, receiveTime)
	}

	return nil
}

// processSenderReport processes RTCP Sender Report for RTT calculation
func (qm *QualityMetrics) processSenderReport(packet []byte, receiveTime time.Time) {
	if len(packet) < 28 {
		return
	}

	// Extract NTP timestamp (bytes 8-15)
	ntpSec := uint32(packet[8])<<24 | uint32(packet[9])<<16 | uint32(packet[10])<<8 | uint32(packet[11])
	ntpFrac := uint32(packet[12])<<24 | uint32(packet[13])<<16 | uint32(packet[14])<<8 | uint32(packet[15])

	// Convert NTP to time
	ntpTime := time.Unix(int64(ntpSec-2208988800), int64(ntpFrac)*1000000000/4294967296) // NTP epoch offset

	// Calculate RTT (simplified - in practice you'd track LSR and DLSR)
	rtt := receiveTime.Sub(ntpTime)
	if rtt > 0 && rtt < time.Second*10 { // Sanity check
		qm.updateRTT(rtt)
	}
}

// processReceiverReport processes RTCP Receiver Report
func (qm *QualityMetrics) processReceiverReport(packet []byte, receiveTime time.Time) {
	if len(packet) < 32 {
		return
	}

	// Extract loss statistics from RR
	lostPackets := uint32(packet[12])<<16 | uint32(packet[13])<<8 | uint32(packet[14])
	lostPackets &= 0xFFFFFF // 24-bit field

	// Update loss statistics
	if lostPackets > uint32(qm.PacketsLost) {
		qm.PacketsLost = uint64(lostPackets)
	}
}

// updateRTT updates RTT statistics
func (qm *QualityMetrics) updateRTT(currentRTT time.Duration) {
	qm.rttSamples = append(qm.rttSamples, currentRTT)
	if len(qm.rttSamples) > qm.maxSamples {
		qm.rttSamples = qm.rttSamples[1:]
	}

	qm.RoundTripTime = currentRTT

	if currentRTT < qm.MinRTT {
		qm.MinRTT = currentRTT
	}
	if currentRTT > qm.MaxRTT {
		qm.MaxRTT = currentRTT
	}

	// Calculate average RTT
	var total time.Duration
	for _, sample := range qm.rttSamples {
		total += sample
	}
	qm.AverageRTT = total / time.Duration(len(qm.rttSamples))
}

// calculateMOS calculates the Mean Opinion Score based on current metrics
func (qm *QualityMetrics) calculateMOS() {
	// Calculate R-Factor first
	qm.calculateRFactor()

	// Convert R-Factor to MOS using ITU-T G.107 model
	if qm.RFactor < 0 {
		qm.MOSScore = 1.0
	} else if qm.RFactor > 100 {
		qm.MOSScore = 5.0
	} else {
		// MOS = 1 + 0.035*R + 7*10^-6*R*(R-60)*(100-R)
		r := qm.RFactor
		qm.MOSScore = 1.0 + 0.035*r + 0.000007*r*(r-60)*(100-r)

		// Clamp to valid MOS range
		if qm.MOSScore < 1.0 {
			qm.MOSScore = 1.0
		} else if qm.MOSScore > 5.0 {
			qm.MOSScore = 5.0
		}
	}
}

// calculateRFactor calculates the R-Factor for quality assessment
func (qm *QualityMetrics) calculateRFactor() {
	// ITU-T G.107 E-model calculation
	// R = Ro - Is - Id - Ie + A

	// Ro: Basic signal-to-noise ratio (codec dependent)
	ro := qm.getCodecRo()

	// Is: Simultaneous impairment (not applicable for recording)
	is := 0.0

	// Id: Delay impairment
	id := qm.calculateDelayImpairment()

	// Ie: Equipment impairment (codec dependent)
	ie := qm.getCodecIe()

	// A: Advantage factor (not applicable)
	a := 0.0

	qm.RFactor = ro - is - id - ie + a
}

// getCodecRo returns the basic signal-to-noise ratio for the codec
func (qm *QualityMetrics) getCodecRo() float64 {
	switch qm.CodecType {
	case "PCMU", "PCMA": // G.711
		return 94.2
	case "G722":
		return 94.2
	case "OPUS":
		return 94.2
	case "EVS":
		return 94.2
	default:
		return 90.0 // Conservative estimate
	}
}

// getCodecIe returns the equipment impairment for the codec
func (qm *QualityMetrics) getCodecIe() float64 {
	// Calculate packet loss impairment
	lossRate := qm.GetPacketLossRate()

	// Base equipment impairment by codec
	var baseIe float64
	switch qm.CodecType {
	case "PCMU", "PCMA": // G.711
		baseIe = 0.0 // Reference codec
	case "G722":
		baseIe = 1.0
	case "OPUS":
		baseIe = 1.0
	case "EVS":
		baseIe = 0.5
	default:
		baseIe = 5.0 // Unknown codec penalty
	}

	// Add packet loss impairment (Bpl factor)
	var bpl float64
	switch qm.CodecType {
	case "PCMU", "PCMA":
		bpl = 4.3
	case "G722":
		bpl = 10.0
	case "OPUS":
		bpl = 20.0 // Opus is more robust to packet loss
	case "EVS":
		bpl = 15.0
	default:
		bpl = 10.0
	}

	lossImpairment := bpl * math.Log(1+15*lossRate)

	return baseIe + lossImpairment
}

// calculateDelayImpairment calculates impairment due to delay and jitter
func (qm *QualityMetrics) calculateDelayImpairment() float64 {
	// Convert RTT and jitter to milliseconds
	rttMs := float64(qm.AverageRTT.Nanoseconds()) / 1000000.0
	jitterMs := float64(qm.AverageJitter.Nanoseconds()) / 1000000.0

	// One-way delay estimation (RTT/2)
	oneWayDelay := rttMs / 2.0

	// ITU-T G.107 delay impairment calculation
	var id float64
	if oneWayDelay < 100 {
		id = 0.0
	} else {
		id = 0.024*oneWayDelay + 0.11*(oneWayDelay-177.3)*math.Max(0, oneWayDelay-177.3)
	}

	// Add jitter impairment
	jitterImpairment := jitterMs * 0.1 // Simplified jitter penalty

	return id + jitterImpairment
}

// GetPacketLossRate returns the current packet loss rate
func (qm *QualityMetrics) GetPacketLossRate() float64 {
	qm.mutex.RLock()
	defer qm.mutex.RUnlock()

	if qm.PacketsReceived == 0 {
		return 0.0
	}

	totalExpected := qm.PacketsReceived + qm.PacketsLost
	return float64(qm.PacketsLost) / float64(totalExpected)
}

// GetJitterMs returns the current jitter in milliseconds
func (qm *QualityMetrics) GetJitterMs() float64 {
	qm.mutex.RLock()
	defer qm.mutex.RUnlock()

	return float64(qm.Jitter.Nanoseconds()) / 1000000.0
}

// GetRTTMs returns the current RTT in milliseconds
func (qm *QualityMetrics) GetRTTMs() float64 {
	qm.mutex.RLock()
	defer qm.mutex.RUnlock()

	return float64(qm.RoundTripTime.Nanoseconds()) / 1000000.0
}

// updateQualityAssessment updates the overall quality assessment
func (qm *QualityMetrics) updateQualityAssessment() {
	assessment := &QualityAssessment{
		LastUpdated:     time.Now(),
		Issues:          make([]string, 0),
		Recommendations: make([]string, 0),
	}

	// Determine overall rating based on MOS score
	switch {
	case qm.MOSScore >= 4.5:
		assessment.OverallRating = "Excellent"
		assessment.Confidence = 0.95
	case qm.MOSScore >= 4.0:
		assessment.OverallRating = "Good"
		assessment.Confidence = 0.90
	case qm.MOSScore >= 3.5:
		assessment.OverallRating = "Fair"
		assessment.Confidence = 0.85
	case qm.MOSScore >= 2.5:
		assessment.OverallRating = "Poor"
		assessment.Confidence = 0.80
	default:
		assessment.OverallRating = "Bad"
		assessment.Confidence = 0.90
	}

	// Identify specific issues
	lossRate := qm.GetPacketLossRate()
	jitterMs := qm.GetJitterMs()
	rttMs := qm.GetRTTMs()

	if lossRate > 0.05 { // 5% loss threshold
		assessment.Issues = append(assessment.Issues, fmt.Sprintf("High packet loss: %.1f%%", lossRate*100))
		assessment.Recommendations = append(assessment.Recommendations, "Check network stability and bandwidth")
	}

	if jitterMs > 50 { // 50ms jitter threshold
		assessment.Issues = append(assessment.Issues, fmt.Sprintf("High jitter: %.1fms", jitterMs))
		assessment.Recommendations = append(assessment.Recommendations, "Implement jitter buffer or QoS prioritization")
	}

	if rttMs > 300 { // 300ms RTT threshold
		assessment.Issues = append(assessment.Issues, fmt.Sprintf("High latency: %.1fms", rttMs))
		assessment.Recommendations = append(assessment.Recommendations, "Optimize network routing or reduce processing delay")
	}

	if qm.PacketsOutOfOrder > qm.PacketsReceived/100 { // 1% out-of-order threshold
		assessment.Issues = append(assessment.Issues, "Significant packet reordering detected")
		assessment.Recommendations = append(assessment.Recommendations, "Check for network path diversity or load balancing issues")
	}

	// Lower confidence if we don't have enough samples
	if qm.PacketsReceived < 100 {
		assessment.Confidence *= 0.7
	}

	qm.qualityAssessment = assessment
}

// GetQualityAssessment returns the current quality assessment
func (qm *QualityMetrics) GetQualityAssessment() QualityAssessment {
	qm.mutex.RLock()
	defer qm.mutex.RUnlock()

	return *qm.qualityAssessment
}

// GetMetricsSummary returns a summary of all quality metrics
func (qm *QualityMetrics) GetMetricsSummary() map[string]interface{} {
	qm.mutex.RLock()
	defer qm.mutex.RUnlock()

	return map[string]interface{}{
		"packets_received":     qm.PacketsReceived,
		"packets_lost":         qm.PacketsLost,
		"packets_out_of_order": qm.PacketsOutOfOrder,
		"packets_duplicated":   qm.PacketsDuplicated,
		"packet_loss_rate":     qm.GetPacketLossRate(),
		"jitter_ms":            qm.GetJitterMs(),
		"average_jitter_ms":    float64(qm.AverageJitter.Nanoseconds()) / 1000000.0,
		"max_jitter_ms":        float64(qm.MaxJitter.Nanoseconds()) / 1000000.0,
		"rtt_ms":               qm.GetRTTMs(),
		"average_rtt_ms":       float64(qm.AverageRTT.Nanoseconds()) / 1000000.0,
		"max_rtt_ms":           float64(qm.MaxRTT.Nanoseconds()) / 1000000.0,
		"mos_score":            qm.MOSScore,
		"r_factor":             qm.RFactor,
		"codec_type":           qm.CodecType,
		"sample_rate":          qm.SampleRate,
		"overall_rating":       qm.qualityAssessment.OverallRating,
		"issues":               qm.qualityAssessment.Issues,
		"recommendations":      qm.qualityAssessment.Recommendations,
	}
}

// Reset resets all quality metrics
func (qm *QualityMetrics) Reset() {
	qm.mutex.Lock()
	defer qm.mutex.Unlock()

	qm.PacketsReceived = 0
	qm.PacketsLost = 0
	qm.PacketsOutOfOrder = 0
	qm.PacketsDuplicated = 0
	qm.Jitter = 0
	qm.MaxJitter = 0
	qm.MinJitter = time.Duration(math.MaxInt64)
	qm.AverageJitter = 0
	qm.RoundTripTime = 0
	qm.MaxRTT = 0
	qm.MinRTT = time.Duration(math.MaxInt64)
	qm.AverageRTT = 0
	qm.MOSScore = 0
	qm.RFactor = 0
	qm.lastSequenceNumber = 0
	qm.lastTimestamp = 0
	qm.lastPacketTime = time.Time{}
	qm.jitterSamples = qm.jitterSamples[:0]
	qm.rttSamples = qm.rttSamples[:0]
	qm.qualityAssessment = &QualityAssessment{}
}
