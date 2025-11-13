package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/troikatech/calling-agent/pkg/errors"
	"github.com/troikatech/calling-agent/pkg/utils"
)

type AuditLogResponse struct {
	ID           string                 `json:"id"`
	UserID       string                 `json:"user_id"`
	Action       string                 `json:"action"`
	ResourceType string                 `json:"resource_type"`
	ResourceID   string                 `json:"resource_id"`
	Metadata     map[string]interface{} `json:"metadata"`
	CreatedAt    string                 `json:"created_at"`
}

func (h *Handler) ListAuditLogs(c *gin.Context) {
	// Parse pagination
	pagination := utils.ParsePagination(c)

	// Parse filters
	userID := c.Query("user_id")
	action := c.Query("action")
	resourceType := c.Query("resource_type")
	resourceID := c.Query("resource_id")
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	query := h.mongoClient.NewQuery("audit_log").Select("*")

	if userID != "" {
		query = query.Eq("user_id", userID)
	}
	if action != "" {
		query = query.Eq("action", action)
	}
	if resourceType != "" {
		query = query.Eq("resource_type", resourceType)
	}
	if resourceID != "" {
		query = query.Eq("resource_id", resourceID)
	}

	// Date filtering
	if startDate != "" || endDate != "" {
		if startDate != "" {
			query = query.Gte("created_at", startDate)
		}
		if endDate != "" {
			query = query.Lte("created_at", endDate)
		}
	} else {
		// Default to last 30 days
		query = query.Gte("created_at", time.Now().AddDate(0, 0, -30).Format(time.RFC3339))
	}

	// Sort by created_at descending
	query = query.Sort("created_at", false)

	// Apply pagination
	query = query.Limit(int64(pagination.Limit))

	logs, err := query.Find(ctx)

	if err != nil {
		h.logger.Error("Failed to fetch audit logs", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	// Parse metadata JSON strings
	var auditLogs []AuditLogResponse
	for _, log := range logs {
		metadataStr, _ := log["metadata"].(string)
		var metadata map[string]interface{}
		json.Unmarshal([]byte(metadataStr), &metadata)

		auditLogs = append(auditLogs, AuditLogResponse{
			ID:           log["id"].(string),
			UserID:       log["user_id"].(string),
			Action:       log["action"].(string),
			ResourceType: log["resource_type"].(string),
			ResourceID:   log["resource_id"].(string),
			Metadata:     metadata,
			CreatedAt:    log["created_at"].(string),
		})
	}

	c.JSON(http.StatusOK, utils.PaginatedResponse{
		Data:  auditLogs,
		Page:  pagination.Page,
		Limit: pagination.Limit,
		Count: len(auditLogs),
	})
}
