#!/usr/bin/env python3
"""Generate a simple RTP pcap file for SIPp testing with G.711 u-law audio."""

import struct
import os

def generate_ulaw_silence():
    """Generate u-law encoded silence (0xFF = silence in u-law)."""
    return b'\xff' * 160  # 160 samples = 20ms at 8kHz

def generate_ulaw_tone(frequency=440, sample_rate=8000, duration_ms=20):
    """Generate a simple tone in u-law format."""
    import math
    samples = int(sample_rate * duration_ms / 1000)
    data = []
    for i in range(samples):
        # Generate sine wave
        t = i / sample_rate
        sample = int(32767 * 0.5 * math.sin(2 * math.pi * frequency * t))
        # Convert to u-law (simplified)
        if sample < 0:
            sample = -sample
            sign = 0x80
        else:
            sign = 0

        # Compress using u-law algorithm
        sample = min(sample, 32635)
        sample += 0x84
        exponent = 7
        for exp in range(7, 0, -1):
            if sample & (1 << (exp + 7)):
                exponent = exp
                break
        mantissa = (sample >> (exponent + 3)) & 0x0F
        ulaw = ~(sign | (exponent << 4) | mantissa) & 0xFF
        data.append(ulaw)

    return bytes(data)

def create_rtp_packet(seq, timestamp, ssrc, payload):
    """Create an RTP packet."""
    # RTP header: V=2, P=0, X=0, CC=0, M=0, PT=0 (PCMU)
    header = struct.pack('>BBHII',
        0x80,  # V=2, P=0, X=0, CC=0
        0x00,  # M=0, PT=0 (PCMU)
        seq & 0xFFFF,
        timestamp & 0xFFFFFFFF,
        ssrc & 0xFFFFFFFF
    )
    return header + payload

def create_pcap_file(filename, num_packets=500, use_tone=True):
    """Create a pcap file with RTP packets."""
    # Pcap global header
    pcap_header = struct.pack('<IHHIIII',
        0xa1b2c3d4,  # Magic number
        2, 4,         # Version
        0,            # Timezone
        0,            # Sigfigs
        65535,        # Snaplen
        1             # Network (Ethernet)
    )

    packets = []
    ssrc = 0x12345678
    timestamp = 0

    for seq in range(num_packets):
        # Generate payload
        if use_tone:
            # Alternate between different tones for variety
            freq = 440 + (seq % 4) * 110  # 440, 550, 660, 770 Hz
            payload = generate_ulaw_tone(freq)
        else:
            payload = generate_ulaw_silence()

        rtp = create_rtp_packet(seq, timestamp, ssrc, payload)
        timestamp += 160  # 160 samples per packet

        # UDP header
        src_port = 10000
        dst_port = 10000
        udp_length = 8 + len(rtp)
        udp_header = struct.pack('>HHHH', src_port, dst_port, udp_length, 0)

        # IP header (simplified)
        ip_length = 20 + udp_length
        ip_header = struct.pack('>BBHHHBBHII',
            0x45,       # Version + IHL
            0x00,       # DSCP + ECN
            ip_length,
            seq,        # ID
            0x4000,     # Flags + Fragment offset (Don't Fragment)
            64,         # TTL
            17,         # Protocol (UDP)
            0,          # Checksum (0 for simplicity)
            0x7f000001, # Source IP (127.0.0.1)
            0x7f000001  # Dest IP (127.0.0.1)
        )

        # Ethernet header
        eth_header = b'\x00' * 6 + b'\x00' * 6 + b'\x08\x00'  # dst + src + type (IP)

        packet = eth_header + ip_header + udp_header + rtp

        # Pcap packet header
        ts_sec = seq * 20 // 1000
        ts_usec = (seq * 20 % 1000) * 1000
        pcap_pkt_header = struct.pack('<IIII',
            ts_sec,
            ts_usec,
            len(packet),
            len(packet)
        )

        packets.append(pcap_pkt_header + packet)

    with open(filename, 'wb') as f:
        f.write(pcap_header)
        for pkt in packets:
            f.write(pkt)

    duration_sec = num_packets * 20 / 1000
    print(f"Created {filename} with {num_packets} RTP packets ({duration_sec:.1f} seconds)")

if __name__ == '__main__':
    # Create a 10-second audio file (500 packets at 20ms each)
    create_pcap_file('test/sipp/g711_audio.pcap', num_packets=500, use_tone=True)
