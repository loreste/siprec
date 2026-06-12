// Package media provides STUN discovery functionality for NAT traversal
// This implements basic STUN protocol support for determining external
// IP addresses and NAT type detection in cloud environments.
//
// Copyright (C) 2024 SIPREC Server Project
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package media

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
)

// STUN protocol constants (RFC 5389)
const (
	stunMagicCookie     uint32 = 0x2112A442
	stunHeaderSize      int    = 20
	stunTransactionSize int    = 12

	stunTypeBindingRequest uint16 = 0x0001
	stunTypeBindingSuccess uint16 = 0x0101
	stunAttrXorMappedAddr  uint16 = 0x0020
	stunAddressFamilyIPv4  uint8  = 0x01
	stunAddressFamilyIPv6  uint8  = 0x02
)

// createSTUNBindingRequest creates a STUN binding request (RFC 5389) with a
// cryptographically random 96-bit transaction ID. It returns the encoded
// request and the transaction ID for matching against the response.
func createSTUNBindingRequest() ([]byte, []byte, error) {
	stunPacket := make([]byte, stunHeaderSize)

	// Message Type: Binding Request (0x0001)
	binary.BigEndian.PutUint16(stunPacket[0:2], stunTypeBindingRequest)

	// Message Length: 0 (no attributes)
	binary.BigEndian.PutUint16(stunPacket[2:4], 0)

	// Magic Cookie: 0x2112A442
	binary.BigEndian.PutUint32(stunPacket[4:8], stunMagicCookie)

	// Transaction ID: 96 bits of cryptographically random data
	transactionID := make([]byte, stunTransactionSize)
	if _, err := rand.Read(transactionID); err != nil {
		return nil, nil, fmt.Errorf("failed to generate STUN transaction ID: %w", err)
	}
	copy(stunPacket[8:stunHeaderSize], transactionID)

	return stunPacket, transactionID, nil
}

// parseSTUNResponse parses a STUN binding success response, verifies the
// magic cookie and transaction ID, and extracts the external IP and port
// from the XOR-MAPPED-ADDRESS attribute (RFC 5389 section 15.2)
func parseSTUNResponse(response []byte, transactionID []byte) (string, int, error) {
	if len(response) < stunHeaderSize {
		return "", 0, fmt.Errorf("STUN response too short: %d bytes", len(response))
	}

	// Check if it's a Binding Success Response (0x0101)
	if msgType := binary.BigEndian.Uint16(response[0:2]); msgType != stunTypeBindingSuccess {
		return "", 0, fmt.Errorf("not a binding success response: type 0x%04x", msgType)
	}

	// Check magic cookie
	if cookie := binary.BigEndian.Uint32(response[4:8]); cookie != stunMagicCookie {
		return "", 0, fmt.Errorf("invalid magic cookie: 0x%08x", cookie)
	}

	// Verify the transaction ID matches the request (RFC 5389 section 7.3.3)
	if len(transactionID) != stunTransactionSize {
		return "", 0, fmt.Errorf("invalid transaction ID length: %d", len(transactionID))
	}
	if !bytes.Equal(response[8:stunHeaderSize], transactionID) {
		return "", 0, fmt.Errorf("transaction ID mismatch: response does not match request")
	}

	// Parse attributes, looking for XOR-MAPPED-ADDRESS
	msgLength := int(binary.BigEndian.Uint16(response[2:4]))
	end := stunHeaderSize + msgLength
	if end > len(response) {
		end = len(response)
	}

	offset := stunHeaderSize
	for offset+4 <= end {
		attrType := binary.BigEndian.Uint16(response[offset : offset+2])
		attrLength := int(binary.BigEndian.Uint16(response[offset+2 : offset+4]))

		if offset+4+attrLength > end {
			break
		}

		if attrType == stunAttrXorMappedAddr {
			attrData := response[offset+4 : offset+4+attrLength]
			return parseXorMappedAddress(attrData, transactionID)
		}

		// Advance past the attribute value, padded to a 4-byte boundary
		offset += 4 + attrLength
		if pad := attrLength % 4; pad != 0 {
			offset += 4 - pad
		}
	}

	return "", 0, fmt.Errorf("XOR-MAPPED-ADDRESS not found in response")
}

// parseXorMappedAddress decodes an XOR-MAPPED-ADDRESS attribute value.
// The port is XORed with the most significant 16 bits of the magic cookie.
// IPv4 addresses are XORed with the magic cookie; IPv6 addresses are XORed
// with the concatenation of the magic cookie and the transaction ID.
func parseXorMappedAddress(attrData []byte, transactionID []byte) (string, int, error) {
	// Family (1 byte after the reserved byte) + port (2 bytes) + address
	if len(attrData) < 8 {
		return "", 0, fmt.Errorf("XOR-MAPPED-ADDRESS attribute too short: %d bytes", len(attrData))
	}

	family := attrData[1]
	port := int(binary.BigEndian.Uint16(attrData[2:4]) ^ uint16(stunMagicCookie>>16))

	// XOR key: magic cookie followed by the transaction ID (only the first
	// 4 bytes are used for IPv4)
	xorKey := make([]byte, 4+stunTransactionSize)
	binary.BigEndian.PutUint32(xorKey[0:4], stunMagicCookie)
	copy(xorKey[4:], transactionID)

	switch family {
	case stunAddressFamilyIPv4:
		ip := make(net.IP, net.IPv4len)
		for i := 0; i < net.IPv4len; i++ {
			ip[i] = attrData[4+i] ^ xorKey[i]
		}
		return ip.String(), port, nil

	case stunAddressFamilyIPv6:
		if len(attrData) < 4+net.IPv6len {
			return "", 0, fmt.Errorf("XOR-MAPPED-ADDRESS IPv6 attribute too short: %d bytes", len(attrData))
		}
		ip := make(net.IP, net.IPv6len)
		for i := 0; i < net.IPv6len; i++ {
			ip[i] = attrData[4+i] ^ xorKey[i]
		}
		return ip.String(), port, nil

	default:
		return "", 0, fmt.Errorf("unsupported address family: 0x%02x", family)
	}
}
