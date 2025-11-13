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

type TTSRequest struct {
	Text            string  `json:"text" binding:"required"`
	VoiceID         string  `json:"voice_id"`
	ModelID         string  `json:"model_id"`
	OutputFormat    string  `json:"output_format"`
	Stability       float64 `json:"stability"`
	SimilarityBoost float64 `json:"similarity_boost"`
	Stream          bool    `json:"stream"`
}

// TextToSpeech converts text to speech
func (h *Handler) TextToSpeech(c *gin.Context) {
	start := time.Now()
	var req TTSRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.BadRequest(c, err.Error())
		return
	}

	// Check if TTS is enabled
	if !h.cfg.FeatureAI || h.ttsService == nil || !h.ttsService.IsAvailable() {
		errors.ErrorResponse(c, http.StatusServiceUnavailable, "Service Unavailable", "TTS service is not available")
		return
	}

	// Use Go TTS service
	ttsReq := &ai.TTSRequest{
		Text:            req.Text,
		VoiceID:         req.VoiceID,
		ModelID:         req.ModelID,
		OutputFormat:    req.OutputFormat,
		Stability:       req.Stability,
		SimilarityBoost: req.SimilarityBoost,
	}

	if req.Stream {
		// Stream audio chunks
		c.Header("Content-Type", "audio/mpeg")
		c.Header("Content-Disposition", "inline; filename=speech.mp3")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Status(http.StatusOK)

		err := h.ttsService.TextToSpeechStream(c.Request.Context(), ttsReq, func(chunk []byte) error {
			_, err := c.Writer.Write(chunk)
			c.Writer.Flush()
			return err
		})

		if err != nil {
			h.logger.Error("TTS streaming failed", zap.Error(err))
			latency := time.Since(start)
			metrics.RecordRequest("/api/ai/tts", false, latency)
			return
		}

		latency := time.Since(start)
		metrics.RecordRequest("/api/ai/tts", true, latency)
		return
	}

	// Return complete audio file
	audioData, err := h.ttsService.TextToSpeech(c.Request.Context(), ttsReq)
	if err != nil {
		h.logger.Error("TTS service failed", zap.Error(err))
		latency := time.Since(start)
		metrics.RecordRequest("/api/ai/tts", false, latency)
		errors.ErrorResponse(c, http.StatusInternalServerError, "Internal Server Error", "Failed to generate speech")
		return
	}

	latency := time.Since(start)
	metrics.RecordRequest("/api/ai/tts", true, latency)

	// Return audio file
	c.Header("Content-Type", "audio/mpeg")
	c.Header("Content-Disposition", "inline; filename=speech.mp3")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "audio/mpeg", audioData)
}

// GetTTSVoices returns available TTS voices
func (h *Handler) GetTTSVoices(c *gin.Context) {
	start := time.Now()

	// Check if TTS is enabled
	if !h.cfg.FeatureAI || h.ttsService == nil || !h.ttsService.IsAvailable() {
		errors.ErrorResponse(c, http.StatusServiceUnavailable, "Service Unavailable", "TTS service is not available")
		return
	}

	// Get available voices
	voices, err := h.ttsService.GetAvailableVoices(c.Request.Context())
	if err != nil {
		h.logger.Error("Failed to get TTS voices", zap.Error(err))
		latency := time.Since(start)
		metrics.RecordRequest("/api/ai/tts/voices", false, latency)
		errors.ErrorResponse(c, http.StatusInternalServerError, "Internal Server Error", "Failed to get voices")
		return
	}

	latency := time.Since(start)
	metrics.RecordRequest("/api/ai/tts/voices", true, latency)

	result := map[string]interface{}{
		"voices": voices,
		"count":  len(voices),
	}

	c.JSON(http.StatusOK, result)
}

