#!/usr/bin/env python3
"""Simple RTP sender for SIPREC testing - sends G.711 u-law audio."""

import socket
import struct
import time
import sys
import math

def generate_ulaw_tone(frequency=440, sample_rate=8000, duration_ms=20):
    """Generate a tone in u-law format."""
    samples = int(sample_rate * duration_ms / 1000)
    data = []
    for i in range(samples):
        t = i / sample_rate
        # Generate sine wave
        sample = int(32767 * 0.3 * math.sin(2 * math.pi * frequency * t))

        # Convert to u-law
        sign = 0x80 if sample < 0 else 0
        sample = abs(sample)
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

def create_rtp_packet(seq, timestamp, ssrc, payload, marker=False):
    """Create an RTP packet."""
    # RTP header: V=2, P=0, X=0, CC=0, M=marker, PT=0 (PCMU)
    byte1 = 0x80  # V=2, P=0, X=0, CC=0
    byte2 = 0x80 if marker else 0x00  # M=marker, PT=0 (PCMU)

    header = struct.pack('>BBHII',
        byte1,
        byte2,
        seq & 0xFFFF,
        timestamp & 0xFFFFFFFF,
        ssrc & 0xFFFFFFFF
    )
    return header + payload

def send_rtp_stream(dest_ip, dest_port, duration_seconds=5, local_port=None):
    """Send RTP stream to destination."""
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)

    if local_port:
        sock.bind(('0.0.0.0', local_port))

    ssrc = 0x12345678
    seq = 0
    timestamp = 0
    packets_per_second = 50  # 20ms packets
    packet_interval = 1.0 / packets_per_second

    total_packets = int(duration_seconds * packets_per_second)

    print(f"Sending {total_packets} RTP packets to {dest_ip}:{dest_port} over {duration_seconds}s")

    start_time = time.time()

    for i in range(total_packets):
        # Generate different tones
        freq = 440 + (i % 8) * 55  # Vary frequency
        payload = generate_ulaw_tone(freq)

        marker = (i == 0)  # First packet has marker bit
        packet = create_rtp_packet(seq, timestamp, ssrc, payload, marker)

        try:
            sock.sendto(packet, (dest_ip, dest_port))
        except Exception as e:
            print(f"Error sending packet {i}: {e}")
            break

        seq = (seq + 1) & 0xFFFF
        timestamp += 160  # 160 samples per 20ms packet

        # Pace the packets
        expected_time = start_time + (i + 1) * packet_interval
        sleep_time = expected_time - time.time()
        if sleep_time > 0:
            time.sleep(sleep_time)

        if (i + 1) % 50 == 0:
            print(f"  Sent {i + 1}/{total_packets} packets...")

    elapsed = time.time() - start_time
    print(f"Done! Sent {total_packets} packets in {elapsed:.2f}s ({total_packets/elapsed:.1f} pps)")

    sock.close()

if __name__ == '__main__':
    if len(sys.argv) < 3:
        print(f"Usage: {sys.argv[0]} <dest_ip> <dest_port> [duration_seconds] [local_port]")
        print(f"Example: {sys.argv[0]} 192.227.78.77 10000 5")
        sys.exit(1)

    dest_ip = sys.argv[1]
    dest_port = int(sys.argv[2])
    duration = float(sys.argv[3]) if len(sys.argv) > 3 else 5.0
    local_port = int(sys.argv[4]) if len(sys.argv) > 4 else None

    send_rtp_stream(dest_ip, dest_port, duration, local_port)
