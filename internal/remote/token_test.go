package remote_test

import (
	"net"
	"strings"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeToken() remote.PairingToken {
	return remote.PairingToken{
		IP:          net.ParseIP("192.168.1.100"),
		Port:        8403,
		PairingCode: [3]byte{0x0C, 0xD1, 0x4F},
	}
}

func TestEncodeDecodeRoundtrip(t *testing.T) {
	orig := makeToken()
	s, err := remote.EncodeToken(orig)
	require.NoError(t, err)

	got, err := remote.DecodeToken(s)
	require.NoError(t, err)

	assert.Equal(t, orig.IP.To4().String(), got.IP.String())
	assert.Equal(t, orig.Port, got.Port)
	assert.Equal(t, orig.PairingCode, got.PairingCode)
}

func TestTokenFormat(t *testing.T) {
	s, err := remote.EncodeToken(makeToken())
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(s, "KR-"), "must start with KR-")
	// Format: "KR-XXXXXX-XXXXXX-XXXXXX" (4 dash-separated parts)
	parts := strings.Split(s, "-")
	assert.Len(t, parts, 4, "format must be KR-XXXXXX-XXXXXX-XXXXXX (4 parts)")
	assert.Equal(t, "KR", parts[0])
	assert.Len(t, parts[1], 6)
	assert.Len(t, parts[2], 6)
	assert.Len(t, parts[3], 6)
}

func TestDecodeToken_CRCMismatch(t *testing.T) {
	s, err := remote.EncodeToken(makeToken())
	require.NoError(t, err)

	// Flip the last character to corrupt the CRC.
	runes := []rune(s)
	last := runes[len(runes)-1]
	if last == 'A' {
		runes[len(runes)-1] = 'B'
	} else {
		runes[len(runes)-1] = 'A'
	}
	corrupted := string(runes)

	_, err = remote.DecodeToken(corrupted)
	assert.Error(t, err, "CRC mismatch must return an error")
}

func TestDecodeToken_MissingPrefix(t *testing.T) {
	_, err := remote.DecodeToken("XXXX-YYYY-ZZZZ-00")
	assert.Error(t, err)
}

func TestDecodeToken_WrongLength(t *testing.T) {
	_, err := remote.DecodeToken("KR-SHORT")
	assert.Error(t, err)
}

func TestDecodeToken_IPv6Rejected(t *testing.T) {
	tok := remote.PairingToken{
		IP:   net.ParseIP("::1"), // IPv6
		Port: 8403,
	}
	_, err := remote.EncodeToken(tok)
	assert.Error(t, err, "IPv6 addresses must be rejected")
}

func TestDecodeToken_CaseInsensitive(t *testing.T) {
	orig := makeToken()
	s, err := remote.EncodeToken(orig)
	require.NoError(t, err)

	// Decode lowercase version.
	lower := strings.ToLower(s)
	got, err := remote.DecodeToken(lower)
	require.NoError(t, err)

	assert.Equal(t, orig.IP.To4().String(), got.IP.String())
	assert.Equal(t, orig.Port, got.Port)
}

func TestDifferentTokensAreUnique(t *testing.T) {
	t1 := remote.PairingToken{IP: net.ParseIP("10.0.0.1"), Port: 8403, PairingCode: [3]byte{1, 2, 3}}
	t2 := remote.PairingToken{IP: net.ParseIP("10.0.0.2"), Port: 8403, PairingCode: [3]byte{1, 2, 3}}

	s1, _ := remote.EncodeToken(t1)
	s2, _ := remote.EncodeToken(t2)
	assert.NotEqual(t, s1, s2)
}

func TestPairingCodeString_SixDigits(t *testing.T) {
	code := [3]byte{0x0C, 0xD1, 0x4F}
	s := remote.PairingCodeString(code)
	assert.Len(t, s, 6)
	// Must be all numeric.
	for _, c := range s {
		assert.True(t, c >= '0' && c <= '9', "pairing code must be numeric")
	}
}

func TestPairingCodeString_ZeroPadded(t *testing.T) {
	// Small value must be zero-padded.
	code := [3]byte{0x00, 0x00, 0x07}
	s := remote.PairingCodeString(code)
	assert.Equal(t, "000007", s)
}

func TestGeneratePairingCode_NotAllZero(t *testing.T) {
	code, err := remote.GeneratePairingCode()
	require.NoError(t, err)
	// Astronomically unlikely to be all zeros.
	assert.NotEqual(t, [3]byte{}, code)
}
