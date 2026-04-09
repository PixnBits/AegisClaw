package vault

import "strings"

// bech32 is a minimal encoder for the BIP173 bech32 format used by the age
// encryption library to represent X25519 identities.  Only encoding is
// implemented; decoding is handled by filippo.io/age itself.

const bech32Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

// bech32Encode encodes data with the given human-readable part and returns
// a lowercase bech32 string.
func bech32Encode(hrp string, data []byte) (string, error) {
	conv := bech32To5Bits(data)

	// Build the checksum over: expandedHRP || conv || 6×0
	expanded := bech32ExpandHRP(hrp)
	checksumInput := make([]byte, 0, len(expanded)+len(conv)+6)
	checksumInput = append(checksumInput, expanded...)
	checksumInput = append(checksumInput, conv...)
	checksumInput = append(checksumInput, 0, 0, 0, 0, 0, 0)
	pm := bech32Polymod(checksumInput) ^ 1

	var b strings.Builder
	b.WriteString(hrp)
	b.WriteByte('1')
	for _, c := range conv {
		b.WriteByte(bech32Charset[c])
	}
	for i := 5; i >= 0; i-- {
		b.WriteByte(bech32Charset[(pm>>(uint(i)*5))&31])
	}
	return b.String(), nil
}

// bech32To5Bits converts a byte slice from 8-bit to 5-bit groups (with padding).
func bech32To5Bits(data []byte) []byte {
	var result []byte
	acc, bits := 0, 0
	for _, b := range data {
		acc = acc<<8 | int(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			result = append(result, byte(acc>>bits)&31)
		}
	}
	if bits > 0 {
		result = append(result, byte(acc<<(5-bits))&31)
	}
	return result
}

// bech32ExpandHRP produces the HRP expansion required for checksum input.
func bech32ExpandHRP(hrp string) []byte {
	exp := make([]byte, 2*len(hrp)+1)
	for i := range hrp {
		exp[i] = hrp[i] >> 5
		exp[i+len(hrp)+1] = hrp[i] & 31
	}
	exp[len(hrp)] = 0
	return exp
}

// bech32Polymod computes the BIP173 polynomial checksum.
func bech32Polymod(values []byte) uint32 {
	c := uint32(1)
	for _, d := range values {
		c0 := byte(c >> 25)
		c = (c&0x1ffffff)<<5 ^ uint32(d)
		if c0&1 != 0 {
			c ^= 0x3b6a57b2
		}
		if c0&2 != 0 {
			c ^= 0x26508e6d
		}
		if c0&4 != 0 {
			c ^= 0x1ea119fa
		}
		if c0&8 != 0 {
			c ^= 0x3d4233dd
		}
		if c0&16 != 0 {
			c ^= 0x2a1462b3
		}
	}
	return c
}
