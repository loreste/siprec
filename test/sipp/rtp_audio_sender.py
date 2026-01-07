#!/usr/bin/env python3
"""RTP sender that plays actual audio from a u-law file."""

import socket
import struct
import time
import sys
import os

def create_rtp_packet(seq, timestamp, ssrc, payload, marker=False):
    """Create an RTP packet."""
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

def send_rtp_from_file(dest_ip, dest_port, audio_file, duration_seconds=None, local_port=None):
    """Send RTP stream from u-law audio file."""

    # Read audio file
    with open(audio_file, 'rb') as f:
        audio_data = f.read()

    print(f"Loaded {len(audio_data)} bytes from {audio_file}")

    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    if local_port:
        sock.bind(('0.0.0.0', local_port))

    ssrc = 0x12345678
    seq = 0
    timestamp = 0
    packet_size = 160  # 20ms of audio at 8kHz
    packets_per_second = 50
    packet_interval = 1.0 / packets_per_second

    total_audio_packets = len(audio_data) // packet_size

    if duration_seconds:
        max_packets = int(duration_seconds * packets_per_second)
        total_packets = min(total_audio_packets, max_packets)
    else:
        total_packets = total_audio_packets

    audio_duration = total_packets / packets_per_second
    print(f"Sending {total_packets} RTP packets to {dest_ip}:{dest_port} ({audio_duration:.1f}s of audio)")

    start_time = time.time()

    for i in range(total_packets):
        offset = (i * packet_size) % len(audio_data)
        payload = audio_data[offset:offset + packet_size]

        if len(payload) < packet_size:
            payload = payload + bytes(packet_size - len(payload))

        marker = (i == 0)
        packet = create_rtp_packet(seq, timestamp, ssrc, payload, marker)

        try:
            sock.sendto(packet, (dest_ip, dest_port))
        except Exception as e:
            print(f"Error sending packet {i}: {e}")
            break

        seq = (seq + 1) & 0xFFFF
        timestamp += packet_size

        # Pace the packets
        expected_time = start_time + (i + 1) * packet_interval
        sleep_time = expected_time - time.time()
        if sleep_time > 0:
            time.sleep(sleep_time)

        if (i + 1) % 500 == 0:
            elapsed = time.time() - start_time
            print(f"  Sent {i + 1}/{total_packets} packets ({elapsed:.1f}s)...")

    elapsed = time.time() - start_time
    print(f"Done! Sent {total_packets} packets in {elapsed:.2f}s")

    sock.close()

if __name__ == '__main__':
    if len(sys.argv) < 4:
        print(f"Usage: {sys.argv[0]} <dest_ip> <dest_port> <audio_file.ulaw> [duration_seconds] [local_port]")
        print(f"Example: {sys.argv[0]} 192.227.78.77 10000 test_audio_5min.ulaw 30")
        sys.exit(1)

    dest_ip = sys.argv[1]
    dest_port = int(sys.argv[2])
    audio_file = sys.argv[3]
    duration = float(sys.argv[4]) if len(sys.argv) > 4 else None
    local_port = int(sys.argv[5]) if len(sys.argv) > 5 else None

    if not os.path.exists(audio_file):
        print(f"Error: Audio file '{audio_file}' not found")
        sys.exit(1)

    send_rtp_from_file(dest_ip, dest_port, audio_file, duration, local_port)
