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

// OpenAIProvider implements the Provider interface for OpenAI
type OpenAIProvider struct {
	apiKey    string
	model     string
	maxTokens int
	timeout   time.Duration
	logger    *zap.Logger
	baseURL   string
}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider(apiKey, model string, maxTokens int, timeout time.Duration, logger *zap.Logger) *OpenAIProvider {
	if apiKey == "" {
		return &OpenAIProvider{logger: logger}
	}

	return &OpenAIProvider{
		apiKey:    apiKey,
		model:     model,
		maxTokens: maxTokens,
		timeout:   timeout,
		logger:    logger,
		baseURL:   "https://api.openai.com/v1",
	}
}

// Name returns the provider name
func (p *OpenAIProvider) Name() string {
	return "openai"
}

// IsAvailable checks if the provider is available
func (p *OpenAIProvider) IsAvailable() bool {
	return p.apiKey != ""
}

// GenerateScript generates a script using OpenAI
func (p *OpenAIProvider) GenerateScript(ctx context.Context, req *ScriptRequest) (*ScriptResponse, error) {
	if !p.IsAvailable() {
		return nil, fmt.Errorf("OpenAI provider not available")
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

	// Prepare messages
	messages := []map[string]interface{}{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": userPrompt},
	}

	// Create request body
	requestBody := map[string]interface{}{
		"model":       p.model,
		"messages":    messages,
		"max_tokens":  p.maxTokens,
		"temperature": 0.7,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	// Execute request
	client := &http.Client{Timeout: p.timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenAI API error: %d - %s", resp.StatusCode, string(body))
	}

	// Parse response
	var openAIResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(openAIResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	scriptText := openAIResp.Choices[0].Message.Content

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

// SummarizeCall generates a summary using OpenAI
func (p *OpenAIProvider) SummarizeCall(ctx context.Context, req *SummarizeRequest) (*SummarizeResponse, error) {
	if !p.IsAvailable() {
		return nil, fmt.Errorf("OpenAI provider not available")
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

	messages := []map[string]interface{}{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": userPrompt},
	}

	requestBody := map[string]interface{}{
		"model":       p.model,
		"messages":    messages,
		"max_tokens":  p.maxTokens,
		"temperature": 0.3,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	client := &http.Client{Timeout: p.timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenAI API error: %d - %s", resp.StatusCode, string(body))
	}

	var openAIResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(openAIResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	content := openAIResp.Choices[0].Message.Content

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
// Uses persona data and documents (RAG context) if available
func (p *OpenAIProvider) GenerateConversationResponse(ctx context.Context, req *ConversationRequest) (string, error) {
	if !p.IsAvailable() {
		return "", fmt.Errorf("OpenAI provider not available")
	}

	// Build system prompt with persona and document context
	systemPrompt := "You are a helpful AI assistant for a calling agent platform. Provide concise, professional responses."

	// Add persona and document context if available
	if req.Context != nil {
		if ragContext, ok := req.Context["rag_context"].(map[string]interface{}); ok {
			// Build enhanced system prompt with persona data
			if personaData, ok := ragContext["persona_data"].(map[string]interface{}); ok && personaData != nil {
				personaInfo := ""
				
				// Extract persona name, description, tone, etc.
				if name, ok := personaData["name"].(string); ok && name != "" {
					personaInfo += fmt.Sprintf("You are acting as: %s. ", name)
				}
				if desc, ok := personaData["description"].(string); ok && desc != "" {
					personaInfo += fmt.Sprintf("Description: %s. ", desc)
				}
				if tone, ok := personaData["tone"].(string); ok && tone != "" {
					personaInfo += fmt.Sprintf("Communication tone: %s. ", tone)
				}
				if instructions, ok := personaData["instructions"].(string); ok && instructions != "" {
					personaInfo += fmt.Sprintf("Special instructions: %s. ", instructions)
				}
				
				if personaInfo != "" {
					systemPrompt = personaInfo + systemPrompt
				}
			}

			// Add document context (RAG) to system prompt
			if documentText, ok := ragContext["document_text"].(string); ok && documentText != "" {
				// Truncate if too long (OpenAI has token limits)
				maxDocLength := 3000 // ~750 tokens
				if len(documentText) > maxDocLength {
					documentText = documentText[:maxDocLength] + "..."
				}
				systemPrompt += fmt.Sprintf("\n\nRelevant information from knowledge base:\n%s\n\nUse this information to provide accurate, context-aware responses.", documentText)
			}
		}
	}

	messages := []map[string]interface{}{
		{"role": "system", "content": systemPrompt},
	}

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
		"model":       p.model,
		"messages":    messages,
		"max_tokens":  500,
		"temperature": 0.7,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	client := &http.Client{Timeout: p.timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("OpenAI API error: %d - %s", resp.StatusCode, string(body))
	}

	var openAIResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(openAIResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return strings.TrimSpace(openAIResp.Choices[0].Message.Content), nil
}

