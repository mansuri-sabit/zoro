package ai

import (
	"context"
	"fmt"
	"go.uber.org/zap"
)

// Manager manages AI providers with fallback logic
type Manager struct {
	providers []Provider
	logger    *zap.Logger
}

// NewManager creates a new AI provider manager
func NewManager(providers []Provider, logger *zap.Logger) *Manager {
	return &Manager{
		providers: providers,
		logger:    logger,
	}
}

// GetAvailableProvider returns the first available provider
func (m *Manager) GetAvailableProvider() Provider {
	for _, provider := range m.providers {
		if provider.IsAvailable() {
			return provider
		}
	}
	return nil
}

// ExecuteWithFallback executes a method on providers with fallback logic
func (m *Manager) ExecuteWithFallback(
	ctx context.Context,
	method func(Provider, context.Context) (interface{}, error),
) (interface{}, error) {
	if len(m.providers) == 0 {
		return nil, fmt.Errorf("no AI providers available")
	}

	var lastErr error
	for _, provider := range m.providers {
		if !provider.IsAvailable() {
			continue
		}

		result, err := method(provider, ctx)
		if err == nil {
			m.logger.Info("Successfully used AI provider",
				zap.String("provider", provider.Name()),
			)
			return result, nil
		}

		lastErr = err
		m.logger.Warn("AI provider failed, trying next",
			zap.String("provider", provider.Name()),
			zap.Error(err),
		)
	}

	return nil, fmt.Errorf("all AI providers failed. Last error: %w", lastErr)
}

// GenerateScript generates a script with fallback
func (m *Manager) GenerateScript(ctx context.Context, req *ScriptRequest) (*ScriptResponse, error) {
	result, err := m.ExecuteWithFallback(ctx, func(provider Provider, ctx context.Context) (interface{}, error) {
		return provider.GenerateScript(ctx, req)
	})

	if err != nil {
		return nil, err
	}

	return result.(*ScriptResponse), nil
}

// SummarizeCall summarizes a call with fallback
func (m *Manager) SummarizeCall(ctx context.Context, req *SummarizeRequest) (*SummarizeResponse, error) {
	result, err := m.ExecuteWithFallback(ctx, func(provider Provider, ctx context.Context) (interface{}, error) {
		return provider.SummarizeCall(ctx, req)
	})

	if err != nil {
		return nil, err
	}

	return result.(*SummarizeResponse), nil
}

// GenerateConversationResponse generates a conversation response with fallback
func (m *Manager) GenerateConversationResponse(ctx context.Context, req *ConversationRequest) (string, error) {
	result, err := m.ExecuteWithFallback(ctx, func(provider Provider, ctx context.Context) (interface{}, error) {
		return provider.GenerateConversationResponse(ctx, req)
	})

	if err != nil {
		return "", err
	}

	return result.(string), nil
}

