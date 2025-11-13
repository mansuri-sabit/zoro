package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/troikatech/calling-agent/pkg/ai"
	"github.com/troikatech/calling-agent/pkg/errors"
	"github.com/troikatech/calling-agent/pkg/metrics"
)

type ConversationRequest struct {
	UserText           string                   `json:"user_text" binding:"required"`
	PersonaID          *int64                   `json:"persona_id"`
	ConversationHistory []map[string]interface{} `json:"conversation_history"`
	Context           map[string]interface{}   `json:"context"`
}

// Conversation generates a conversational response
func (h *Handler) Conversation(c *gin.Context) {
	start := time.Now()
	var req ConversationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.BadRequest(c, err.Error())
		return
	}

	// Check if AI is enabled
	if !h.cfg.FeatureAI || h.aiManager == nil {
		errors.ErrorResponse(c, http.StatusServiceUnavailable, "Service Unavailable", "AI service is not enabled")
		return
	}

	// Build RAG context if persona loader is available
	var ragContext map[string]interface{}
	if h.personaLoader != nil && req.PersonaID != nil {
		ctx, err := h.personaLoader.BuildRAGContext(c.Request.Context(), req.PersonaID)
		if err == nil && ctx != nil {
			ragContext = ctx
		}
	}

	// Build context for AI request
	aiContext := req.Context
	if aiContext == nil {
		aiContext = map[string]interface{}{}
	}

	// Add RAG context if available
	if ragContext != nil {
		aiContext["rag_context"] = ragContext
	}

	// Use Go AI manager
	conversationReq := &ai.ConversationRequest{
		UserText:           req.UserText,
		PersonaID:          req.PersonaID,
		ConversationHistory: req.ConversationHistory,
		Context:           aiContext,
	}

	response, err := h.aiManager.GenerateConversationResponse(c.Request.Context(), conversationReq)
	if err != nil {
		h.logger.Error("Failed to generate conversation response", zap.Error(err))
		latency := time.Since(start)
		metrics.RecordRequest("/api/ai/conversation", false, latency)
		errors.ErrorResponse(c, http.StatusInternalServerError, "Internal Server Error", "Failed to generate conversation response")
		return
	}

	latency := time.Since(start)
	metrics.RecordRequest("/api/ai/conversation", true, latency)

	result := map[string]interface{}{
		"response":    response,
		"generated_at": time.Now().Format(time.RFC3339),
	}

	c.JSON(http.StatusOK, result)
}

