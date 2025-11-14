package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"time"

	"go.uber.org/zap"
)

// OpenAITTSService handles Text-to-Speech using OpenAI TTS API
type OpenAITTSService struct {
	apiKey  string
	timeout time.Duration
	logger  *zap.Logger
	baseURL string
}

// NewOpenAITTSService creates a new OpenAI TTS service
func NewOpenAITTSService(apiKey string, timeout time.Duration, logger *zap.Logger) *OpenAITTSService {
	if apiKey == "" {
		return &OpenAITTSService{logger: logger}
	}

	return &OpenAITTSService{
		apiKey:  apiKey,
		timeout: timeout,
		logger:  logger,
		baseURL: "https://api.openai.com/v1",
	}
}

// IsAvailable checks if OpenAI TTS service is available
func (s *OpenAITTSService) IsAvailable() bool {
	return s.apiKey != ""
}

// OpenAITTSRequest represents an OpenAI TTS request
type OpenAITTSRequest struct {
	Text   string
	Model  string // tts-1 or tts-1-hd
	Voice  string // alloy, echo, fable, onyx, nova, shimmer
	Format string // mp3, opus, aac, flac, pcm (for raw PCM)
	Speed  float64
}

// TextToSpeechPCM converts text to speech and returns raw 16-bit 16kHz PCM audio
func (s *OpenAITTSService) TextToSpeechPCM(ctx context.Context, req *OpenAITTSRequest) ([]byte, error) {
	if !s.IsAvailable() {
		return nil, fmt.Errorf("OpenAI TTS service not available. Set OPENAI_API_KEY environment variable")
	}

	if req.Text == "" {
		return nil, fmt.Errorf("text cannot be empty")
	}

	// Use defaults if not provided
	model := req.Model
	if model == "" {
		model = "tts-1-hd" // Best quality as per requirements
	}

	voice := req.Voice
	if voice == "" {
		voice = "shimmer" // Female, super natural Hindi as per requirements
	}

	format := req.Format
	if format == "" {
		format = "mp3" // Use mp3 and convert to PCM 16kHz
	}

	speed := req.Speed
	if speed == 0 {
		speed = 1.0 // Default speed
	}

	// Prepare request body
	requestBody := map[string]interface{}{
		"model": model,
		"input":  req.Text,
		"voice":  voice,
		"response_format": format,
		"speed":  speed,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Build URL
	url := fmt.Sprintf("%s/audio/speech", s.baseURL)

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)

	// Execute request
	client := &http.Client{Timeout: s.timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenAI TTS API error: %d - %s", resp.StatusCode, string(body))
	}

	// Read audio data (mp3 format)
	mp3Data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read audio data: %w", err)
	}

	// Convert MP3 to raw 16-bit 16kHz PCM using ffmpeg
	if len(mp3Data) > 0 {
		pcmData, err := s.convertMP3ToPCM16k(mp3Data)
		if err != nil {
			return nil, fmt.Errorf("failed to convert MP3 to PCM: %w", err)
		}
		return pcmData, nil
	}

	return nil, fmt.Errorf("no audio data received")
}

// convertMP3ToPCM16k converts MP3 to raw 16-bit 16kHz mono PCM
func (s *OpenAITTSService) convertMP3ToPCM16k(mp3Data []byte) ([]byte, error) {
	// Use ffmpeg: -i pipe:0 (MP3 input) -> -f s16le -ar 16000 -ac 1 (16-bit 16kHz mono PCM output)
	cmd := exec.Command("ffmpeg",
		"-i", "pipe:0",     // Read MP3 from stdin
		"-f", "s16le",      // Output format: 16-bit signed little-endian PCM
		"-ar", "16000",     // Output sample rate: 16kHz
		"-ac", "1",         // Output channels: mono
		"-",                // Output to stdout
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

