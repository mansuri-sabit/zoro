package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// DeepgramClient handles Speech-to-Text using Deepgram API
type DeepgramClient struct {
	apiKey  string
	timeout time.Duration
	logger  *zap.Logger
	baseURL string
}

// NewDeepgramClient creates a new Deepgram STT client
func NewDeepgramClient(apiKey string, timeout time.Duration, logger *zap.Logger) *DeepgramClient {
	if apiKey == "" {
		return &DeepgramClient{logger: logger}
	}

	return &DeepgramClient{
		apiKey:  apiKey,
		timeout: timeout,
		logger:  logger,
		baseURL: "https://api.deepgram.com/v1",
	}
}

// IsAvailable checks if Deepgram client is available
func (d *DeepgramClient) IsAvailable() bool {
	return d.apiKey != ""
}

// STTRequest represents a Deepgram STT request
type STTRequest struct {
	AudioData   []byte // Raw PCM16 audio data (16kHz, mono)
	SampleRate  int    // Sample rate (should be 16000)
	Language    string // Optional language code (e.g., "en", "hi")
	Model       string // Optional model (e.g., "nova-2", "base")
	Punctuate   bool   // Add punctuation
	Interim     bool   // Return interim results
	Endpointing bool   // Enable endpointing
}

// STTResponse represents a Deepgram STT response
type STTResponse struct {
	Text     string
	Language string
	IsFinal  bool
}

// SpeechToText converts speech audio to text using Deepgram
// Audio must be raw PCM16, 16kHz, mono, little-endian
func (d *DeepgramClient) SpeechToText(ctx context.Context, req *STTRequest) (*STTResponse, error) {
	if !d.IsAvailable() {
		return nil, fmt.Errorf("Deepgram STT service not available. Set DEEPGRAM_API_KEY environment variable")
	}

	if len(req.AudioData) == 0 {
		return nil, fmt.Errorf("audio data cannot be empty")
	}

	// Use defaults
	sampleRate := req.SampleRate
	if sampleRate == 0 {
		sampleRate = 16000 // Default 16kHz
	}

	model := req.Model
	if model == "" {
		model = "nova-2" // Best accuracy model
	}

	// Build URL with query parameters
	url := fmt.Sprintf("%s/listen?model=%s&language=%s&punctuate=%t&interim_results=%t&endpointing=%t",
		d.baseURL,
		model,
		req.Language,
		req.Punctuate,
		req.Interim,
		req.Endpointing,
	)

	if req.Language == "" {
		// Remove language parameter if not specified (let Deepgram auto-detect)
		url = fmt.Sprintf("%s/listen?model=%s&punctuate=%t&interim_results=%t&endpointing=%t",
			d.baseURL,
			model,
			req.Punctuate,
			req.Interim,
			req.Endpointing,
		)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(req.AudioData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "audio/pcm") // Raw PCM format
	httpReq.Header.Set("Authorization", "Token "+d.apiKey)

	// Execute request
	client := &http.Client{Timeout: d.timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Deepgram API error: %d - %s", resp.StatusCode, string(body))
	}

	// Parse response
	var deepgramResp struct {
		Results struct {
			Channels []struct {
				Alternatives []struct {
					Transcript string  `json:"transcript"`
					Confidence float64 `json:"confidence"`
				} `json:"alternatives"`
			} `json:"channels"`
		} `json:"results"`
		Metadata struct {
			Language string `json:"language"`
		} `json:"metadata"`
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if err := json.Unmarshal(body, &deepgramResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Extract transcript
	text := ""
	if len(deepgramResp.Results.Channels) > 0 {
		if len(deepgramResp.Results.Channels[0].Alternatives) > 0 {
			text = deepgramResp.Results.Channels[0].Alternatives[0].Transcript
		}
	}

	language := deepgramResp.Metadata.Language
	if language == "" {
		language = req.Language
		if language == "" {
			language = "unknown"
		}
	}

	return &STTResponse{
		Text:     text,
		Language: language,
		IsFinal:  true, // Prerecorded API always returns final results
	}, nil
}

