package media

import (
	"encoding/base64"
	"testing"

	"github.com/pion/sdp/v3"
	"github.com/pion/srtp/v2"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestParseSRTPAttributes(t *testing.T) {
	forwarder := &RTPForwarder{}
	keyMaterial := make([]byte, 30)
	for i := range keyMaterial {
		keyMaterial[i] = byte(i + 1)
	}

	attr := "1 AES_CM_128_HMAC_SHA1_80 inline:" + base64.StdEncoding.EncodeToString(keyMaterial)

	parseSRTPAttributes(forwarder, attr, logrus.New())

	assert.Equal(t, "AES_CM_128_HMAC_SHA1_80", forwarder.SRTPProfile)
	if assert.Len(t, forwarder.SRTPMasterKey, 16) {
		assert.Equal(t, keyMaterial[:16], forwarder.SRTPMasterKey)
	}
	if assert.Len(t, forwarder.SRTPMasterSalt, 14) {
		assert.Equal(t, keyMaterial[16:30], forwarder.SRTPMasterSalt)
	}
}

func TestDetermineSRTPProfile(t *testing.T) {
	assert.Equal(t, srtp.ProtectionProfileAes128CmHmacSha1_80, determineSRTPProfile("AES_CM_128_HMAC_SHA1_80"))
	assert.Equal(t, srtp.ProtectionProfileAes128CmHmacSha1_32, determineSRTPProfile("AES_CM_128_HMAC_SHA1_32"))
	assert.Equal(t, srtp.ProtectionProfileAeadAes128Gcm, determineSRTPProfile("AEAD_AES_128_GCM"))
	assert.Equal(t, srtp.ProtectionProfileAeadAes256Gcm, determineSRTPProfile("AEAD_AES_256_GCM"))
}

func TestConfigureForwarderForMediaDescription(t *testing.T) {
	const offer = `v=0
o=ATS99 399418590 399418590 IN IP4 192.168.22.133
s=SipCall
t=0 0
m=audio 11584 RTP/AVP 8 108
c=IN IP4 192.168.82.21
a=label:0
a=rtpmap:8 PCMA/8000
a=rtpmap:108 telephone-event/8000
a=sendonly
a=rtcp:11585
a=ptime:20
m=audio 15682 RTP/AVP 8 108
c=IN IP4 192.168.82.21
a=label:1
a=rtpmap:8 PCMA/8000
a=rtpmap:108 telephone-event/8000
a=sendonly
a=rtcp:15683
a=ptime:20
`

	session := &sdp.SessionDescription{}
	if err := session.Unmarshal([]byte(offer)); err != nil {
		t.Fatalf("failed to parse offer: %v", err)
	}

	logger := logrus.New()
	forwarderA := &RTPForwarder{}
	forwarderB := &RTPForwarder{}

	ConfigureForwarderForMediaDescription(forwarderA, session, session.MediaDescriptions[0], logger)
	ConfigureForwarderForMediaDescription(forwarderB, session, session.MediaDescriptions[1], logger)

	assert.Equal(t, 11585, forwarderA.ExpectedRemoteRTCPPort)
	assert.Equal(t, byte(8), forwarderA.CodecPayloadType)
	assert.Equal(t, 15683, forwarderB.ExpectedRemoteRTCPPort)
	assert.Equal(t, byte(8), forwarderB.CodecPayloadType)
}
