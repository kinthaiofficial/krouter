// Package remote implements LAN remote access for the krouter daemon.
//
// Token format (spec/10 §3):
//
//	11 bytes:
//	  byte 0:    version = 0x01
//	  bytes 1-4: IPv4 address (big-endian)
//	  bytes 5-6: port uint16 (big-endian)
//	  bytes 7-9: 24-bit random pairing code
//	  byte 10:   CRC-8 (poly 0x07) over bytes 0-9
//
// Encoded to 16 Base32 Crockford characters, formatted as "KR-XXXX-XXXX-XXXX-XX".
package remote

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
)

const (
	crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	tokenRawLen       = 11
	tokenEncodedLen   = 18 // ceil(11*8/5) = 18
	tokenVersion      = 0x01
)

// PairingToken holds the decoded fields from a KR-… token string.
type PairingToken struct {
	IP          net.IP
	Port        uint16
	PairingCode [3]byte
}

// EncodeToken encodes a PairingToken into a "KR-XXXX-XXXX-XXXX-XX" string.
func EncodeToken(t PairingToken) (string, error) {
	ip4 := t.IP.To4()
	if ip4 == nil {
		return "", errors.New("remote: token requires an IPv4 address")
	}

	var raw [tokenRawLen]byte
	raw[0] = tokenVersion
	copy(raw[1:5], ip4)
	binary.BigEndian.PutUint16(raw[5:7], t.Port)
	copy(raw[7:10], t.PairingCode[:])
	raw[10] = crc8(raw[:10])

	encoded := base32CrockfordEncode(raw[:])
	// Format as KR-XXXXXX-XXXXXX-XXXXXX (18 chars → 6+6+6 with separators).
	return fmt.Sprintf("KR-%s-%s-%s",
		encoded[0:6],
		encoded[6:12],
		encoded[12:18],
	), nil
}

// DecodeToken parses a "KR-XXXX-XXXX-XXXX-XX" token string.
func DecodeToken(s string) (PairingToken, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if !strings.HasPrefix(s, "KR-") {
		return PairingToken{}, errors.New("remote: token must start with KR-")
	}
	// Strip "KR-" prefix and inter-group dashes.
	body := strings.ReplaceAll(s[3:], "-", "")
	if len(body) != tokenEncodedLen {
		return PairingToken{}, fmt.Errorf("remote: token body must be %d chars, got %d", tokenEncodedLen, len(body))
	}

	raw, err := base32CrockfordDecode(body)
	if err != nil {
		return PairingToken{}, fmt.Errorf("remote: decode token: %w", err)
	}
	if len(raw) < tokenRawLen {
		return PairingToken{}, errors.New("remote: decoded token too short")
	}

	if raw[0] != tokenVersion {
		return PairingToken{}, fmt.Errorf("remote: unsupported token version %d", raw[0])
	}
	if crc8(raw[:10]) != raw[10] {
		return PairingToken{}, errors.New("remote: token CRC check failed")
	}

	var t PairingToken
	t.IP = net.IP(raw[1:5])
	t.Port = binary.BigEndian.Uint16(raw[5:7])
	copy(t.PairingCode[:], raw[7:10])
	return t, nil
}

// GeneratePairingCode returns 3 random bytes for the pairing code field.
func GeneratePairingCode() ([3]byte, error) {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return b, fmt.Errorf("remote: generate pairing code: %w", err)
	}
	return b, nil
}

// PairingCodeString formats a 3-byte pairing code as a 6-digit decimal string.
func PairingCodeString(code [3]byte) string {
	n := int(code[0])<<16 | int(code[1])<<8 | int(code[2])
	return fmt.Sprintf("%06d", n%1_000_000)
}

// --- CRC-8 (poly 0x07) ---

func crc8(data []byte) byte {
	var crc byte
	for _, b := range data {
		crc ^= b
		for i := 0; i < 8; i++ {
			if crc&0x80 != 0 {
				crc = (crc << 1) ^ 0x07
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// --- Base32 Crockford codec ---

// base32CrockfordEncode encodes src bytes using the Crockford Base32 alphabet.
// Output length: ceil(len(src)*8/5).
func base32CrockfordEncode(src []byte) string {
	if len(src) == 0 {
		return ""
	}
	bits := 0
	buf := 0
	var out []byte
	for _, b := range src {
		buf = (buf << 8) | int(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out = append(out, crockfordAlphabet[(buf>>bits)&0x1F])
		}
	}
	if bits > 0 {
		out = append(out, crockfordAlphabet[(buf<<(5-bits))&0x1F])
	}
	return string(out)
}

// base32CrockfordDecode decodes a Crockford Base32 string into bytes.
func base32CrockfordDecode(s string) ([]byte, error) {
	bits := 0
	buf := 0
	var out []byte
	for _, c := range strings.ToUpper(s) {
		val := strings.IndexRune(crockfordAlphabet, c)
		if val < 0 {
			return nil, fmt.Errorf("invalid Crockford character %q", c)
		}
		buf = (buf << 5) | val
		bits += 5
		if bits >= 8 {
			bits -= 8
			out = append(out, byte(buf>>bits))
		}
	}
	return out, nil
}
