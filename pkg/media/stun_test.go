package media

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rfc5769TransactionID is the transaction ID used by the RFC 5769 test vectors
var rfc5769TransactionID = []byte{
	0xb7, 0xe7, 0xa7, 0x01, 0xbc, 0x34, 0xd6, 0x86, 0xfa, 0x87, 0xdf, 0xae,
}

// rfc5769IPv4Response is the sample IPv4 binding success response from
// RFC 5769 section 2.2 (SOFTWARE, XOR-MAPPED-ADDRESS, MESSAGE-INTEGRITY,
// FINGERPRINT). The mapped address is 192.0.2.1:32853.
var rfc5769IPv4Response = []byte{
	0x01, 0x01, 0x00, 0x3c, // Response type and message length
	0x21, 0x12, 0xa4, 0x42, // Magic cookie
	0xb7, 0xe7, 0xa7, 0x01, //
	0xbc, 0x34, 0xd6, 0x86, // Transaction ID
	0xfa, 0x87, 0xdf, 0xae, //
	0x80, 0x22, 0x00, 0x0b, // SOFTWARE attribute header
	0x74, 0x65, 0x73, 0x74, //
	0x20, 0x76, 0x65, 0x63, // UTF-8 server name "test vector"
	0x74, 0x6f, 0x72, 0x20, // (with padding)
	0x00, 0x20, 0x00, 0x08, // XOR-MAPPED-ADDRESS attribute header
	0x00, 0x01, 0xa1, 0x47, // Address family (IPv4) and xor'd mapped port
	0xe1, 0x12, 0xa6, 0x43, // Xor'd mapped IP address
	0x00, 0x08, 0x00, 0x14, // MESSAGE-INTEGRITY attribute header
	0x2b, 0x91, 0xf5, 0x99, //
	0xfd, 0x9e, 0x90, 0xc3, //
	0x8c, 0x74, 0x89, 0xf9, // HMAC-SHA1 fingerprint
	0x92, 0xaf, 0x9b, 0xa5, //
	0x3f, 0x06, 0xbe, 0x7d, //
	0x7b, 0x4c, 0x96, 0x7c, //
	0x80, 0x28, 0x00, 0x04, // FINGERPRINT attribute header
	0xc0, 0x7d, 0x4c, 0x96, // CRC32 fingerprint
}

func TestCreateSTUNBindingRequest(t *testing.T) {
	packet, txID, err := createSTUNBindingRequest()
	require.NoError(t, err)

	require.Len(t, packet, 20)
	require.Len(t, txID, 12)

	// Message type: Binding Request
	assert.Equal(t, uint16(0x0001), binary.BigEndian.Uint16(packet[0:2]))

	// Message length: 0
	assert.Equal(t, uint16(0), binary.BigEndian.Uint16(packet[2:4]))

	// Magic cookie
	assert.Equal(t, uint32(0x2112A442), binary.BigEndian.Uint32(packet[4:8]))

	// Transaction ID embedded in the packet matches the returned ID
	assert.Equal(t, txID, packet[8:20])

	// Transaction ID must not be all zeros
	assert.False(t, bytes.Equal(txID, make([]byte, 12)), "transaction ID must be random, not zeros")

	// Two requests must have distinct transaction IDs
	_, txID2, err := createSTUNBindingRequest()
	require.NoError(t, err)
	assert.False(t, bytes.Equal(txID, txID2), "transaction IDs must be unique per request")
}

func TestParseSTUNResponseRFC5769IPv4(t *testing.T) {
	ip, port, err := parseSTUNResponse(rfc5769IPv4Response, rfc5769TransactionID)
	require.NoError(t, err)
	assert.Equal(t, "192.0.2.1", ip)
	assert.Equal(t, 32853, port)
}

func TestParseSTUNResponseTransactionIDMismatch(t *testing.T) {
	wrongTxID := append([]byte(nil), rfc5769TransactionID...)
	wrongTxID[0] ^= 0xff

	_, _, err := parseSTUNResponse(rfc5769IPv4Response, wrongTxID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "transaction ID mismatch")
}

