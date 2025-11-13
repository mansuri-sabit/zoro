package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

// AnthropicProvider implements the Provider interface for Anthropic Claude
type AnthropicProvider struct {
	apiKey    string
	model     string
	maxTokens int
	timeout   time.Duration
	logger    *zap.Logger
	baseURL   string
}

// NewAnthropicProvider creates a new Anthropic provider
func NewAnthropicProvider(apiKey, model string, maxTokens int, timeout time.Duration, logger *zap.Logger) *AnthropicProvider {
	if apiKey == "" {
		return &AnthropicProvider{logger: logger}
	}

	return &AnthropicProvider{
		apiKey:    apiKey,
		model:     model,
		maxTokens: maxTokens,
		timeout:   timeout,
		logger:    logger,
		baseURL:   "https://api.anthropic.com/v1",
	}
}

// Name returns the provider name
func (p *AnthropicProvider) Name() string {
	return "anthropic"
}

// IsAvailable checks if the provider is available
func (p *AnthropicProvider) IsAvailable() bool {
	return p.apiKey != ""
}

// GenerateScript generates a script using Anthropic Claude
func (p *AnthropicProvider) GenerateScript(ctx context.Context, req *ScriptRequest) (*ScriptResponse, error) {
	if !p.IsAvailable() {
		return nil, fmt.Errorf("Anthropic provider not available")
	}

	systemPrompt := `You are a professional sales agent creating a call script.
Create a natural, conversational script that:
1. Opens with a friendly greeting
2. Introduces the company and value proposition
3. Asks for permission to continue
4. Includes compliance messaging about recording and opt-out

Keep it concise (3-5 sentences) and professional.`

	userPrompt := fmt.Sprintf(`Create a call script for:
- Industry: %s
- Value Proposition: %s
- Context: %v

Include compliance lines about:
- Call recording disclosure
- Opt-out option (Press 3)
- Privacy policy mention`, req.Industry, req.ValueProp, req.Context)

	// Prepare messages for Anthropic API
	requestBody := map[string]interface{}{
		"model":       p.model,
		"max_tokens":  p.maxTokens,
		"system":      systemPrompt,
		"messages": []map[string]interface{}{
			{
				"role":    "user",
				"content": userPrompt,
			},
		},
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	// Execute request
	client := &http.Client{Timeout: p.timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Anthropic API error: %d - %s", resp.StatusCode, string(body))
	}

	// Parse response
	var anthropicResp struct {
		Content []struct {
			Text string `json:"text"`
			Type string `json:"type"`
		} `json:"content"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&anthropicResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(anthropicResp.Content) == 0 {
		return nil, fmt.Errorf("no content in response")
	}

	scriptText := anthropicResp.Content[0].Text

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

// SummarizeCall generates a summary using Anthropic Claude
func (p *AnthropicProvider) SummarizeCall(ctx context.Context, req *SummarizeRequest) (*SummarizeResponse, error) {
	if !p.IsAvailable() {
		return nil, fmt.Errorf("Anthropic provider not available")
	}

	systemPrompt := `You are a call analytics assistant. Analyze the call recording and provide:
1. A concise summary (2-3 sentences)
2. Relevant tags for categorization
3. Key points (bullet points)
4. Sentiment analysis (positive, neutral, or negative)`

	userPrompt := fmt.Sprintf(`Summarize this call recording:
- Call SID: %s
- Recording URL: %s
%s

Provide a detailed analysis with summary, tags, key points, and sentiment.`, req.CallSID, req.RecordingURL, func() string {
		if req.Transcription != nil {
			return fmt.Sprintf("\nTranscription: %s", *req.Transcription)
		}
		return ""
	}())

	requestBody := map[string]interface{}{
		"model":      p.model,
		"max_tokens": p.maxTokens,
		"system":     systemPrompt,
		"messages": []map[string]interface{}{
			{
				"role":    "user",
				"content": userPrompt,
			},
		},
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: p.timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Anthropic API error: %d - %s", resp.StatusCode, string(body))
	}

	var anthropicResp struct {
		Content []struct {
			Text string `json:"text"`
			Type string `json:"type"`
		} `json:"content"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&anthropicResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(anthropicResp.Content) == 0 {
		return nil, fmt.Errorf("no content in response")
	}

	content := anthropicResp.Content[0].Text

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
func (p *AnthropicProvider) GenerateConversationResponse(ctx context.Context, req *ConversationRequest) (string, error) {
	if !p.IsAvailable() {
		return "", fmt.Errorf("Anthropic provider not available")
	}

	systemPrompt := "You are a helpful AI assistant for a calling agent platform. Provide concise, professional responses."

	// Build messages from conversation history
	messages := []map[string]interface{}{}

	// Add conversation history
	for _, msg := range req.ConversationHistory {
		if role, ok := msg["role"].(string); ok {
			if content, ok := msg["content"].(string); ok {
				messages = append(messages, map[string]interface{}{
					"role":    role,
					"content": content,
				})
			}
		}
	}

	// Add current user message
	messages = append(messages, map[string]interface{}{
		"role":    "user",
		"content": req.UserText,
	})

	requestBody := map[string]interface{}{
		"model":      p.model,
		"max_tokens": 500,
		"system":     systemPrompt,
		"messages":   messages,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: p.timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Anthropic API error: %d - %s", resp.StatusCode, string(body))
	}

	var anthropicResp struct {
		Content []struct {
			Text string `json:"text"`
			Type string `json:"type"`
		} `json:"content"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&anthropicResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(anthropicResp.Content) == 0 {
		return "", fmt.Errorf("no content in response")
	}

	return strings.TrimSpace(anthropicResp.Content[0].Text), nil
}

