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

type GenerateScriptRequest struct {
	PersonaID int64                  `json:"persona_id" binding:"required"`
	Context   map[string]interface{} `json:"context"`
}

type SummarizeCallRequest struct {
	CallSID      string `json:"call_sid"`
	RecordingURL string `json:"recording_url"`
}

func (h *Handler) GenerateScript(c *gin.Context) {
	start := time.Now()
	var req GenerateScriptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.BadRequest(c, err.Error())
		return
	}

	// Check if AI is enabled
	if !h.cfg.FeatureAI || h.aiManager == nil {
		errors.ErrorResponse(c, http.StatusServiceUnavailable, "Service Unavailable", "AI service is not enabled")
		return
	}

	// Extract industry and value prop from context
	industry := "general"
	if ind, ok := req.Context["industry"].(string); ok {
		industry = ind
	}

	valueProp := "our solution"
	if vp, ok := req.Context["value_prop"].(string); ok {
		valueProp = vp
	}

	// Build RAG context if persona loader is available
	var ragContext map[string]interface{}
	if h.personaLoader != nil {
		personaID := req.PersonaID
		ctx, err := h.personaLoader.BuildRAGContext(c.Request.Context(), &personaID)
		if err == nil && ctx != nil {
			ragContext = ctx
		}
	}

	// Use Go AI provider
	scriptReq := &ai.ScriptRequest{
		PersonaID: req.PersonaID,
		Context:   req.Context,
		Industry:  industry,
		ValueProp: valueProp,
	}

	scriptResp, err := h.aiManager.GenerateScript(c.Request.Context(), scriptReq)
	if err != nil {
		h.logger.Error("Failed to generate script", zap.Error(err))
		latency := time.Since(start)
		metrics.RecordRequest("/api/ai/script", false, latency)
		errors.ErrorResponse(c, http.StatusInternalServerError, "Internal Server Error", "Failed to generate script")
		return
	}

	// Build response
	result := map[string]interface{}{
		"script_blocks": scriptResp.ScriptBlocks,
		"compliance":    scriptResp.Compliance,
		"provider":      scriptResp.Provider,
		"generated_at":  time.Now().Format(time.RFC3339),
	}

	// Add RAG context if available
	if ragContext != nil {
		result["rag_context"] = ragContext
	}

	latency := time.Since(start)
	metrics.RecordRequest("/api/ai/script", true, latency)

	c.JSON(http.StatusOK, result)
}

func (h *Handler) SummarizeCall(c *gin.Context) {
	start := time.Now()
	var req SummarizeCallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.BadRequest(c, err.Error())
		return
	}

	// Check if AI is enabled
	if !h.cfg.FeatureAI || h.aiManager == nil {
		errors.ErrorResponse(c, http.StatusServiceUnavailable, "Service Unavailable", "AI service is not enabled")
		return
	}

	// Use Go AI provider
	summarizeReq := &ai.SummarizeRequest{
		CallSID:      req.CallSID,
		RecordingURL: req.RecordingURL,
		Transcription: nil, // TODO: Add transcription if available
	}

	summarizeResp, err := h.aiManager.SummarizeCall(c.Request.Context(), summarizeReq)
	if err != nil {
		h.logger.Error("Failed to summarize call", zap.Error(err))
		latency := time.Since(start)
		metrics.RecordRequest("/api/ai/summarize", false, latency)
		errors.ErrorResponse(c, http.StatusInternalServerError, "Internal Server Error", "Failed to summarize call")
		return
	}

	// Build response
	result := map[string]interface{}{
		"summary":     summarizeResp.Summary,
		"tags":        summarizeResp.Tags,
		"key_points":  summarizeResp.KeyPoints,
		"sentiment":   summarizeResp.Sentiment,
		"provider":    summarizeResp.Provider,
		"generated_at": time.Now().Format(time.RFC3339),
	}

	latency := time.Since(start)
	metrics.RecordRequest("/api/ai/summarize", true, latency)

	c.JSON(http.StatusOK, result)
}
