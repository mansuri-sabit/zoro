package ai

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestOpenAIProvider_IsAvailable(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name   string
		apiKey string
		want   bool
	}{
		{
			name:   "available with api key",
			apiKey: "test-api-key",
			want:   true,
		},
		{
			name:   "not available without api key",
			apiKey: "",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewOpenAIProvider(tt.apiKey, "gpt-4o-mini", 2000, 30*time.Second, logger)
			if got := p.IsAvailable(); got != tt.want {
				t.Errorf("OpenAIProvider.IsAvailable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOpenAIProvider_Name(t *testing.T) {
	logger := zap.NewNop()
	p := NewOpenAIProvider("test-key", "gpt-4o-mini", 2000, 30*time.Second, logger)
	if got := p.Name(); got != "openai" {
		t.Errorf("OpenAIProvider.Name() = %v, want openai", got)
	}
}

func TestOpenAIProvider_IsAvailable_WithoutAPIKey(t *testing.T) {
	logger := zap.NewNop()
	p := NewOpenAIProvider("", "gpt-4o-mini", 2000, 30*time.Second, logger)
	if p.IsAvailable() {
		t.Error("OpenAIProvider.IsAvailable() = true, want false when API key is empty")
	}
}

// Note: Integration tests for actual API calls would require API keys
// and should be in a separate test file or use test fixtures

