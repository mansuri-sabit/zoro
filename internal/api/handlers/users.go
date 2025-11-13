package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/troikatech/calling-agent/pkg/audit"
	"github.com/troikatech/calling-agent/pkg/errors"
	"github.com/troikatech/calling-agent/pkg/middleware"
	"github.com/troikatech/calling-agent/pkg/utils"
)

type UserResponse struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	IsActive    bool   `json:"is_active"`
	LastLoginAt string `json:"last_login_at,omitempty"`
	CreatedAt   string `json:"created_at"`
}

type UpdateUserRequest struct {
	Email    string `json:"email"`
	Role     string `json:"role"`
	IsActive *bool  `json:"is_active"`
}

func (h *Handler) ListUsers(c *gin.Context) {
	// Parse pagination
	pagination := utils.ParsePagination(c)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	query := h.mongoClient.NewQuery("users").
		Select("id", "email", "role", "is_active", "last_login_at", "created_at").
		Limit(int64(pagination.Limit))

	users, err := query.Find(ctx)
	if err != nil {
		h.logger.Error("Failed to fetch users", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	// Convert to UserResponse
	userResponses := make([]UserResponse, 0, len(users))
	for _, u := range users {
		userResponses = append(userResponses, UserResponse{
			ID:          getString(u, "id"),
			Email:       getString(u, "email"),
			Role:        getString(u, "role"),
			IsActive:    getBool(u, "is_active"),
			LastLoginAt: getString(u, "last_login_at"),
			CreatedAt:   getString(u, "created_at"),
		})
	}

	c.JSON(http.StatusOK, utils.PaginatedResponse{
		Data:  userResponses,
		Page:  pagination.Page,
		Limit: pagination.Limit,
		Count: len(userResponses),
	})
}

func (h *Handler) GetUser(c *gin.Context) {
	id, _ := c.Get("id")
	idStr := id.(string)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	user, err := h.mongoClient.NewQuery("users").
		Select("id", "email", "role", "is_active", "last_login_at", "created_at").
		Eq("id", idStr).
		FindOne(ctx)

	if err != nil || user == nil {
		errors.NotFound(c, "user not found")
		return
	}

	userResponse := UserResponse{
		ID:          getString(user, "id"),
		Email:       getString(user, "email"),
		Role:        getString(user, "role"),
		IsActive:    getBool(user, "is_active"),
		LastLoginAt: getString(user, "last_login_at"),
		CreatedAt:   getString(user, "created_at"),
	}

	c.JSON(http.StatusOK, userResponse)
}

func (h *Handler) UpdateUser(c *gin.Context) {
	id, _ := c.Get("id")
	idStr := id.(string)
	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.BadRequest(c, err.Error())
		return
	}

	updates := map[string]interface{}{}
	if req.Email != "" {
		updates["email"] = middleware.SanitizeString(req.Email)
	}
	if req.Role != "" {
		// Validate role
		validRoles := map[string]bool{
			"admin": true, "manager": true, "operator": true,
			"viewer": true, "auditor": true,
		}
		if !validRoles[req.Role] {
			errors.BadRequest(c, "invalid role")
			return
		}
		updates["role"] = req.Role
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}

	if len(updates) == 0 {
		errors.BadRequest(c, "no fields to update")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	_, err := h.mongoClient.NewQuery("users").
		Eq("id", idStr).
		UpdateOne(ctx, updates)

	if err != nil {
		h.logger.Error("Failed to update user", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	// Audit log
	audit.Log(h.mongoClient, userIDStr, string(audit.ActionUpdate), "user", idStr, updates)

	c.JSON(http.StatusOK, gin.H{"message": "user updated"})
}

func (h *Handler) DeleteUser(c *gin.Context) {
	id, _ := c.Get("id")
	idStr := id.(string)
	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	// Prevent self-deletion
	if idStr == userIDStr {
		errors.BadRequest(c, "cannot delete your own account")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	_, err := h.mongoClient.NewQuery("users").
		Eq("id", idStr).
		DeleteOne(ctx)

	if err != nil {
		h.logger.Error("Failed to delete user", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	// Audit log
	audit.Log(h.mongoClient, userIDStr, string(audit.ActionDelete), "user", idStr, nil)

	c.JSON(http.StatusOK, gin.H{"message": "user deleted"})
}

func (h *Handler) ActivateUser(c *gin.Context) {
	id, _ := c.Get("id")
	idStr := id.(string)
	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	_, err := h.mongoClient.NewQuery("users").
		Eq("id", idStr).
		UpdateOne(ctx, map[string]interface{}{"is_active": true})

	if err != nil {
		h.logger.Error("Failed to activate user", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	// Audit log
	audit.Log(h.mongoClient, userIDStr, string(audit.ActionActivate), "user", idStr, nil)

	c.JSON(http.StatusOK, gin.H{"message": "user activated"})
}

func (h *Handler) DeactivateUser(c *gin.Context) {
	id, _ := c.Get("id")
	idStr := id.(string)
	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	// Prevent self-deactivation
	if idStr == userIDStr {
		errors.BadRequest(c, "cannot deactivate your own account")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	_, err := h.mongoClient.NewQuery("users").
		Eq("id", idStr).
		UpdateOne(ctx, map[string]interface{}{"is_active": false})

	if err != nil {
		h.logger.Error("Failed to deactivate user", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	// Audit log
	audit.Log(h.mongoClient, userIDStr, string(audit.ActionDeactivate), "user", idStr, nil)

	c.JSON(http.StatusOK, gin.H{"message": "user deactivated"})
}

// Helper functions to safely extract values from map
func getString(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func getBool(m map[string]interface{}, key string) bool {
	if val, ok := m[key]; ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return false
}

// ValidateUUIDParam validates UUID path parameters (exported for use in main.go)
func ValidateUUIDParam(paramName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param(paramName)
		if idStr == "" {
			errors.BadRequest(c, paramName+" parameter is required")
			c.Abort()
			return
		}

		_, err := uuid.Parse(idStr)
		if err != nil {
			errors.BadRequest(c, "invalid "+paramName+": must be a valid UUID")
			c.Abort()
			return
		}

		c.Set(paramName, idStr)
		c.Next()
	}
}
