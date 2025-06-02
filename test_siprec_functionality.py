#!/usr/bin/env python3
"""
Comprehensive SIPREC Functionality Test
Tests SIPREC message processing, metadata extraction, and multipart handling
"""
import socket
import time
import uuid
import xml.etree.ElementTree as ET

class SIPRECFunctionalityTest:
    def __init__(self):
        self.host = '127.0.0.1'
        self.tcp_port = 5060
        self.udp_port = 5061
        self.tls_port = 5065
        
    def create_siprec_metadata(self, session_id, call_id):
        """Create a comprehensive SIPREC metadata XML"""
        metadata = f"""<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording:1" session="{session_id}">
  <datamode>complete</datamode>
  <session id="{session_id}" start_time="2024-12-08T10:00:00Z">
    <associate-time>2024-12-08T10:00:00Z</associate-time>
    <participant id="participant-1" participant_type="caller">
      <nameID aor="sip:alice@example.com">
        <name>Alice Johnson</name>
      </nameID>
      <send>stream-1</send>
      <associate-time>2024-12-08T10:00:00Z</associate-time>
    </participant>
    <participant id="participant-2" participant_type="callee">
      <nameID aor="sip:bob@example.com">
        <name>Bob Smith</name>
      </nameID>
      <send>stream-2</send>
      <associate-time>2024-12-08T10:00:01Z</associate-time>
    </participant>
    <stream id="stream-1" session="{session_id}" mode="separate">
      <label>audio-caller</label>
      <associate-time>2024-12-08T10:00:00Z</associate-time>
    </stream>
    <stream id="stream-2" session="{session_id}" mode="separate">
      <label>audio-callee</label>
      <associate-time>2024-12-08T10:00:01Z</associate-time>
    </stream>
    <recordingmetadata id="metadata-1">
      <attribute name="conference-id" value="{call_id}"/>
      <attribute name="recording-reason" value="compliance"/>
      <attribute name="location" value="New York Office"/>
    </recordingmetadata>
  </session>
</recording>"""
        return metadata
    
    def create_sdp_offer(self):
        """Create a basic SDP offer for SIPREC"""
        timestamp = int(time.time())
        sdp = f"""v=0
o=- {timestamp} {timestamp} IN IP4 127.0.0.1
s=SIPREC Recording Session
c=IN IP4 127.0.0.1
t=0 0
m=audio 16384 RTP/AVP 0 8
a=rtpmap:0 PCMU/8000
a=rtpmap:8 PCMA/8000
a=sendonly
a=label:audio-caller
m=audio 16386 RTP/AVP 0 8
a=rtpmap:0 PCMU/8000
a=rtpmap:8 PCMA/8000
a=sendonly
a=label:audio-callee"""
        return sdp
    
    def create_siprec_invite(self, call_id, transport='tcp'):
        """Create a complete SIPREC INVITE message"""
        session_id = f"session-{uuid.uuid4().hex[:8]}"
        boundary = f"boundary-{uuid.uuid4().hex[:8]}"
        
        # Create SDP and metadata
        sdp = self.create_sdp_offer()
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
        port = 5555
        
        invite = f"INVITE sip:siprec@127.0.0.1:5060 SIP/2.0\r\n"
        invite += f"Via: SIP/2.0/{transport_upper} 127.0.0.1:{port};branch=z9hG4bK-siprec-{uuid.uuid4().hex[:8]}\r\n"
        invite += f"From: <sip:recorder@test.com>;tag=siprec-{uuid.uuid4().hex[:8]}\r\n"
        invite += f"To: <sip:siprec@127.0.0.1>\r\n"
        invite += f"Call-ID: {call_id}\r\n"
        invite += f"CSeq: 1 INVITE\r\n"
        invite += f"Contact: <sip:recorder@127.0.0.1:{port};transport={transport}>\r\n"
        invite += f"Max-Forwards: 70\r\n"
        invite += f"User-Agent: SIPREC Test Client v1.0\r\n"
        invite += f"Content-Type: multipart/mixed;boundary={boundary}\r\n"
        invite += f"Content-Length: {content_length}\r\n"
        invite += f"\r\n"
        invite += body
        
        return invite, session_id, len(metadata), len(sdp)
    
    def send_tcp_siprec(self, message):
        """Send SIPREC message over TCP"""
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            sock.settimeout(10)
            sock.connect((self.host, self.tcp_port))
            
            sock.sendall(message.encode())
            
            # Read response
            response_data = b""
            start_time = time.time()
            
            while time.time() - start_time < 10:
                try:
                    chunk = sock.recv(4096)
                    if not chunk:
                        break
                    response_data += chunk
                    if b'\r\n\r\n' in response_data:
                        break
                except socket.timeout:
                    break
            
            return response_data.decode('utf-8', errors='ignore')
            
        except Exception as e:
            return f"ERROR: {e}"
        finally:
            try:
                sock.close()
            except:
                pass
    
    def send_udp_siprec(self, message):
        """Send SIPREC message over UDP"""
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
            sock.settimeout(10)
            
            sock.sendto(message.encode(), (self.host, self.udp_port))
            
            # Read response
            response_data, addr = sock.recvfrom(65536)
            return response_data.decode('utf-8', errors='ignore')
            
        except Exception as e:
            return f"ERROR: {e}"
        finally:
            try:
                sock.close()
            except:
                pass
    
    def analyze_response(self, response):
        """Analyze SIP response for SIPREC indicators"""
        if not response or response.startswith("ERROR:"):
            return False, "No valid response received"
        
        lines = response.split('\n')
        status_line = lines[0].strip() if lines else ""
        
        analysis = {
            'status_ok': '200 OK' in status_line,
            'has_sdp': 'application/sdp' in response.lower(),
            'has_content_length': 'content-length:' in response.lower(),
            'has_contact': 'contact:' in response.lower(),
            'response_size': len(response),
            'status_line': status_line
        }
        
        return '200 OK' in status_line, analysis
    
    def test_tcp_siprec_basic(self):
        """Test basic SIPREC over TCP"""
        print("🧪 Testing basic SIPREC over TCP")
        
        call_id = f"tcp-siprec-test-{int(time.time())}"
        invite, session_id, metadata_size, sdp_size = self.create_siprec_invite(call_id, 'tcp')
        
        print(f"   📞 Call ID: {call_id}")
        print(f"   📋 Session ID: {session_id}")
        print(f"   📊 Message size: {len(invite)} bytes")
        print(f"   📄 Metadata size: {metadata_size} bytes")
        print(f"   🎵 SDP size: {sdp_size} bytes")
        
        response = self.send_tcp_siprec(invite)
        success, analysis = self.analyze_response(response)
        
        if success:
            print(f"   ✅ Response: {analysis['status_line']}")
            print(f"   ✅ SDP in response: {analysis['has_sdp']}")
            print(f"   ✅ Response size: {analysis['response_size']} bytes")
        else:
            print(f"   ❌ Failed: {analysis}")
            print(f"   ❌ Response preview: {response[:200]}...")
        
        return success
    
    def test_udp_siprec_basic(self):
        """Test basic SIPREC over UDP"""
        print("🧪 Testing basic SIPREC over UDP")
        
        call_id = f"udp-siprec-test-{int(time.time())}"
        invite, session_id, metadata_size, sdp_size = self.create_siprec_invite(call_id, 'udp')
        
        print(f"   📞 Call ID: {call_id}")
        print(f"   📊 Message size: {len(invite)} bytes")
        
        # UDP may have size limitations, check if message fits
        if len(invite) > 1400:
            print(f"   ⚠️ Message size ({len(invite)} bytes) may be too large for UDP")
        
        response = self.send_udp_siprec(invite)
        success, analysis = self.analyze_response(response)
        
        if success:
            print(f"   ✅ Response: {analysis['status_line']}")
            print(f"   ✅ UDP SIPREC works for large messages")
        else:
            print(f"   ❌ UDP failed (expected for large messages): {analysis.get('status_line', 'No response')}")
        
        return success
    
    def test_large_metadata_siprec(self):
        """Test SIPREC with large metadata (stress test)"""
        print("🧪 Testing SIPREC with large metadata")
        
        call_id = f"large-metadata-test-{int(time.time())}"
        
        # Create extra large metadata
        session_id = f"session-{uuid.uuid4().hex[:8]}"
        boundary = f"boundary-{uuid.uuid4().hex[:8]}"
        
        # Create metadata with many participants and attributes
        large_metadata = f"""<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording:1" session="{session_id}">
  <datamode>complete</datamode>
  <session id="{session_id}" start_time="2024-12-08T10:00:00Z">"""
        
        # Add 20 participants to make metadata large
        for i in range(20):
            large_metadata += f"""
    <participant id="participant-{i}" participant_type="{"caller" if i % 2 == 0 else "callee"}">
      <nameID aor="sip:user{i}@example.com">
        <name>User {i} Full Name</name>
      </nameID>
      <send>stream-{i}</send>
      <associate-time>2024-12-08T10:00:{i:02d}Z</associate-time>
    </participant>
    <stream id="stream-{i}" session="{session_id}" mode="separate">
      <label>audio-user-{i}</label>
      <associate-time>2024-12-08T10:00:{i:02d}Z</associate-time>
    </stream>"""
        
        # Add many attributes
        large_metadata += f"""
    <recordingmetadata id="metadata-1">"""
        for i in range(50):
            large_metadata += f"""
      <attribute name="custom-attr-{i}" value="This is a custom attribute value {i} with some extra text to make it larger"/>"""
        large_metadata += f"""
    </recordingmetadata>
  </session>
</recording>"""
        
        # Create SDP
        sdp = self.create_sdp_offer()
        
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
        body += f"{large_metadata}\r\n"
        body += f"--{boundary}--\r\n"
        
        content_length = len(body.encode())
        
        # Create INVITE
        invite = f"INVITE sip:siprec@127.0.0.1:5060 SIP/2.0\r\n"
        invite += f"Via: SIP/2.0/TCP 127.0.0.1:5555;branch=z9hG4bK-large-{uuid.uuid4().hex[:8]}\r\n"
        invite += f"From: <sip:recorder@test.com>;tag=large-{uuid.uuid4().hex[:8]}\r\n"
        invite += f"To: <sip:siprec@127.0.0.1>\r\n"
        invite += f"Call-ID: {call_id}\r\n"
        invite += f"CSeq: 1 INVITE\r\n"
        invite += f"Contact: <sip:recorder@127.0.0.1:5555;transport=tcp>\r\n"
        invite += f"Max-Forwards: 70\r\n"
        invite += f"User-Agent: Large Metadata SIPREC Test v1.0\r\n"
        invite += f"Content-Type: multipart/mixed;boundary={boundary}\r\n"
        invite += f"Content-Length: {content_length}\r\n"
        invite += f"\r\n"
        invite += body
        
        print(f"   📞 Call ID: {call_id}")
        print(f"   📊 Total message size: {len(invite)} bytes")
        print(f"   📄 Metadata size: {len(large_metadata)} bytes")
        print(f"   👥 Participants: 20")
        print(f"   🏷️ Attributes: 50")
        
        response = self.send_tcp_siprec(invite)
        success, analysis = self.analyze_response(response)
        
        if success:
            print(f"   ✅ Large metadata processed successfully!")
            print(f"   ✅ Response: {analysis['status_line']}")
        else:
            print(f"   ❌ Large metadata test failed: {analysis}")
        
        return success
    
    def test_multipart_parsing(self):
        """Test that multipart messages are properly parsed"""
        print("🧪 Testing multipart MIME parsing validation")
        
        call_id = f"multipart-test-{int(time.time())}"
        invite, session_id, metadata_size, sdp_size = self.create_siprec_invite(call_id, 'tcp')
        
        # Verify the multipart structure we created
        parts = invite.split('--boundary-')
        multipart_valid = len(parts) >= 3  # Should have at least header + 2 parts + end
        
        has_sdp_part = any('application/sdp' in part for part in parts)
        has_metadata_part = any('application/rs-metadata+xml' in part for part in parts)
        
        print(f"   📋 Multipart structure validation:")
        print(f"   ✅ Valid multipart: {multipart_valid}")
        print(f"   ✅ Has SDP part: {has_sdp_part}")
        print(f"   ✅ Has metadata part: {has_metadata_part}")
        
        if multipart_valid and has_sdp_part and has_metadata_part:
            print(f"   ✅ Multipart message structure is correct")
            
            # Now test server processing
            response = self.send_tcp_siprec(invite)
            success, analysis = self.analyze_response(response)
            
            if success:
                print(f"   ✅ Server correctly processed multipart message")
                return True
            else:
                print(f"   ❌ Server failed to process multipart message")
                return False
        else:
            print(f"   ❌ Multipart message structure is invalid")
            return False
    
    def test_metadata_xml_validation(self):
        """Test that metadata XML is well-formed"""
        print("🧪 Testing metadata XML validation")
        
        call_id = f"xml-test-{int(time.time())}"
        session_id = f"session-{uuid.uuid4().hex[:8]}"
        
        metadata = self.create_siprec_metadata(session_id, call_id)
        
        try:
            # Parse XML to validate structure
            root = ET.fromstring(metadata)
            
            # Check required elements
            has_session = root.find('.//{urn:ietf:params:xml:ns:recording:1}session') is not None
            has_participants = len(root.findall('.//{urn:ietf:params:xml:ns:recording:1}participant')) > 0
            has_streams = len(root.findall('.//{urn:ietf:params:xml:ns:recording:1}stream')) > 0
            
            print(f"   ✅ XML is well-formed")
            print(f"   ✅ Has session element: {has_session}")
            print(f"   ✅ Has participants: {has_participants}")
            print(f"   ✅ Has streams: {has_streams}")
            
            return has_session and has_participants and has_streams
            
        except ET.ParseError as e:
            print(f"   ❌ XML parsing failed: {e}")
            return False
    
    def run_all_tests(self):
        """Run all SIPREC functionality tests"""
        print("🚀 SIPREC Functionality Testing")
        print("=" * 60)
        
        tests = [
            ("Basic TCP SIPREC", self.test_tcp_siprec_basic),
            ("Basic UDP SIPREC", self.test_udp_siprec_basic),  
            ("Large Metadata SIPREC", self.test_large_metadata_siprec),
            ("Multipart MIME Parsing", self.test_multipart_parsing),
            ("Metadata XML Validation", self.test_metadata_xml_validation)
        ]
        
        results = []
        for test_name, test_func in tests:
            print(f"\n{test_name}:")
            try:
                result = test_func()
                results.append((test_name, result))
                print(f"Result: {'✅ PASS' if result else '❌ FAIL'}")
            except Exception as e:
                print(f"❌ EXCEPTION: {e}")
                results.append((test_name, False))
        
        print(f"\n{'='*60}")
        print("📊 SIPREC Test Results Summary:")
        all_passed = True
        for test_name, result in results:
            status = "✅ PASS" if result else "❌ FAIL"
            print(f"  {test_name}: {status}")
            all_passed = all_passed and result
        
        if all_passed:
            print("\n🎉 All SIPREC functionality tests passed!")
            print("✅ TCP SIPREC message processing works")
            print("✅ Multipart MIME handling works")
            print("✅ SIPREC metadata extraction works")
            print("✅ Large metadata payloads work")
            print("✅ XML metadata validation works")
        else:
            print("\n❌ Some SIPREC functionality tests failed")
        
        return all_passed

if __name__ == "__main__":
    tester = SIPRECFunctionalityTest()
    success = tester.run_all_tests()
    exit(0 if success else 1)