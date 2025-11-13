package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// STTService handles Speech-to-Text using OpenAI Whisper
type STTService struct {
	apiKey          string
	defaultModel    string
	defaultLanguage string
	timeout         time.Duration
	logger          *zap.Logger
	baseURL         string
}

// NewSTTService creates a new STT service
func NewSTTService(apiKey, model, language string, timeout time.Duration, logger *zap.Logger) *STTService {
	if apiKey == "" {
		return &STTService{logger: logger}
	}

	return &STTService{
		apiKey:          apiKey,
		defaultModel:    model,
		defaultLanguage: language,
		timeout:         timeout,
		logger:          logger,
		baseURL:         "https://api.openai.com/v1",
	}
}

// IsAvailable checks if STT service is available
func (s *STTService) IsAvailable() bool {
	return s.apiKey != ""
}

// STTRequest represents a STT request
type STTRequest struct {
	AudioData  []byte
	AudioFormat string
	Language   string
	Prompt     string
}

// STTResponse represents a STT response
type STTResponse struct {
	Text     string
	Language string
	Duration *float64
}

// SpeechToText converts speech audio to text
func (s *STTService) SpeechToText(ctx context.Context, req *STTRequest) (*STTResponse, error) {
	if !s.IsAvailable() {
		return nil, fmt.Errorf("STT service not available. Set OPENAI_API_KEY environment variable")
	}

	if len(req.AudioData) == 0 {
		return nil, fmt.Errorf("audio data cannot be empty")
	}

	// Use defaults if not provided
	model := s.defaultModel
	if model == "" {
		model = "whisper-1" // Default model
	}

	audioFormat := req.AudioFormat
	if audioFormat == "" {
		audioFormat = "mp3" // Default format
	}

	// Create multipart form
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	// Add file
	part, err := writer.CreateFormFile("file", fmt.Sprintf("audio.%s", audioFormat))
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := part.Write(req.AudioData); err != nil {
		return nil, fmt.Errorf("failed to write audio data: %w", err)
	}

	// Add model
	if err := writer.WriteField("model", model); err != nil {
		return nil, fmt.Errorf("failed to write model field: %w", err)
	}

	// Add language if provided
	if req.Language != "" {
		if err := writer.WriteField("language", req.Language); err != nil {
			return nil, fmt.Errorf("failed to write language field: %w", err)
		}
	} else if s.defaultLanguage != "" {
		if err := writer.WriteField("language", s.defaultLanguage); err != nil {
			return nil, fmt.Errorf("failed to write language field: %w", err)
		}
	}

	// Add prompt if provided
	if req.Prompt != "" {
		if err := writer.WriteField("prompt", req.Prompt); err != nil {
			return nil, fmt.Errorf("failed to write prompt field: %w", err)
		}
	}

	// Add response format
	if err := writer.WriteField("response_format", "json"); err != nil {
		return nil, fmt.Errorf("failed to write response_format field: %w", err)
	}

	writer.Close()

	// Build URL
	url := fmt.Sprintf("%s/audio/transcriptions", s.baseURL)

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, &requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", writer.FormDataContentType())
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
		return nil, fmt.Errorf("OpenAI Whisper API error: %d - %s", resp.StatusCode, string(body))
	}

	// Parse response
	var whisperResp struct {
		Text     string   `json:"text"`
		Language string   `json:"language"`
		Duration *float64 `json:"duration"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&whisperResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Use detected language if not provided
	language := whisperResp.Language
	if language == "" {
		language = req.Language
		if language == "" {
			language = "unknown"
		}
	}

	return &STTResponse{
		Text:     whisperResp.Text,
		Language: language,
		Duration: whisperResp.Duration,
	}, nil
}

