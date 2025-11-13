package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/troikatech/calling-agent/pkg/auth"
	"github.com/troikatech/calling-agent/pkg/errors"
)

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type RegisterRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
	Role     string `json:"role"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type AuthResponse struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	User         UserInfo  `json:"user"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type UserInfo struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

func (h *Handler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.BadRequest(c, err.Error())
		return
	}

	// Fetch user with password_hash
	user, err := h.mongoClient.NewQuery("users").
		Select("id", "email", "role", "password_hash", "is_active").
		Eq("email", req.Email).
		FindOne(c.Request.Context())

	if err != nil || user == nil {
		errors.Unauthorized(c, "invalid credentials")
		return
	}

	// Check if user is active
	if isActive, ok := user["is_active"].(bool); ok && !isActive {
		errors.Forbidden(c, "account is inactive")
		return
	}

	// Verify password with bcrypt
	passwordHash, _ := user["password_hash"].(string)
	if err := auth.VerifyPassword(passwordHash, req.Password); err != nil {
		errors.Unauthorized(c, "invalid credentials")
		return
	}

	userID, _ := user["id"].(string)
	userEmail, _ := user["email"].(string)
	userRole, _ := user["role"].(string)

	// Generate access token
	accessToken, expiresAt, err := auth.GenerateAccessToken(
		userID,
		userEmail,
		userRole,
		h.cfg.JWTSecret,
		h.cfg.JWTIssuer,
		h.cfg.JWTAudience,
		h.cfg.AccessTTLMin,
	)
	if err != nil {
		h.logger.Error("Failed to generate access token", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	// Generate refresh token
	refreshToken, err := auth.GenerateRefreshToken()
	if err != nil {
		h.logger.Error("Failed to generate refresh token", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	// Store refresh token
	if err := auth.StoreRefreshToken(h.mongoClient, userID, refreshToken, h.cfg.RefreshTTLDays); err != nil {
		h.logger.Error("Failed to store refresh token", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	// Update last_login_at
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	h.mongoClient.NewQuery("users").
		Eq("id", userID).
		UpdateOne(ctx, map[string]interface{}{
			"last_login_at": time.Now().Format(time.RFC3339),
		})

	c.JSON(http.StatusOK, AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		User: UserInfo{
			ID:    userID,
			Email: userEmail,
			Role:  userRole,
		},
		ExpiresAt: expiresAt,
	})
}

func (h *Handler) Register(c *gin.Context) {
	// Check if self-registration is allowed
	if !h.cfg.AllowSelfRegister {
		errors.Forbidden(c, "self-registration is disabled")
		return
	}

	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.BadRequest(c, err.Error())
		return
	}

	if req.Role == "" {
		req.Role = "viewer"
	}

	// Validate role
	validRoles := map[string]bool{"viewer": true, "operator": true}
	if !validRoles[req.Role] {
		errors.BadRequest(c, "invalid role for self-registration")
		return
	}

	// Check if user already exists
	existingUser, _ := h.mongoClient.NewQuery("users").
		Select("id").
		Eq("email", req.Email).
		FindOne(c.Request.Context())

	if existingUser != nil {
		errors.Conflict(c, "email already registered")
		return
	}

	// Hash password
	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		h.logger.Error("Failed to hash password", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	// Generate UUID
	userID := uuid.New().String()

	userData := map[string]interface{}{
		"id":            userID,
		"email":         req.Email,
		"password_hash": passwordHash,
		"role":          req.Role,
		"is_active":     true,
		"created_at":    time.Now().Format(time.RFC3339),
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	_, err = h.mongoClient.NewQuery("users").Insert(ctx, userData)
	if err != nil {
		h.logger.Error("Failed to create user", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	// Generate tokens
	accessToken, expiresAt, err := auth.GenerateAccessToken(
		userID,
		req.Email,
		req.Role,
		h.cfg.JWTSecret,
		h.cfg.JWTIssuer,
		h.cfg.JWTAudience,
		h.cfg.AccessTTLMin,
	)
	if err != nil {
		h.logger.Error("Failed to generate access token", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	refreshToken, err := auth.GenerateRefreshToken()
	if err != nil {
		h.logger.Error("Failed to generate refresh token", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	if err := auth.StoreRefreshToken(h.mongoClient, userID, refreshToken, h.cfg.RefreshTTLDays); err != nil {
		h.logger.Error("Failed to store refresh token", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	c.JSON(http.StatusCreated, AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		User: UserInfo{
			ID:    userID,
			Email: req.Email,
			Role:  req.Role,
		},
		ExpiresAt: expiresAt,
	})
}

func (h *Handler) Refresh(c *gin.Context) {
	var req RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.BadRequest(c, err.Error())
		return
	}

	// Verify refresh token
	userID, err := auth.VerifyRefreshToken(h.mongoClient, req.RefreshToken)
	if err != nil {
		errors.Unauthorized(c, "invalid or expired refresh token")
		return
	}

	// Fetch user
	user, err := h.mongoClient.NewQuery("users").
		Select("id", "email", "role", "is_active").
		Eq("id", userID).
		FindOne(c.Request.Context())

	if err != nil || user == nil {
		errors.Unauthorized(c, "user not found")
		return
	}
	if isActive, ok := user["is_active"].(bool); ok && !isActive {
		errors.Forbidden(c, "account is inactive")
		return
	}

	userEmail, _ := user["email"].(string)
	userRole, _ := user["role"].(string)

	// Generate new access token
	accessToken, expiresAt, err := auth.GenerateAccessToken(
		userID,
		userEmail,
		userRole,
		h.cfg.JWTSecret,
		h.cfg.JWTIssuer,
		h.cfg.JWTAudience,
		h.cfg.AccessTTLMin,
	)
	if err != nil {
		h.logger.Error("Failed to generate access token", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	// Optionally rotate refresh token (security best practice)
	refreshToken, err := auth.GenerateRefreshToken()
	if err != nil {
		h.logger.Error("Failed to generate refresh token", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	// Revoke old token
	_ = auth.RevokeRefreshToken(h.mongoClient, req.RefreshToken)

	// Store new refresh token
	if err := auth.StoreRefreshToken(h.mongoClient, userID, refreshToken, h.cfg.RefreshTTLDays); err != nil {
		h.logger.Error("Failed to store refresh token", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	c.JSON(http.StatusOK, AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		User: UserInfo{
			ID:    userID,
			Email: userEmail,
			Role:  userRole,
		},
		ExpiresAt: expiresAt,
	})
}

func (h *Handler) Logout(c *gin.Context) {
	// Get refresh token from request
	var req RefreshRequest
	if err := c.ShouldBindJSON(&req); err == nil && req.RefreshToken != "" {
		// Revoke refresh token
		_ = auth.RevokeRefreshToken(h.mongoClient, req.RefreshToken)
	}

	// Optionally revoke all user tokens
	userID, exists := c.Get("user_id")
	if exists {
		_ = auth.RevokeAllUserTokens(h.mongoClient, userID.(string))
	}

	c.JSON(http.StatusOK, gin.H{"message": "logged out successfully"})
}
