package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
)

// GeminiProvider implements the Provider interface for Google Gemini
type GeminiProvider struct {
	apiKey    string
	model     string
	timeout   time.Duration
	logger    *zap.Logger
	baseURL   string
}

// NewGeminiProvider creates a new Gemini provider
func NewGeminiProvider(apiKey, model string, timeout time.Duration, logger *zap.Logger) *GeminiProvider {
	if apiKey == "" {
		return &GeminiProvider{logger: logger}
	}

	return &GeminiProvider{
		apiKey:  apiKey,
		model:   model,
		timeout: timeout,
		logger:  logger,
		baseURL: "https://generativelanguage.googleapis.com/v1beta",
	}
}

// Name returns the provider name
func (p *GeminiProvider) Name() string {
	return "gemini"
}

// IsAvailable checks if the provider is available
func (p *GeminiProvider) IsAvailable() bool {
	return p.apiKey != ""
}

// GenerateScript generates a script using Gemini
func (p *GeminiProvider) GenerateScript(ctx context.Context, req *ScriptRequest) (*ScriptResponse, error) {
	if !p.IsAvailable() {
		return nil, fmt.Errorf("Gemini provider not available")
	}

	prompt := fmt.Sprintf(`Create a professional sales call script for:
- Industry: %s
- Value Proposition: %s
- Context: %v

Requirements:
1. Friendly opening greeting
2. Company introduction
3. Value proposition
4. Permission to continue
5. Keep it concise (3-5 sentences)

Also include compliance messaging about:
- Call recording disclosure
- Opt-out option (Press 3)
- Privacy policy`, req.Industry, req.ValueProp, req.Context)

	// Prepare request body for Gemini API
	requestBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]interface{}{
					{
						"text": prompt,
					},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature": 0.7,
			"topK":        40,
			"topP":        0.95,
			"maxOutputTokens": 2000,
		},
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Build URL with API key
	apiURL := fmt.Sprintf("%s/models/%s:generateContent?key=%s", p.baseURL, p.model, url.QueryEscape(p.apiKey))

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Execute request
	client := &http.Client{Timeout: p.timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Gemini API error: %d - %s", resp.StatusCode, string(body))
	}

	// Parse response
	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("no candidates in response")
	}

	scriptText := geminiResp.Candidates[0].Content.Parts[0].Text

	// Parse script into blocks
	scriptBlocks := []string{}
	for _, block := range strings.Split(scriptText, ".") {
		block = strings.TrimSpace(block)
		if block != "" {
			scriptBlocks = append(scriptBlocks, block)
		}
	}

	if len(scriptBlocks) == 0 {
		scriptBlocks = []string{scriptText}
	}

	compliance := []string{
		"This call may be recorded for quality and training purposes.",
		"Press 3 at any time to opt out of future calls.",
		"Your information is kept confidential per our privacy policy.",
	}

	return &ScriptResponse{
		ScriptBlocks: scriptBlocks,
		Compliance:   compliance,
		Provider:     p.Name(),
	}, nil
}

// SummarizeCall generates a summary using Gemini
func (p *GeminiProvider) SummarizeCall(ctx context.Context, req *SummarizeRequest) (*SummarizeResponse, error) {
	if !p.IsAvailable() {
		return nil, fmt.Errorf("Gemini provider not available")
	}

	prompt := fmt.Sprintf(`Summarize this call recording:
- Call SID: %s
- Recording URL: %s
%s

Provide a detailed analysis with:
1. A concise summary (2-3 sentences)
2. Relevant tags for categorization
3. Key points (bullet points)
4. Sentiment analysis (positive, neutral, or negative)`, req.CallSID, req.RecordingURL, func() string {
		if req.Transcription != nil {
			return fmt.Sprintf("\nTranscription: %s", *req.Transcription)
		}
		return ""
	}())

	requestBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]interface{}{
					{
						"text": prompt,
					},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature": 0.3,
			"topK":        40,
			"topP":        0.95,
			"maxOutputTokens": 2000,
		},
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	apiURL := fmt.Sprintf("%s/models/%s:generateContent?key=%s", p.baseURL, p.model, url.QueryEscape(p.apiKey))

	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: p.timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Gemini API error: %d - %s", resp.StatusCode, string(body))
	}

	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("no candidates in response")
	}

	content := geminiResp.Candidates[0].Content.Parts[0].Text

	// Parse response (simplified - in production, use structured output or parse JSON)
	summary := content
	tags := []string{"call", "summary"}
	keyPoints := strings.Split(content, "\n")
	sentiment := "neutral"

	// Try to extract structured data from response
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.ToLower(strings.TrimSpace(line))
		if strings.Contains(line, "sentiment") {
			if strings.Contains(line, "positive") {
				sentiment = "positive"
			} else if strings.Contains(line, "negative") {
				sentiment = "negative"
			}
		}
	}

	return &SummarizeResponse{
		Summary:   summary,
		Tags:      tags,
		KeyPoints: keyPoints,
		Sentiment: sentiment,
		Provider:  p.Name(),
	}, nil
}

// GenerateConversationResponse generates a conversational response
func (p *GeminiProvider) GenerateConversationResponse(ctx context.Context, req *ConversationRequest) (string, error) {
	if !p.IsAvailable() {
		return "", fmt.Errorf("Gemini provider not available")
	}

	// Build conversation history
	contents := []map[string]interface{}{}

	// Add conversation history
	for _, msg := range req.ConversationHistory {
		if role, ok := msg["role"].(string); ok {
			if content, ok := msg["content"].(string); ok {
				contents = append(contents, map[string]interface{}{
					"role": role,
					"parts": []map[string]interface{}{
						{
							"text": content,
						},
					},
				})
			}
		}
	}

	// Add current user message
	contents = append(contents, map[string]interface{}{
		"role": "user",
		"parts": []map[string]interface{}{
			{
				"text": req.UserText,
			},
		},
	})

	requestBody := map[string]interface{}{
		"contents": contents,
		"generationConfig": map[string]interface{}{
			"temperature": 0.7,
			"topK":        40,
			"topP":        0.95,
			"maxOutputTokens": 500,
		},
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	apiURL := fmt.Sprintf("%s/models/%s:generateContent?key=%s", p.baseURL, p.model, url.QueryEscape(p.apiKey))

	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: p.timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Gemini API error: %d - %s", resp.StatusCode, string(body))
	}

	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("no candidates in response")
	}

	return strings.TrimSpace(geminiResp.Candidates[0].Content.Parts[0].Text), nil
}

