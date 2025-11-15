package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/joho/godotenv"
)

func main() {
	// Try to load .env file from multiple locations
	envPaths := []string{
		".env",                    // Current directory
		"../.env",                 // Parent directory
		"../../.env",              // Backend root
		filepath.Join("backend", ".env"), // If running from project root
	}

	loaded := false
	for _, path := range envPaths {
		if err := godotenv.Load(path); err == nil {
			loaded = true
			break
		}
	}

	if loaded {
		fmt.Println("✅ Loaded .env file")
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Println("❌ ERROR: OPENAI_API_KEY environment variable not set")
		fmt.Println("Set it with: export OPENAI_API_KEY=your-key-here")
		os.Exit(1)
	}

	fmt.Println("🔍 Testing OpenAI API Integration...")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// Test 1: Check API Key format
	fmt.Println("1️⃣  Checking API Key format...")
	if len(apiKey) < 20 {
		fmt.Println("   ⚠️  WARNING: API key seems too short")
	} else {
		fmt.Printf("   ✅ API Key found (length: %d)\n", len(apiKey))
		if apiKey[:3] == "sk-" {
			fmt.Println("   ✅ API Key format looks correct (starts with 'sk-')")
		} else {
			fmt.Println("   ⚠️  WARNING: API key doesn't start with 'sk-'")
		}
	}
	fmt.Println()

	// Test 2: Test Chat Completions API (gpt-4o-mini)
	fmt.Println("2️⃣  Testing Chat Completions API (gpt-4o-mini)...")
	if err := testChatCompletions(apiKey); err != nil {
		fmt.Printf("   ❌ FAILED: %v\n", err)
	} else {
		fmt.Println("   ✅ SUCCESS: Chat Completions API working")
	}
	fmt.Println()

	// Test 3: Test TTS API (tts-1-hd)
	fmt.Println("3️⃣  Testing Text-to-Speech API (tts-1-hd, shimmer)...")
	if err := testTTS(apiKey); err != nil {
		fmt.Printf("   ❌ FAILED: %v\n", err)
	} else {
		fmt.Println("   ✅ SUCCESS: TTS API working")
	}
	fmt.Println()

	// Test 4: Test STT API (whisper-1) - Skip for now as it needs audio file
	fmt.Println("4️⃣  Testing Speech-to-Text API (whisper-1)...")
	fmt.Println("   ⏭️  SKIPPED: Requires audio file (can test manually)")
	fmt.Println()

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("✅ OpenAI API Integration Test Complete!")
	fmt.Println()
	fmt.Println("📝 Summary:")
	fmt.Println("   - API Key: ✅ Found")
	fmt.Println("   - Chat Completions (gpt-4o-mini): Check above")
	fmt.Println("   - TTS (tts-1-hd): Check above")
	fmt.Println("   - STT (whisper-1): Manual test required")
}

func testChatCompletions(apiKey string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	requestBody := map[string]interface{}{
		"model": "gpt-4o-mini",
		"messages": []map[string]interface{}{
			{
				"role":    "system",
				"content": "You are a helpful assistant. Reply with just 'OK' to confirm you're working.",
			},
			{
				"role":    "user",
				"content": "Say OK if you can hear me.",
			},
		},
		"max_tokens": 10,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if len(result.Choices) == 0 {
		return fmt.Errorf("no choices in response")
	}

	fmt.Printf("   📤 Response: %s\n", result.Choices[0].Message.Content)
	return nil
}

func testTTS(apiKey string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	requestBody := map[string]interface{}{
		"model": "tts-1-hd",
		"input":  "Hello, this is a test of OpenAI TTS API.",
		"voice":  "shimmer",
		"response_format": "pcm",
		"speed":  1.0,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/audio/speech", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	audioData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read audio data: %w", err)
	}

	if len(audioData) == 0 {
		return fmt.Errorf("no audio data received")
	}

	fmt.Printf("   📤 Received %d bytes of PCM audio (24kHz)\n", len(audioData))
	fmt.Printf("   ✅ TTS working correctly")
	return nil
}

