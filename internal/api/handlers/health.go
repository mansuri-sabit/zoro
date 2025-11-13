package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type HealthResponse struct {
	Status    string            `json:"status"`
	Timestamp string            `json:"timestamp"`
	Services  map[string]string `json:"services"`
}

func (h *Handler) HealthCheck(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	services := map[string]string{
		"api":      "healthy",
		"database": "unknown",
		"redis":    "unknown",
	}

	// Check Redis connection
	if err := h.redisClient.Ping(ctx).Err(); err != nil {
		services["redis"] = "unhealthy"
	} else {
		services["redis"] = "healthy"
	}

	// Check MongoDB connection
	if err := h.mongoClient.Ping(ctx); err != nil {
		services["database"] = "unhealthy"
	} else {
		services["database"] = "healthy"
	}

	// Check AI services if enabled
	if h.cfg.FeatureAI {
		aiServices := map[string]string{}

		// Check AI manager
		if h.aiManager != nil {
			provider := h.aiManager.GetAvailableProvider()
			if provider != nil {
				aiServices["ai_provider"] = provider.Name()
			} else {
				aiServices["ai_provider"] = "unavailable"
			}
		} else {
			aiServices["ai_provider"] = "unavailable"
		}

		// Check TTS service
		if h.ttsService != nil && h.ttsService.IsAvailable() {
			aiServices["tts"] = "available"
		} else {
			aiServices["tts"] = "unavailable"
		}

		// Check STT service
		if h.sttService != nil && h.sttService.IsAvailable() {
			aiServices["stt"] = "available"
		} else {
			aiServices["stt"] = "unavailable"
		}

		// Add AI services to response
		for k, v := range aiServices {
			services[k] = v
		}
	} else {
		services["ai"] = "disabled"
	}

	overallStatus := "healthy"
	for _, status := range services {
		if status == "unhealthy" {
			overallStatus = "degraded"
			break
		}
	}

	c.JSON(http.StatusOK, HealthResponse{
		Status:    overallStatus,
		Timestamp: time.Now().Format(time.RFC3339),
		Services:  services,
	})
}
