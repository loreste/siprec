package media

import (
	"encoding/base64"
	"testing"

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
