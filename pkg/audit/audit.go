package audit

import (
	"context"
	"encoding/json"
	"time"

	"go.uber.org/zap"

	"github.com/troikatech/calling-agent/pkg/logger"
	"github.com/troikatech/calling-agent/pkg/mongo"
)

// Action represents an audit action
type Action string

const (
	ActionCreate     Action = "create"
	ActionUpdate     Action = "update"
	ActionDelete     Action = "delete"
	ActionActivate   Action = "activate"
	ActionDeactivate Action = "deactivate"
	ActionPause      Action = "pause"
	ActionResume     Action = "resume"
	ActionCancel     Action = "cancel"
	ActionLogin      Action = "login"
	ActionLogout     Action = "logout"
)

// Log logs an audit event
func Log(client *mongo.Client, userID, action, resourceType, resourceID string, metadata map[string]interface{}) error {
	if client == nil {
		logger.Log.Warn("Audit logging skipped: MongoDB client not available")
		return nil
	}

	metadataJSON, _ := json.Marshal(metadata)

	auditData := map[string]interface{}{
		"user_id":       userID,
		"action":        action,
		"resource_type": resourceType,
		"resource_id":   resourceID,
		"metadata":      string(metadataJSON),
		"created_at":    time.Now().Format(time.RFC3339),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.NewQuery("audit_log").Insert(ctx, auditData)
	if err != nil {
		logger.Log.Error("Failed to log audit event",
			zap.Error(err),
			zap.String("action", action),
			zap.String("resource_type", resourceType),
		)
		return err
	}

	return nil
}
