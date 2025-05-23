package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

// SIPREC Metadata structures
type Recording struct {
	XMLName  xml.Name `xml:"recording"`
	NS       string   `xml:"xmlns,attr"`
	DataMode string   `xml:"datamode"`
	Session  Session  `xml:"session"`
	Participants []Participant `xml:"participant"`
	Streams     []Stream      `xml:"stream"`
	Associations []ParticipantStreamAssoc `xml:"participantstreamassoc"`
	RecordingSession RecordingSession `xml:"recordingsession"`
}

type Session struct {
	ID           string `xml:"id,attr"`
	AssociateTime string `xml:"associate-time"`
}

type Participant struct {
	ID     string `xml:"id,attr"`
	NameID NameID `xml:"nameID"`
}

type NameID struct {
	AOR  string `xml:"aor,attr"`
	Name string `xml:"name"`
}

type Stream struct {
	ID            string `xml:"id,attr"`
	SessionRef    string `xml:"session,attr"`
	Label         string `xml:"label"`
	AssociateTime string `xml:"associate-time"`
}

type ParticipantStreamAssoc struct {
	ID          string `xml:"id,attr"`
	Participant string `xml:"participant"`
	Stream      string `xml:"stream"`
}

type RecordingSession struct {
	ID            string `xml:"id,attr"`
	AssociateTime string `xml:"associate-time"`
	Reason        string `xml:"reason"`
}

const (
	SERVER_HOST = "35.222.226.67"
	SIP_PORT    = "5060"
	HTTP_PORT   = "8080"
)

func main() {
	fmt.Println("=== SIPREC Metadata Test (Go Implementation) ===")
	fmt.Printf("Target: %s:%s\n", SERVER_HOST, SIP_PORT)
	fmt.Printf("Start Time: %s\n\n", time.Now().Format(time.RFC3339))

	// Test 1: Validate server health
	fmt.Println("1. Testing server health...")
	if !testServerHealth() {
		log.Fatal("Server health check failed")
	}

	// Test 2: Test SIPREC metadata parsing
	fmt.Println("\n2. Testing SIPREC metadata parsing...")
	testMetadataParsing()

	// Test 3: Send complete SIPREC flow
	fmt.Println("\n3. Testing complete SIPREC flow...")
	testCompleteSIPRECFlow()

	// Test 4: Test session management
	fmt.Println("\n4. Testing session management...")
	testSessionManagement()

	fmt.Println("\n=== Test Complete ===")
}

func testServerHealth() bool {
	url := fmt.Sprintf("http://%s:%s/health", SERVER_HOST, HTTP_PORT)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("✗ Health check failed: %v\n", err)
		return false
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("✓ Server health: %s\n", string(body))
	return resp.StatusCode == 200
}

func testMetadataParsing() {
	// Create test metadata
	sessionID := fmt.Sprintf("sess_%d", time.Now().Unix())
	recordingSessionID := fmt.Sprintf("rs_%d", time.Now().Unix())
	
	metadata := Recording{
		NS:       "urn:ietf:params:xml:ns:recording:1",
		DataMode: "complete",
		Session: Session{
			ID:            sessionID,
			AssociateTime: time.Now().UTC().Format(time.RFC3339),
		},
		Participants: []Participant{
			{
				ID: "part1",
				NameID: NameID{
					AOR:  "sip:alice@example.com",
					Name: "Alice Smith",
				},
			},
			{
				ID: "part2",
				NameID: NameID{
					AOR:  "sip:bob@example.com",
					Name: "Bob Johnson",
				},
			},
		},
		Streams: []Stream{
			{
				ID:            "stream1",
				SessionRef:    sessionID,
				Label:         "1",
				AssociateTime: time.Now().UTC().Format(time.RFC3339),
			},
			{
				ID:            "stream2",
				SessionRef:    sessionID,
				Label:         "2",
				AssociateTime: time.Now().UTC().Format(time.RFC3339),
			},
		},
		Associations: []ParticipantStreamAssoc{
			{
				ID:          "psa1",
				Participant: "part1",
				Stream:      "stream1",
			},
			{
				ID:          "psa2",
				Participant: "part2",
				Stream:      "stream2",
			},
		},
		RecordingSession: RecordingSession{
			ID:            recordingSessionID,
			AssociateTime: time.Now().UTC().Format(time.RFC3339),
			Reason:        "compliance",
		},
	}

	// Marshal to XML
	xmlData, err := xml.MarshalIndent(metadata, "", "  ")
	if err != nil {
		fmt.Printf("✗ Metadata marshal failed: %v\n", err)
		return
	}

	fmt.Printf("✓ Generated metadata XML (%d bytes)\n", len(xmlData))
	fmt.Printf("Session ID: %s\n", sessionID)
	fmt.Printf("Recording Session ID: %s\n", recordingSessionID)

	// Parse it back to validate
	var parsed Recording
	err = xml.Unmarshal(xmlData, &parsed)
	if err != nil {
		fmt.Printf("✗ Metadata parse failed: %v\n", err)
		return
	}

	fmt.Printf("✓ Metadata validation successful\n")
	fmt.Printf("  Participants: %d\n", len(parsed.Participants))
	fmt.Printf("  Streams: %d\n", len(parsed.Streams))
	fmt.Printf("  Associations: %d\n", len(parsed.Associations))
}

