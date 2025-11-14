package audio

// DecodeMuLawToPCM16 converts G.711 μ-law (8-bit) to 16-bit signed PCM
// μ-law is a companding algorithm used in telephony
// Input: μ-law encoded bytes (8-bit samples at 8kHz)
// Output: 16-bit signed little-endian PCM samples
func DecodeMuLawToPCM16(muLaw []byte) []byte {
	if len(muLaw) == 0 {
		return nil
	}

	// μ-law to linear conversion table
	// This implements the ITU-T G.711 standard
	pcm := make([]int16, len(muLaw))

	for i, mu := range muLaw {
		// Invert all bits (μ-law uses inverted representation)
		mu = ^mu

		// Extract sign bit (bit 7)
		sign := (mu & 0x80) >> 7

		// Extract exponent (bits 4-6)
		exponent := (mu & 0x70) >> 4

		// Extract mantissa (bits 0-3)
		mantissa := mu & 0x0F

		// Calculate linear value
		var linear int16

		if exponent == 0 {
			// Special case for exponent 0
			linear = int16(33 + 2*mantissa)
		} else {
			// Normal case: (33 + 2*mantissa) * 2^(exponent-1) - 33
			linear = int16((33 + 2*int(mantissa)) << (exponent - 1))
			linear -= 33
		}

		// Apply sign
		if sign == 0 {
			linear = -linear
		}

		pcm[i] = linear
	}

	// Convert to byte array (little-endian 16-bit)
	result := make([]byte, len(pcm)*2)
	for i, sample := range pcm {
		result[i*2] = byte(sample & 0xFF)
		result[i*2+1] = byte((sample >> 8) & 0xFF)
	}

	return result
}

