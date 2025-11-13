package ai

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

// TTSService handles Text-to-Speech using ElevenLabs
type TTSService struct {
	apiKey            string
	defaultVoiceID    string
	defaultModelID    string
	defaultOutputFormat string
	timeout           time.Duration
	logger            *zap.Logger
	baseURL           string
}

// NewTTSService creates a new TTS service
func NewTTSService(apiKey, voiceID, modelID, outputFormat string, timeout time.Duration, logger *zap.Logger) *TTSService {
	if apiKey == "" {
		return &TTSService{logger: logger}
	}

	return &TTSService{
		apiKey:              apiKey,
		defaultVoiceID:      voiceID,
		defaultModelID:      modelID,
		defaultOutputFormat: outputFormat,
		timeout:             timeout,
		logger:              logger,
		baseURL:             "https://api.elevenlabs.io/v1",
	}
}

// IsAvailable checks if TTS service is available
func (s *TTSService) IsAvailable() bool {
	return s.apiKey != ""
}

// TTSRequest represents a TTS request
type TTSRequest struct {
	Text            string
	VoiceID         string
	ModelID         string
	OutputFormat    string
	Stability       float64
	SimilarityBoost float64
}

// TextToSpeech converts text to speech audio
func (s *TTSService) TextToSpeech(ctx context.Context, req *TTSRequest) ([]byte, error) {
	if !s.IsAvailable() {
		return nil, fmt.Errorf("TTS service not available. Set ELEVENLABS_API_KEY environment variable")
	}

	if req.Text == "" {
		return nil, fmt.Errorf("text cannot be empty")
	}

	// Use defaults if not provided
	voiceID := req.VoiceID
	if voiceID == "" {
		voiceID = s.defaultVoiceID
	}
	if voiceID == "" {
		voiceID = "21m00Tcm4TlvDq8ikWAM" // Default voice
	}

	modelID := req.ModelID
	if modelID == "" {
		modelID = s.defaultModelID
	}
	if modelID == "" {
		modelID = "eleven_multilingual_v2" // Default model
	}

	outputFormat := req.OutputFormat
	if outputFormat == "" {
		outputFormat = s.defaultOutputFormat
	}
	if outputFormat == "" {
		outputFormat = "mp3_44100_128" // Default format
	}

	stability := req.Stability
	if stability == 0 {
		stability = 0.5 // Default stability
	}

	similarityBoost := req.SimilarityBoost
	if similarityBoost == 0 {
		similarityBoost = 0.5 // Default similarity boost
	}

	// Prepare request body
	requestBody := map[string]interface{}{
		"text":             req.Text,
		"model_id":         modelID,
		"voice_settings": map[string]interface{}{
			"stability":        stability,
			"similarity_boost": similarityBoost,
		},
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Build URL
	url := fmt.Sprintf("%s/text-to-speech/%s?output_format=%s", s.baseURL, voiceID, outputFormat)

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("xi-api-key", s.apiKey)
	httpReq.Header.Set("Accept", "audio/mpeg")

	// Execute request
	client := &http.Client{Timeout: s.timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ElevenLabs API error: %d - %s", resp.StatusCode, string(body))
	}

	// Read audio data
	audioData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read audio data: %w", err)
	}

	return audioData, nil
}

// TextToSpeechStream converts text to speech with streaming
func (s *TTSService) TextToSpeechStream(ctx context.Context, req *TTSRequest, callback func([]byte) error) error {
	if !s.IsAvailable() {
		return fmt.Errorf("TTS service not available. Set ELEVENLABS_API_KEY environment variable")
	}

	if req.Text == "" {
		return fmt.Errorf("text cannot be empty")
	}

	// Use defaults if not provided
	voiceID := req.VoiceID
	if voiceID == "" {
		voiceID = s.defaultVoiceID
	}
	if voiceID == "" {
		voiceID = "21m00Tcm4TlvDq8ikWAM"
	}

	modelID := req.ModelID
	if modelID == "" {
		modelID = s.defaultModelID
	}
	if modelID == "" {
		modelID = "eleven_multilingual_v2"
	}

	outputFormat := req.OutputFormat
	if outputFormat == "" {
		outputFormat = s.defaultOutputFormat
	}
	if outputFormat == "" {
		outputFormat = "mp3_44100_128"
	}

	stability := req.Stability
	if stability == 0 {
		stability = 0.5
	}

	similarityBoost := req.SimilarityBoost
	if similarityBoost == 0 {
		similarityBoost = 0.5
	}

	// Prepare request body
	requestBody := map[string]interface{}{
		"text":      req.Text,
		"model_id":  modelID,
		"voice_settings": map[string]interface{}{
			"stability":        stability,
			"similarity_boost": similarityBoost,
		},
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Build URL with streaming
	url := fmt.Sprintf("%s/text-to-speech/%s/stream?output_format=%s", s.baseURL, voiceID, outputFormat)

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("xi-api-key", s.apiKey)
	httpReq.Header.Set("Accept", "audio/mpeg")

	// Execute request
	client := &http.Client{Timeout: s.timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ElevenLabs API error: %d - %s", resp.StatusCode, string(body))
	}

	// Stream audio chunks
	buffer := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			if err := callback(buffer[:n]); err != nil {
				return fmt.Errorf("callback error: %w", err)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read stream: %w", err)
		}
	}

	return nil
}

// GetAvailableVoices returns list of available voices
func (s *TTSService) GetAvailableVoices(ctx context.Context) ([]map[string]interface{}, error) {
	if !s.IsAvailable() {
		return nil, fmt.Errorf("TTS service not available. Set ELEVENLABS_API_KEY environment variable")
	}

	// Build URL
	url := fmt.Sprintf("%s/voices", s.baseURL)

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("xi-api-key", s.apiKey)

	// Execute request
	client := &http.Client{Timeout: s.timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ElevenLabs API error: %d - %s", resp.StatusCode, string(body))
	}

	// Parse response
	var voicesResp struct {
		Voices []struct {
			VoiceID string `json:"voice_id"`
			Name    string `json:"name"`
			Category string `json:"category,omitempty"`
		} `json:"voices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&voicesResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert to map slice
	voices := make([]map[string]interface{}, 0, len(voicesResp.Voices))
	for _, voice := range voicesResp.Voices {
		voices = append(voices, map[string]interface{}{
			"voice_id": voice.VoiceID,
			"name":     voice.Name,
			"category": voice.Category,
		})
	}

	return voices, nil
}