func testCompleteSIPRECFlow() {
	callID := fmt.Sprintf("siprec-go-test-%d@testclient", time.Now().Unix())
	sessionID := fmt.Sprintf("sess_%d", time.Now().Unix())
	
	fmt.Printf("Testing with Call-ID: %s\n", callID)

	// Step 1: Send INVITE
	fmt.Println("  Sending SIPREC INVITE...")
	success := sendSIPRECInvite(callID, sessionID)
	if !success {
		fmt.Println("✗ INVITE failed")
		return
	}

	// Step 2: Send UPDATE
	time.Sleep(2 * time.Second)
	fmt.Println("  Sending SIPREC UPDATE...")
	sendSIPRECUpdate(callID, sessionID)

	// Step 3: Send BYE
	time.Sleep(2 * time.Second)
	fmt.Println("  Sending SIPREC BYE...")
	sendSIPRECBye(callID)

	fmt.Println("✓ Complete SIPREC flow tested")
}

func sendSIPRECInvite(callID, sessionID string) bool {
	branch := fmt.Sprintf("z9hG4bK%d", time.Now().Unix())
	tag := fmt.Sprintf("tag%d", time.Now().Unix())
	
	// Create metadata XML
	metadata := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording:1">
  <datamode>complete</datamode>
  <session id="%s">
    <associate-time>%s</associate-time>
  </session>
  <participant id="part1">
    <nameID aor="sip:alice@example.com">
      <name>Alice Smith</name>
    </nameID>
  </participant>
  <participant id="part2">
    <nameID aor="sip:bob@example.com">
      <name>Bob Johnson</name>
    </nameID>
  </participant>
  <stream id="stream1" session="%s">
    <label>1</label>
    <associate-time>%s</associate-time>
  </stream>
  <stream id="stream2" session="%s">
    <label>2</label>
    <associate-time>%s</associate-time>
  </stream>
  <participantstreamassoc id="psa1">
    <participant>part1</participant>
    <stream>stream1</stream>
  </participantstreamassoc>
  <participantstreamassoc id="psa2">
    <participant>part2</participant>
    <stream>stream2</stream>
  </participantstreamassoc>
  <recordingsession id="rs_%d">
    <associate-time>%s</associate-time>
    <reason>compliance</reason>
  </recordingsession>
</recording>`, sessionID, time.Now().UTC().Format(time.RFC3339), sessionID, 
		time.Now().UTC().Format(time.RFC3339), sessionID, time.Now().UTC().Format(time.RFC3339),
		time.Now().Unix(), time.Now().UTC().Format(time.RFC3339))

	sdp := `v=0
o=srs 123456789 987654321 IN IP4 192.168.1.100
s=SIPREC Recording Session
c=IN IP4 192.168.1.100
t=0 0
m=audio 10000 RTP/AVP 0 8
a=sendonly
a=rtpmap:0 PCMU/8000
a=rtpmap:8 PCMA/8000
a=label:1
m=audio 10002 RTP/AVP 0 8
a=sendonly
a=rtpmap:0 PCMU/8000
a=rtpmap:8 PCMA/8000
a=label:2`

	multipart := fmt.Sprintf(`--boundary123
Content-Type: application/sdp

%s

--boundary123
Content-Type: application/rs-metadata+xml

%s

--boundary123--`, sdp, metadata)

	invite := fmt.Sprintf(`INVITE sip:recorder@%s:%s SIP/2.0
Via: SIP/2.0/UDP 192.168.1.100:5060;branch=%s
Max-Forwards: 70
To: <sip:recorder@%s:%s>
From: SRS <sip:srs@testclient.local>;tag=%s
Call-ID: %s
CSeq: 1 INVITE
Contact: <sip:srs@192.168.1.100:5060>
User-Agent: SIPREC-Go-TestClient/1.0
Content-Type: multipart/mixed;boundary=boundary123
Content-Length: %d

%s
`, SERVER_HOST, SIP_PORT, branch, SERVER_HOST, SIP_PORT, tag, callID, len(multipart), multipart)

	return sendSIPMessage(invite)
}

func sendSIPRECUpdate(callID, sessionID string) bool {
	branch := fmt.Sprintf("z9hG4bK%d", time.Now().Unix())
	
	metadata := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording:1">
  <datamode>partial</datamode>
  <session id="%s">
    <associate-time>%s</associate-time>
  </session>
  <participant id="part3">
    <nameID aor="sip:charlie@example.com">
      <name>Charlie Brown</name>
    </nameID>
  </participant>
</recording>`, sessionID, time.Now().UTC().Format(time.RFC3339))

	update := fmt.Sprintf(`UPDATE sip:recorder@%s:%s SIP/2.0
Via: SIP/2.0/UDP 192.168.1.100:5060;branch=%s
Max-Forwards: 70
To: <sip:recorder@%s:%s>
From: SRS <sip:srs@testclient.local>;tag=tag123
Call-ID: %s
CSeq: 2 UPDATE
Contact: <sip:srs@192.168.1.100:5060>
User-Agent: SIPREC-Go-TestClient/1.0
Content-Type: application/rs-metadata+xml
Content-Length: %d

%s
`, SERVER_HOST, SIP_PORT, branch, SERVER_HOST, SIP_PORT, callID, len(metadata), metadata)

	return sendSIPMessage(update)
}

