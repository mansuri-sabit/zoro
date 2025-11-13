package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/troikatech/calling-agent/pkg/client"
	"github.com/troikatech/calling-agent/pkg/errors"
	"github.com/troikatech/calling-agent/pkg/metrics"
)

type CreateCallRequest struct {
	From   string `json:"from" binding:"required"`
	To     string `json:"to" binding:"required"`
	FlowID string `json:"flow_id" binding:"required"`
}

func (h *Handler) CreateCall(c *gin.Context) {
	start := time.Now()
	var req CreateCallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.BadRequest(c, err.Error())
		return
	}

	dialerReq := map[string]interface{}{
		"from":      req.From,
		"to":        req.To,
		"flow_id":   req.FlowID,
		"caller_id": h.cfg.ExotelExophone,
	}

	httpClient := client.NewHTTPClient("dialer-service", 10*time.Second)
	resp, err := httpClient.Post(c.Request.Context(), "http://dialer:8090/internal/calls/initiate", dialerReq)

	if err != nil {
		h.logger.Error("Failed to initiate call", zap.Error(err))
		latency := time.Since(start)
		metrics.RecordRequest("/api/calls", false, latency)
		errors.InternalError(c, err, h.logger)
		return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	latency := time.Since(start)
	success := resp.StatusCode == http.StatusOK
	metrics.RecordRequest("/api/calls", success, latency)

	if resp.StatusCode != http.StatusOK {
		errors.ErrorResponse(c, resp.StatusCode, "Call Initiation Failed", fmt.Sprintf("%v", result["error"]))
		return
	}

	c.JSON(http.StatusOK, result)
}

func (h *Handler) GetCall(c *gin.Context) {
	callSID := c.Param("call_sid")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	call, err := h.mongoClient.NewQuery("calls").
		Select("*").
		Eq("call_sid", callSID).
		FindOne(ctx)

	if err != nil || call == nil {
		errors.NotFound(c, "call not found")
		return
	}

	c.JSON(http.StatusOK, call)
}

func (h *Handler) GetRecording(c *gin.Context) {
	callSID := c.Param("call_sid")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	call, err := h.mongoClient.NewQuery("calls").
		Select("recording_url").
		Eq("call_sid", callSID).
		FindOne(ctx)

	if err != nil || call == nil {
		errors.NotFound(c, "recording not found")
		return
	}

	recordingURL, ok := call["recording_url"].(string)
	if !ok || recordingURL == "" {
		errors.NotFound(c, "recording not available")
		return
	}

	c.Redirect(http.StatusFound, recordingURL)
}
