package audio

// Resample8kTo16k resamples 8kHz PCM16 audio to 16kHz using linear interpolation
// Input: 16-bit signed little-endian PCM at 8kHz
// Output: 16-bit signed little-endian PCM at 16kHz
func Resample8kTo16k(pcm8k []byte) []byte {
	if len(pcm8k) == 0 {
		return nil
	}

	// Convert bytes to int16 samples
	samples8k := make([]int16, len(pcm8k)/2)
	for i := 0; i < len(samples8k); i++ {
		samples8k[i] = int16(pcm8k[i*2]) | int16(pcm8k[i*2+1])<<8
	}

	// Resample: 8kHz -> 16kHz means 2x samples
	// Simple linear interpolation: for each input sample, output 2 samples
	// Sample 0: output sample 0
	// Sample 1: interpolate between sample 0 and sample 1, then output sample 1
	samples16k := make([]int16, len(samples8k)*2)

	for i := 0; i < len(samples8k); i++ {
		// First sample: direct copy
		samples16k[i*2] = samples8k[i]

		// Second sample: interpolate with next sample (or repeat if last)
		if i < len(samples8k)-1 {
			// Linear interpolation: average of current and next
			samples16k[i*2+1] = int16((int32(samples8k[i]) + int32(samples8k[i+1])) / 2)
		} else {
			// Last sample: repeat
			samples16k[i*2+1] = samples8k[i]
		}
	}

	// Convert back to bytes (little-endian)
	result := make([]byte, len(samples16k)*2)
	for i, sample := range samples16k {
		result[i*2] = byte(sample & 0xFF)
		result[i*2+1] = byte((sample >> 8) & 0xFF)
	}

	return result
}

// Resample16kTo8k resamples 16kHz PCM16 audio to 8kHz by decimation
// Input: 16-bit signed little-endian PCM at 16kHz
// Output: 16-bit signed little-endian PCM at 8kHz
func Resample16kTo8k(pcm16k []byte) []byte {
	if len(pcm16k) == 0 {
		return nil
	}

	// Convert bytes to int16 samples
	samples16k := make([]int16, len(pcm16k)/2)
	for i := 0; i < len(samples16k); i++ {
		samples16k[i] = int16(pcm16k[i*2]) | int16(pcm16k[i*2+1])<<8
	}

	// Decimate: take every other sample
	samples8k := make([]int16, len(samples16k)/2)
	for i := 0; i < len(samples8k); i++ {
		samples8k[i] = samples16k[i*2]
	}

	// Convert back to bytes (little-endian)
	result := make([]byte, len(samples8k)*2)
	for i, sample := range samples8k {
		result[i*2] = byte(sample & 0xFF)
		result[i*2+1] = byte((sample >> 8) & 0xFF)
	}

	return result
}

