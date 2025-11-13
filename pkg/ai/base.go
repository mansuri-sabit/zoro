package ai

import (
	"context"
)

// Provider is the base interface for all AI providers
type Provider interface {
	// GenerateScript generates a call script based on persona and context
	GenerateScript(ctx context.Context, req *ScriptRequest) (*ScriptResponse, error)

	// SummarizeCall generates a summary from call recording
	SummarizeCall(ctx context.Context, req *SummarizeRequest) (*SummarizeResponse, error)

	// GenerateConversationResponse generates a conversational response
	GenerateConversationResponse(ctx context.Context, req *ConversationRequest) (string, error)

	// IsAvailable checks if the provider is available/configured
	IsAvailable() bool

	// Name returns the provider name
	Name() string
}

// ScriptRequest represents a script generation request
type ScriptRequest struct {
	PersonaID int64
	Context   map[string]interface{}
	Industry  string
	ValueProp string
}

// ScriptResponse represents a script generation response
type ScriptResponse struct {
	ScriptBlocks []string `json:"script_blocks"`
	Compliance   []string `json:"compliance"`
	Provider     string   `json:"provider"`
}

// SummarizeRequest represents a call summarization request
type SummarizeRequest struct {
	CallSID      string
	RecordingURL string
	Transcription *string
}

// SummarizeResponse represents a call summarization response
type SummarizeResponse struct {
	Summary    string   `json:"summary"`
	Tags       []string `json:"tags"`
	KeyPoints  []string `json:"key_points"`
	Sentiment  string   `json:"sentiment"`
	Provider   string   `json:"provider"`
}

// ConversationRequest represents a conversation request
type ConversationRequest struct {
	UserText           string
	PersonaID          *int64
	ConversationHistory []map[string]interface{}
	Context           map[string]interface{}
}

