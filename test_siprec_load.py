#!/usr/bin/env python3
"""
SIPREC Load Testing Script
Tests SIPREC server with multiple concurrent requests
"""
import socket
import time
import uuid
import threading
import random
import argparse
import sys
from datetime import datetime
from concurrent.futures import ThreadPoolExecutor, as_completed
import statistics

class SIPRECLoadTest:
    def __init__(self, host='localhost', tcp_port=5060, udp_port=5061):
        self.host = host
        self.tcp_port = tcp_port
        self.udp_port = udp_port
        self.stats = {
            'total_requests': 0,
            'successful_requests': 0,
            'failed_requests': 0,
            'total_time': 0,
            'response_times': [],
            'errors': []
        }
        self.stats_lock = threading.Lock()
        
    def create_siprec_metadata(self, session_id, call_id, language='en-US'):
        """Create SIPREC metadata with correct structure for server validation"""
        languages = ['en-US', 'es-ES', 'fr-FR', 'de-DE', 'it-IT', 'pt-BR', 'ja-JP', 'ko-KR', 'zh-CN']
        random_lang = random.choice(languages)
        
        metadata = f"""<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording:1" session="{session_id}" state="active" sequence="1">
  <participant id="participant-1">
    <name>Test Caller {random.randint(1,100)}</name>
    <aor>sip:caller{random.randint(1000,9999)}@test.com</aor>
  </participant>
  <participant id="participant-2">
    <name>Test Callee {random.randint(1,100)}</name>
    <aor>sip:callee{random.randint(1000,9999)}@test.com</aor>
  </participant>
  <stream label="audio-caller" streamid="stream-1" type="audio" mode="separate"/>
  <stream label="audio-callee" streamid="stream-2" type="audio" mode="separate"/>
  <sessionrecordingassoc sessionid="{session_id}" callid="{call_id}"/>
</recording>"""
        return metadata
    
    def create_sdp_offer(self, port_offset=0):
        """Create SDP offer with random codecs and port numbers"""
        timestamp = int(time.time())
        base_port = 16384 + (port_offset * 4)
        
        codecs = ['0', '8', '9', '18']  # PCMU, PCMA, G722, G729
        selected_codecs = random.sample(codecs, random.randint(1, len(codecs)))
        codec_string = ' '.join(selected_codecs)
        
        sdp = f"""v=0
o=- {timestamp} {timestamp} IN IP4 {self.host}
s=SIPREC Load Test Session
c=IN IP4 {self.host}
t=0 0
m=audio {base_port} RTP/AVP {codec_string}"""
        
        for codec in selected_codecs:
            if codec == '0':
                sdp += f"\na=rtpmap:0 PCMU/8000"
            elif codec == '8':
                sdp += f"\na=rtpmap:8 PCMA/8000"
            elif codec == '9':
                sdp += f"\na=rtpmap:9 G722/8000"
            elif codec == '18':
                sdp += f"\na=rtpmap:18 G729/8000"
        
        sdp += f"""
a=sendonly
a=label:audio-caller
m=audio {base_port + 2} RTP/AVP {codec_string}"""
        
        for codec in selected_codecs:
            if codec == '0':
                sdp += f"\na=rtpmap:0 PCMU/8000"
            elif codec == '8':
                sdp += f"\na=rtpmap:8 PCMA/8000"
            elif codec == '9':
                sdp += f"\na=rtpmap:9 G722/8000"
            elif codec == '18':
                sdp += f"\na=rtpmap:18 G729/8000"
        
        sdp += f"""
a=sendonly
a=label:audio-callee"""
        
        return sdp
    
    def create_siprec_invite(self, call_id, transport='tcp', port_offset=0):
        """Create a SIPREC INVITE message"""
        session_id = f"session-{uuid.uuid4().hex[:8]}"
        boundary = f"boundary-{uuid.uuid4().hex[:8]}"
        
        # Create SDP and metadata
        sdp = self.create_sdp_offer(port_offset)
        metadata = self.create_siprec_metadata(session_id, call_id)
        
        # Create multipart body
        body = f"--{boundary}\r\n"
        body += f"Content-Type: application/sdp\r\n"
        body += f"Content-Disposition: session;handling=required\r\n"
        body += f"\r\n"
        body += f"{sdp}\r\n"
        body += f"--{boundary}\r\n"
        body += f"Content-Type: application/rs-metadata+xml\r\n" 
        body += f"Content-Disposition: recording-session\r\n"
        body += f"\r\n"
        body += f"{metadata}\r\n"
        body += f"--{boundary}--\r\n"
        
        content_length = len(body.encode())
        
        # Create SIP headers
        transport_upper = transport.upper()
        client_port = 5555 + random.randint(0, 1000)
        
        port = self.tcp_port if transport == 'tcp' else self.udp_port
        
        invite = f"INVITE sip:siprec@{self.host}:{port} SIP/2.0\r\n"
        invite += f"Via: SIP/2.0/{transport_upper} {self.host}:{client_port};branch=z9hG4bK-load-{uuid.uuid4().hex[:8]}\r\n"
        invite += f"From: <sip:loadtest@test.com>;tag=load-{uuid.uuid4().hex[:8]}\r\n"
        invite += f"To: <sip:siprec@{self.host}>\r\n"
        invite += f"Call-ID: {call_id}\r\n"
        invite += f"CSeq: 1 INVITE\r\n"
        invite += f"Contact: <sip:loadtest@{self.host}:{client_port};transport={transport}>\r\n"
        invite += f"Max-Forwards: 70\r\n"
        invite += f"User-Agent: SIPREC Load Test v1.0\r\n"
        invite += f"Content-Type: multipart/mixed;boundary={boundary}\r\n"
        invite += f"Content-Length: {content_length}\r\n"
        invite += f"\r\n"
        invite += body
        
        return invite, session_id
    
    def create_ack_message(self, invite_message, response):
        """Create ACK message to complete SIP call setup"""
        # Extract necessary headers from INVITE and 200 OK response
        invite_lines = invite_message.split('\r\n')
        response_lines = response.split('\r\n')
        
        # Extract Call-ID, From, To, Via from INVITE
        call_id = ""
        from_header = ""
        via_header = ""
        
        for line in invite_lines:
            if line.startswith('Call-ID:'):
                call_id = line
            elif line.startswith('From:'):
                from_header = line
            elif line.startswith('Via:'):
                via_header = line
        
        # Extract To header with tag from response
        to_header = ""
        for line in response_lines:
            if line.startswith('To:'):
                to_header = line
                break
        
        # Build ACK message (fix Via header with new branch)
        import uuid
        new_branch = f"z9hG4bK-ack-{uuid.uuid4().hex[:8]}"
        via_fixed = via_header.split(';')[0] + f";branch={new_branch}"
        
        ack = f"ACK sip:siprec@{self.host}:{self.tcp_port} SIP/2.0\r\n"
        ack += f"{via_fixed}\r\n"
        ack += f"{from_header}\r\n"
        ack += f"{to_header}\r\n"
        ack += f"{call_id}\r\n"
        ack += f"CSeq: 1 ACK\r\n"
        ack += f"Max-Forwards: 70\r\n"
        ack += f"Content-Length: 0\r\n"
        ack += f"\r\n"
        
        return ack
    
    def send_tcp_request(self, message):
        """Send SIPREC message over TCP and measure response time"""
        start_time = time.time()
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            sock.settimeout(30)
            sock.connect((self.host, self.tcp_port))
            
            sock.sendall(message.encode())
            
            # Read response
            response_data = b""
            while True:
                chunk = sock.recv(4096)
                if not chunk:
                    break
                response_data += chunk
                if b'\r\n\r\n' in response_data:
                    # Check if we have complete response with body
                    if b'Content-Length:' in response_data:
                        headers_end = response_data.find(b'\r\n\r\n')
                        headers = response_data[:headers_end].decode('utf-8', errors='ignore')
                        for line in headers.split('\r\n'):
                            if line.startswith('Content-Length:'):
                                content_length = int(line.split(':')[1].strip())
                                body_length = len(response_data) - headers_end - 4
                                if body_length >= content_length:
                                    break
                    else:
                        break
            
            elapsed_time = time.time() - start_time
            response = response_data.decode('utf-8', errors='ignore')
            
            if '200 OK' in response:
                # Send ACK to complete the call setup
                ack_message = self.create_ack_message(message, response)
                sock.sendall(ack_message.encode())
                return True, elapsed_time, response
            elif '400 Bad Request' in response:
                return True, elapsed_time, response
            else:
                return False, elapsed_time, response
                
        except Exception as e:
            elapsed_time = time.time() - start_time
            return False, elapsed_time, str(e)
        finally:
            try:
                sock.close()
            except:
                pass
    
    def send_udp_request(self, message):
        """Send SIPREC message over UDP and measure response time"""
        start_time = time.time()
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
            sock.settimeout(5)
            
            sock.sendto(message.encode(), (self.host, self.udp_port))
            
            # Read response
            response_data, addr = sock.recvfrom(65536)
            elapsed_time = time.time() - start_time
            response = response_data.decode('utf-8', errors='ignore')
            
            if '200 OK' in response or '400 Bad Request' in response:
                return True, elapsed_time, response
            else:
                return False, elapsed_time, response
                
        except Exception as e:
            elapsed_time = time.time() - start_time
            return False, elapsed_time, str(e)
        finally:
            try:
                sock.close()
            except:
                pass
    
    def process_request(self, request_id, transport='tcp'):
        """Process a single SIPREC request"""
        call_id = f"load-test-{request_id}-{int(time.time() * 1000)}"
        invite, session_id = self.create_siprec_invite(call_id, transport, request_id % 100)
        
        if transport == 'tcp':
            success, elapsed_time, response = self.send_tcp_request(invite)
        else:
            success, elapsed_time, response = self.send_udp_request(invite)
        
        # Update stats
        with self.stats_lock:
            self.stats['total_requests'] += 1
            if success:
                self.stats['successful_requests'] += 1
            else:
                self.stats['failed_requests'] += 1
                self.stats['errors'].append(f"Request {request_id}: {response[:100]}")
            
            self.stats['response_times'].append(elapsed_time)
            self.stats['total_time'] += elapsed_time
        
        return success, elapsed_time, call_id
    
    def run_concurrent_test(self, num_requests=100, max_workers=10, transport='tcp'):
        """Run concurrent SIPREC requests"""
        print(f"\n🚀 Starting SIPREC Load Test")
        print(f"   Target: {self.host}:{self.tcp_port if transport == 'tcp' else self.udp_port}")
        print(f"   Protocol: {transport.upper()}")
        print(f"   Total Requests: {num_requests}")
        print(f"   Concurrent Workers: {max_workers}")
        print(f"   Start Time: {datetime.now().isoformat()}")
        print("-" * 60)
        
        # Reset stats
        self.stats = {
            'total_requests': 0,
            'successful_requests': 0,
            'failed_requests': 0,
            'total_time': 0,
            'response_times': [],
            'errors': []
        }
        
        start_time = time.time()
        
        # Run requests concurrently
        with ThreadPoolExecutor(max_workers=max_workers) as executor:
            futures = []
            for i in range(num_requests):
                future = executor.submit(self.process_request, i, transport)
                futures.append(future)
                
                # Small delay between submissions to avoid overwhelming
                if i % 10 == 0:
                    time.sleep(0.01)
            
            # Process results as they complete
            completed = 0
            for future in as_completed(futures):
                completed += 1
                success, elapsed_time, call_id = future.result()
                
                # Print progress every 10%
                if completed % (num_requests // 10) == 0:
                    progress = (completed / num_requests) * 100
                    print(f"   Progress: {progress:.0f}% ({completed}/{num_requests})")
        
        total_elapsed = time.time() - start_time
        
        # Calculate statistics
        response_times = self.stats['response_times']
        avg_response_time = statistics.mean(response_times) if response_times else 0
        median_response_time = statistics.median(response_times) if response_times else 0
        min_response_time = min(response_times) if response_times else 0
        max_response_time = max(response_times) if response_times else 0
        
        if len(response_times) > 1:
            std_dev = statistics.stdev(response_times)
        else:
            std_dev = 0
        
        requests_per_second = num_requests / total_elapsed if total_elapsed > 0 else 0
        
        # Print results
        print("\n📊 Load Test Results")
        print("-" * 60)
        print(f"Total Requests:      {self.stats['total_requests']}")
        print(f"Successful:          {self.stats['successful_requests']} ({(self.stats['successful_requests']/num_requests*100):.1f}%)")
        print(f"Failed:              {self.stats['failed_requests']} ({(self.stats['failed_requests']/num_requests*100):.1f}%)")
        print(f"Total Time:          {total_elapsed:.2f} seconds")
        print(f"Requests/Second:     {requests_per_second:.2f}")
        print(f"\nResponse Times:")
        print(f"  Average:           {avg_response_time*1000:.2f} ms")
        print(f"  Median:            {median_response_time*1000:.2f} ms")
        print(f"  Min:               {min_response_time*1000:.2f} ms")
        print(f"  Max:               {max_response_time*1000:.2f} ms")
        print(f"  Std Dev:           {std_dev*1000:.2f} ms")
        
        # Print errors if any
        if self.stats['errors']:
            print(f"\n❌ Errors ({len(self.stats['errors'])} total):")
            for error in self.stats['errors'][:5]:  # Show first 5 errors
                print(f"   {error}")
            if len(self.stats['errors']) > 5:
                print(f"   ... and {len(self.stats['errors']) - 5} more errors")
        
        success_rate = (self.stats['successful_requests'] / num_requests * 100) if num_requests > 0 else 0
        
        if success_rate >= 99:
            print(f"\n✅ Excellent! Server handled {success_rate:.1f}% of requests successfully")
        elif success_rate >= 95:
            print(f"\n✅ Good! Server handled {success_rate:.1f}% of requests successfully")
        elif success_rate >= 90:
            print(f"\n⚠️  Warning: Server only handled {success_rate:.1f}% of requests successfully")
        else:
            print(f"\n❌ Poor performance: Server only handled {success_rate:.1f}% of requests successfully")
        
        return self.stats

def main():
    parser = argparse.ArgumentParser(description='SIPREC Load Testing Tool')
    parser.add_argument('host', help='SIPREC server hostname or IP')
    parser.add_argument('-p', '--tcp-port', type=int, default=5060, help='TCP port (default: 5060)')
    parser.add_argument('-u', '--udp-port', type=int, default=5061, help='UDP port (default: 5061)')
    parser.add_argument('-n', '--requests', type=int, default=100, help='Number of requests (default: 100)')
    parser.add_argument('-w', '--workers', type=int, default=10, help='Concurrent workers (default: 10)')
    parser.add_argument('-t', '--transport', choices=['tcp', 'udp'], default='tcp', help='Transport protocol (default: tcp)')
    parser.add_argument('--stress', action='store_true', help='Run stress test with increasing load')
    
    args = parser.parse_args()
    
    tester = SIPRECLoadTest(args.host, args.tcp_port, args.udp_port)
    
    if args.stress:
        # Stress test with increasing load
        print("🔥 Running STRESS TEST with increasing load levels")
        
        load_levels = [
            (10, 5),    # 10 requests, 5 workers
            (50, 10),   # 50 requests, 10 workers
            (100, 20),  # 100 requests, 20 workers
            (500, 50),  # 500 requests, 50 workers
            (1000, 100) # 1000 requests, 100 workers
        ]
        
        for requests, workers in load_levels:
            print(f"\n{'='*60}")
            print(f"STRESS LEVEL: {requests} requests with {workers} workers")
            print(f"{'='*60}")
            
            stats = tester.run_concurrent_test(requests, workers, args.transport)
            
            # Stop if success rate drops below 90%
            success_rate = (stats['successful_requests'] / requests * 100) if requests > 0 else 0
            if success_rate < 90:
                print(f"\n⚠️  Stopping stress test - success rate dropped to {success_rate:.1f}%")
                break
            
            # Small delay between levels
            time.sleep(2)
    else:
        # Normal load test
        tester.run_concurrent_test(args.requests, args.workers, args.transport)

if __name__ == "__main__":
    main()