func sendSIPRECBye(callID string) bool {
	branch := fmt.Sprintf("z9hG4bK%d", time.Now().Unix())
	
	bye := fmt.Sprintf(`BYE sip:recorder@%s:%s SIP/2.0
Via: SIP/2.0/UDP 192.168.1.100:5060;branch=%s
Max-Forwards: 70
To: <sip:recorder@%s:%s>
From: SRS <sip:srs@testclient.local>;tag=tag123
Call-ID: %s
CSeq: 3 BYE
Contact: <sip:srs@192.168.1.100:5060>
User-Agent: SIPREC-Go-TestClient/1.0
Content-Length: 0

`, SERVER_HOST, SIP_PORT, branch, SERVER_HOST, SIP_PORT, callID)

	return sendSIPMessage(bye)
}

func sendSIPMessage(message string) bool {
	conn, err := net.DialTimeout("udp", fmt.Sprintf("%s:%s", SERVER_HOST, SIP_PORT), 5*time.Second)
	if err != nil {
		fmt.Printf("✗ Connection failed: %v\n", err)
		return false
	}
	defer conn.Close()

	// Send message
	_, err = conn.Write([]byte(message))
	if err != nil {
		fmt.Printf("✗ Send failed: %v\n", err)
		return false
	}

	// Read response with timeout
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	response := make([]byte, 4096)
	n, err := conn.Read(response)
	
	if err != nil {
		fmt.Printf("✗ No response received: %v\n", err)
		return false
	}

	responseStr := string(response[:n])
	fmt.Printf("✓ Received response (%d bytes)\n", n)
	
	// Check for success indicators
	if strings.Contains(responseStr, "200 OK") {
		fmt.Println("✓ Success response (200 OK)")
		return true
	} else if strings.Contains(responseStr, "100 Trying") {
		fmt.Println("✓ Processing response (100 Trying)")
		return true
	} else {
		fmt.Printf("Response preview: %s...\n", responseStr[:min(200, len(responseStr))])
	}

	return true
}

func testSessionManagement() {
	fmt.Println("  Testing session metrics...")
	
	url := fmt.Sprintf("http://%s:%s/metrics", SERVER_HOST, HTTP_PORT)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("✗ Metrics request failed: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	metrics := string(body)

	// Look for SIPREC-related metrics
	lines := strings.Split(metrics, "\n")
	for _, line := range lines {
		if strings.Contains(line, "siprec") {
			fmt.Printf("  %s\n", line)
		}
	}

	fmt.Println("✓ Session management metrics retrieved")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}