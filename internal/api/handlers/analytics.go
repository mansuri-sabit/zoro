package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/troikatech/calling-agent/pkg/errors"
)

func (h *Handler) GetAnalyticsOverview(c *gin.Context) {
	// Get all calls from last 30 days
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	// MongoDB query with date filter
	query := h.mongoClient.NewQuery("calls").
		Select("status", "duration_sec", "cost").
		Gte("created_at", time.Now().AddDate(0, 0, -30).Format(time.RFC3339))

	calls, err := query.Find(ctx)
	if err != nil {
		h.logger.Error("Failed to fetch analytics", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	totalCalls := len(calls)
	completedCalls := 0
	noAnswer := 0
	busy := 0
	failed := 0
	totalDuration := 0
	totalCost := 0.0

	for _, call := range calls {
		status, _ := call["status"].(string)
		switch status {
		case "completed":
			completedCalls++
		case "no-answer", "noanswer":
			noAnswer++
		case "busy":
			busy++
		case "failed":
			failed++
		}

		if duration, ok := call["duration_sec"].(float64); ok {
			totalDuration += int(duration)
		}
		if cost, ok := call["cost"].(float64); ok {
			totalCost += cost
		}
	}

	avgDuration := 0.0
	if totalCalls > 0 {
		avgDuration = float64(totalDuration) / float64(totalCalls)
	}

	c.JSON(http.StatusOK, gin.H{
		"overview": map[string]interface{}{
			"total_calls":     totalCalls,
			"completed_calls": completedCalls,
			"no_answer":       noAnswer,
			"busy":            busy,
			"failed":          failed,
			"avg_duration":    avgDuration,
			"total_cost":      totalCost,
		},
		"period": "30 days",
	})
}

func (h *Handler) GetAnalyticsDrilldown(c *gin.Context) {
	campaignID := c.Query("campaign_id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	query := h.mongoClient.NewQuery("campaign_contacts").Select("*")

	if campaignID != "" {
		query = query.Eq("campaign_id", campaignID)
	}

	contacts, err := query.Find(ctx)
	if err != nil {
		h.logger.Error("Failed to fetch analytics", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	stats := map[string]int{
		"pending":   0,
		"calling":   0,
		"completed": 0,
		"failed":    0,
		"skipped":   0,
	}

	for _, contact := range contacts {
		if status, ok := contact["status"].(string); ok {
			stats[status]++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"stats":          stats,
		"total_contacts": len(contacts),
		"campaign_id":    campaignID,
	})
}
