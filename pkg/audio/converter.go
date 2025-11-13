package audio

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os/exec"
	"io"
)

// ConvertMP3ToPCM converts MP3 audio to 16-bit PCM, 8kHz, mono
// Returns raw PCM bytes ready for chunking
func ConvertMP3ToPCM(mp3Data []byte) ([]byte, error) {
	// Try using ffmpeg if available (most reliable)
	if hasFFmpeg() {
		return convertWithFFmpeg(mp3Data)
	}

	// Fallback: For now, return error if ffmpeg not available
	// In production, you might want to add a pure Go MP3 decoder
	return nil, fmt.Errorf("ffmpeg not available - audio conversion requires ffmpeg")
}

// convertWithFFmpeg uses ffmpeg to convert MP3 to PCM
func convertWithFFmpeg(mp3Data []byte) ([]byte, error) {
	// ffmpeg command: -i - (read from stdin) -f s16le -ar 8000 -ac 1 (16-bit PCM, 8kHz, mono) - (output to stdout)
	cmd := exec.Command("ffmpeg",
		"-i", "pipe:0",           // Read from stdin
		"-f", "s16le",             // 16-bit signed little-endian PCM
		"-ar", "8000",             // 8kHz sample rate
		"-ac", "1",                // Mono
		"-",                       // Output to stdout
	)

	cmd.Stdin = bytes.NewReader(mp3Data)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &bytes.Buffer{} // Suppress stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg conversion failed: %w", err)
	}

	return out.Bytes(), nil
}

// hasFFmpeg checks if ffmpeg is available in PATH
func hasFFmpeg() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// ChunkPCM splits PCM audio data into chunks of specified size
// Returns base64-encoded chunks ready for Exotel JSON format
func ChunkPCM(pcmData []byte, chunkSize int) [][]byte {
	if chunkSize <= 0 {
		chunkSize = 3200 // Default Exotel chunk size
	}

	var chunks [][]byte
	for i := 0; i < len(pcmData); i += chunkSize {
		end := i + chunkSize
		if end > len(pcmData) {
			end = len(pcmData)
		}
		chunk := pcmData[i:end]
		chunks = append(chunks, chunk)
	}

	return chunks
}

// EncodePCMChunkToBase64 encodes a PCM chunk to base64 for Exotel JSON
func EncodePCMChunkToBase64(pcmChunk []byte) string {
	return base64.StdEncoding.EncodeToString(pcmChunk)
}

// DecodeBase64PCM decodes base64-encoded PCM from Exotel
func DecodeBase64PCM(base64Data string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(base64Data)
}

// ConvertAndChunk converts MP3 to PCM and chunks it for Exotel streaming
// Returns base64-encoded chunks of 3200 bytes each
func ConvertAndChunk(mp3Data []byte, chunkSize int) ([]string, error) {
	if chunkSize <= 0 {
		chunkSize = 3200 // Default Exotel chunk size
	}

	// Convert MP3 to PCM
	pcmData, err := ConvertMP3ToPCM(mp3Data)
	if err != nil {
		return nil, fmt.Errorf("failed to convert MP3 to PCM: %w", err)
	}

	// Chunk the PCM data
	chunks := ChunkPCM(pcmData, chunkSize)

	// Base64 encode each chunk
	encodedChunks := make([]string, len(chunks))
	for i, chunk := range chunks {
		encodedChunks[i] = EncodePCMChunkToBase64(chunk)
	}

	return encodedChunks, nil
}

// StreamPCMChunks streams PCM chunks through a callback function
// Useful for real-time streaming without loading all chunks into memory
func StreamPCMChunks(mp3Data []byte, chunkSize int, callback func([]byte) error) error {
	if chunkSize <= 0 {
		chunkSize = 3200
	}

	// Convert MP3 to PCM using ffmpeg with streaming
	if !hasFFmpeg() {
		return fmt.Errorf("ffmpeg not available")
	}

	cmd := exec.Command("ffmpeg",
		"-i", "pipe:0",
		"-f", "s16le",
		"-ar", "8000",
		"-ac", "1",
		"-",
	)

	cmd.Stdin = bytes.NewReader(mp3Data)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	cmd.Stderr = &bytes.Buffer{}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	// Stream chunks as they're available
	buffer := make([]byte, chunkSize)
	for {
		n, err := stdout.Read(buffer)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buffer[:n])
			if err := callback(chunk); err != nil {
				cmd.Process.Kill()
				return fmt.Errorf("callback error: %w", err)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			cmd.Process.Kill()
			return fmt.Errorf("read error: %w", err)
		}
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("ffmpeg wait error: %w", err)
	}

	return nil
}

