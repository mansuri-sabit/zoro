package handlers

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/troikatech/calling-agent/pkg/ai"
	"github.com/troikatech/calling-agent/pkg/errors"
	"github.com/troikatech/calling-agent/pkg/metrics"
)

// SpeechToText converts speech to text
func (h *Handler) SpeechToText(c *gin.Context) {
	start := time.Now()

	// Check if STT is enabled
	if !h.cfg.FeatureAI || h.sttService == nil || !h.sttService.IsAvailable() {
		errors.ErrorResponse(c, http.StatusServiceUnavailable, "Service Unavailable", "STT service is not available")
		return
	}

	// Get audio file from multipart form
	file, err := c.FormFile("file")
	if err != nil {
		errors.BadRequest(c, "file is required")
		return
	}

	// Open file
	src, err := file.Open()
	if err != nil {
		h.logger.Error("Failed to open file", zap.Error(err))
		latency := time.Since(start)
		metrics.RecordRequest("/api/ai/stt", false, latency)
		errors.ErrorResponse(c, http.StatusInternalServerError, "Internal Server Error", "Failed to open file")
		return
	}
	defer src.Close()

	// Read audio data
	audioData, err := io.ReadAll(src)
	if err != nil {
		h.logger.Error("Failed to read file", zap.Error(err))
		latency := time.Since(start)
		metrics.RecordRequest("/api/ai/stt", false, latency)
		errors.ErrorResponse(c, http.StatusInternalServerError, "Internal Server Error", "Failed to read file")
		return
	}

	// Get language (optional)
	language := c.PostForm("language")

	// Get prompt (optional)
	prompt := c.PostForm("prompt")

	// Detect audio format from filename
	audioFormat := "mp3" // Default
	if file.Filename != "" {
		parts := strings.Split(file.Filename, ".")
		if len(parts) > 1 {
			audioFormat = strings.ToLower(parts[len(parts)-1])
		}
	}

	// Use Go STT service
	sttReq := &ai.STTRequest{
		AudioData:   audioData,
		AudioFormat: audioFormat,
		Language:    language,
		Prompt:      prompt,
	}

	sttResp, err := h.sttService.SpeechToText(c.Request.Context(), sttReq)
	if err != nil {
		h.logger.Error("STT service failed", zap.Error(err))
		latency := time.Since(start)
		metrics.RecordRequest("/api/ai/stt", false, latency)
		errors.ErrorResponse(c, http.StatusInternalServerError, "Internal Server Error", "Failed to transcribe speech")
		return
	}

	latency := time.Since(start)
	metrics.RecordRequest("/api/ai/stt", true, latency)

	// Build response
	result := map[string]interface{}{
		"text":     sttResp.Text,
		"language": sttResp.Language,
	}

	if sttResp.Duration != nil {
		result["duration"] = *sttResp.Duration
	}

	c.JSON(http.StatusOK, result)
}