// buildSTUNResponse constructs a binding success response containing an
// XOR-MAPPED-ADDRESS attribute for the given address
func buildSTUNResponse(t *testing.T, txID []byte, ip net.IP, port int) []byte {
	t.Helper()

	var family byte
	var addr []byte
	if ip4 := ip.To4(); ip4 != nil {
		family = 0x01
		addr = ip4
	} else {
		family = 0x02
		addr = ip.To16()
	}

	xorKey := []byte{0x21, 0x12, 0xa4, 0x42}
	xorKey = append(xorKey, txID...)

	attrValue := make([]byte, 4+len(addr))
	attrValue[0] = 0x00
	attrValue[1] = family
	binary.BigEndian.PutUint16(attrValue[2:4], uint16(port)^0x2112)
	for i, b := range addr {
		attrValue[4+i] = b ^ xorKey[i]
	}

	packet := make([]byte, 20, 24+len(attrValue))
	binary.BigEndian.PutUint16(packet[0:2], 0x0101)
	binary.BigEndian.PutUint16(packet[2:4], uint16(4+len(attrValue)))
	binary.BigEndian.PutUint32(packet[4:8], 0x2112A442)
	copy(packet[8:20], txID)

	attrHeader := make([]byte, 4)
	binary.BigEndian.PutUint16(attrHeader[0:2], 0x0020)
	binary.BigEndian.PutUint16(attrHeader[2:4], uint16(len(attrValue)))
	packet = append(packet, attrHeader...)
	packet = append(packet, attrValue...)

	return packet
}

func TestParseSTUNResponseIPv6(t *testing.T) {
	// RFC 5769 section 2.4 uses 2001:db8:1234:5678:11:2233:4455:6677 port 32853
	expectedIP := net.ParseIP("2001:db8:1234:5678:11:2233:4455:6677")
	require.NotNil(t, expectedIP)

	response := buildSTUNResponse(t, rfc5769TransactionID, expectedIP, 32853)

	ip, port, err := parseSTUNResponse(response, rfc5769TransactionID)
	require.NoError(t, err)
	assert.Equal(t, expectedIP.String(), ip)
	assert.Equal(t, 32853, port)
}

func TestParseSTUNResponseEncodeDecodeRoundTrip(t *testing.T) {
	_, txID, err := createSTUNBindingRequest()
	require.NoError(t, err)

	response := buildSTUNResponse(t, txID, net.ParseIP("203.0.113.7"), 54321)

	ip, port, err := parseSTUNResponse(response, txID)
	require.NoError(t, err)
	assert.Equal(t, "203.0.113.7", ip)
	assert.Equal(t, 54321, port)
}

func TestParseSTUNResponseErrors(t *testing.T) {
	validTxID := rfc5769TransactionID

	t.Run("too short", func(t *testing.T) {
		_, _, err := parseSTUNResponse([]byte{0x01, 0x01}, validTxID)
		assert.Error(t, err)
	})

	t.Run("not a success response", func(t *testing.T) {
		response := append([]byte(nil), rfc5769IPv4Response...)
		response[0] = 0x00
		response[1] = 0x01 // Binding Request, not Success Response
		_, _, err := parseSTUNResponse(response, validTxID)
		assert.Error(t, err)
	})

	t.Run("invalid magic cookie", func(t *testing.T) {
		response := append([]byte(nil), rfc5769IPv4Response...)
		response[4] = 0x00
		_, _, err := parseSTUNResponse(response, validTxID)
		assert.Error(t, err)
	})

	t.Run("missing XOR-MAPPED-ADDRESS", func(t *testing.T) {
		// Header only, no attributes
		response := make([]byte, 20)
		binary.BigEndian.PutUint16(response[0:2], 0x0101)
		binary.BigEndian.PutUint32(response[4:8], 0x2112A442)
		copy(response[8:20], validTxID)
		_, _, err := parseSTUNResponse(response, validTxID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "XOR-MAPPED-ADDRESS not found")
	})

	t.Run("unsupported address family", func(t *testing.T) {
		response := buildSTUNResponse(t, validTxID, net.ParseIP("192.0.2.1"), 1234)
		response[25] = 0x09 // corrupt the family byte
		_, _, err := parseSTUNResponse(response, validTxID)
		assert.Error(t, err)
	})
}
