package ai

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"
)

// MockProvider is a mock implementation of Provider for testing
type MockProvider struct {
	name      string
	available bool
	shouldErr bool
}

func (m *MockProvider) GenerateScript(ctx context.Context, req *ScriptRequest) (*ScriptResponse, error) {
	if m.shouldErr {
		return nil, errors.New("mock error")
	}
	return &ScriptResponse{
		ScriptBlocks: []string{"Hello", "World"},
		Compliance:   []string{"Compliance 1"},
		Provider:     m.name,
	}, nil
}

func (m *MockProvider) SummarizeCall(ctx context.Context, req *SummarizeRequest) (*SummarizeResponse, error) {
	if m.shouldErr {
		return nil, errors.New("mock error")
	}
	return &SummarizeResponse{
		Summary:   "Test summary",
		Tags:      []string{"test"},
		KeyPoints: []string{"point 1"},
		Sentiment: "positive",
		Provider:  m.name,
	}, nil
}

func (m *MockProvider) GenerateConversationResponse(ctx context.Context, req *ConversationRequest) (string, error) {
	if m.shouldErr {
		return "", errors.New("mock error")
	}
	return "Test response", nil
}

func (m *MockProvider) IsAvailable() bool {
	return m.available
}

func (m *MockProvider) Name() string {
	return m.name
}

func TestManager_GetAvailableProvider(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name      string
		providers []Provider
		want      string
		wantNil   bool
	}{
		{
			name: "returns first available provider",
			providers: []Provider{
				&MockProvider{name: "provider1", available: true},
				&MockProvider{name: "provider2", available: true},
			},
			want:    "provider1",
			wantNil: false,
		},
		{
			name: "returns nil when no providers available",
			providers: []Provider{
				&MockProvider{name: "provider1", available: false},
				&MockProvider{name: "provider2", available: false},
			},
			want:    "",
			wantNil: true,
		},
		{
			name: "skips unavailable providers",
			providers: []Provider{
				&MockProvider{name: "provider1", available: false},
				&MockProvider{name: "provider2", available: true},
			},
			want:    "provider2",
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewManager(tt.providers, logger)
			got := m.GetAvailableProvider()

			if tt.wantNil {
				if got != nil {
					t.Errorf("Manager.GetAvailableProvider() = %v, want nil", got)
				}
			} else {
				if got == nil {
					t.Errorf("Manager.GetAvailableProvider() = nil, want %v", tt.want)
				} else if got.Name() != tt.want {
					t.Errorf("Manager.GetAvailableProvider() = %v, want %v", got.Name(), tt.want)
				}
			}
		})
	}
}

func TestManager_GenerateScript_WithFallback(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name      string
		providers []Provider
		wantErr   bool
		wantProvider string
	}{
		{
			name: "succeeds with first provider",
			providers: []Provider{
				&MockProvider{name: "provider1", available: true, shouldErr: false},
				&MockProvider{name: "provider2", available: true, shouldErr: false},
			},
			wantErr: false,
			wantProvider: "provider1",
		},
		{
			name: "falls back to second provider when first fails",
			providers: []Provider{
				&MockProvider{name: "provider1", available: true, shouldErr: true},
				&MockProvider{name: "provider2", available: true, shouldErr: false},
			},
			wantErr: false,
			wantProvider: "provider2",
		},
		{
			name: "fails when all providers fail",
			providers: []Provider{
				&MockProvider{name: "provider1", available: true, shouldErr: true},
				&MockProvider{name: "provider2", available: true, shouldErr: true},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewManager(tt.providers, logger)
			req := &ScriptRequest{
				PersonaID: 1,
				Context:   map[string]interface{}{},
				Industry:  "tech",
				ValueProp: "test",
			}

			resp, err := m.GenerateScript(context.Background(), req)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Manager.GenerateScript() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Manager.GenerateScript() error = %v, want nil", err)
				}
				if resp == nil {
					t.Errorf("Manager.GenerateScript() response = nil, want non-nil")
				} else if resp.Provider != tt.wantProvider {
					t.Errorf("Manager.GenerateScript() provider = %v, want %v", resp.Provider, tt.wantProvider)
				}
			}
		})
	}
}

func TestManager_SummarizeCall_WithFallback(t *testing.T) {
	logger := zap.NewNop()
	m := NewManager([]Provider{
		&MockProvider{name: "provider1", available: true, shouldErr: false},
	}, logger)

	req := &SummarizeRequest{
		CallSID:      "CA123",
		RecordingURL: "https://example.com/recording.mp3",
	}

	resp, err := m.SummarizeCall(context.Background(), req)
	if err != nil {
		t.Errorf("Manager.SummarizeCall() error = %v, want nil", err)
	}
	if resp == nil {
		t.Errorf("Manager.SummarizeCall() response = nil, want non-nil")
	}
	if resp.Summary == "" {
		t.Errorf("Manager.SummarizeCall() summary = empty, want non-empty")
	}
}

func TestManager_GenerateConversationResponse_WithFallback(t *testing.T) {
	logger := zap.NewNop()
	m := NewManager([]Provider{
		&MockProvider{name: "provider1", available: true, shouldErr: false},
	}, logger)

	req := &ConversationRequest{
		UserText:           "Hello",
		ConversationHistory: []map[string]interface{}{},
		Context:           map[string]interface{}{},
	}

	resp, err := m.GenerateConversationResponse(context.Background(), req)
	if err != nil {
		t.Errorf("Manager.GenerateConversationResponse() error = %v, want nil", err)
	}
	if resp == "" {
		t.Errorf("Manager.GenerateConversationResponse() response = empty, want non-empty")
	}
}

