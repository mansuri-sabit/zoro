package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/troikatech/calling-agent/pkg/mongo"
)

// StoreRefreshToken stores a refresh token hash in the database
func StoreRefreshToken(client *mongo.Client, userID, token string, expiresInDays int) error {
	tokenHash := hashToken(token)
	expiresAt := time.Now().Add(time.Duration(expiresInDays) * 24 * time.Hour)

	data := map[string]interface{}{
		"user_id":     userID,
		"token_hash":  tokenHash,
		"expires_at":  expiresAt.Format(time.RFC3339),
		"revoked_at":  nil,
		"last_used_at": nil,
		"created_at":   time.Now().Format(time.RFC3339),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.NewQuery("refresh_tokens").Insert(ctx, data)
	return err
}

// VerifyRefreshToken verifies a refresh token and returns user info
func VerifyRefreshToken(client *mongo.Client, token string) (string, error) {
	tokenHash := hashToken(token)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Check if token exists and is not revoked
	tokenData, err := client.NewQuery("refresh_tokens").
		Select("user_id", "expires_at", "revoked_at").
		Eq("token_hash", tokenHash).
		FindOne(ctx)

	if err != nil || tokenData == nil {
		return "", fmt.Errorf("refresh token not found")
	}

	// Check if revoked
	if revokedAt, ok := tokenData["revoked_at"].(string); ok && revokedAt != "" {
		return "", fmt.Errorf("refresh token has been revoked")
	}

	// Check if expired
	expiresAtStr, _ := tokenData["expires_at"].(string)
	expiresAt, err := time.Parse(time.RFC3339, expiresAtStr)
	if err != nil || time.Now().After(expiresAt) {
		return "", fmt.Errorf("refresh token has expired")
	}

	userID, _ := tokenData["user_id"].(string)

	// Update last_used_at
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	client.NewQuery("refresh_tokens").
		Eq("token_hash", tokenHash).
		UpdateOne(ctx2, map[string]interface{}{
			"last_used_at": time.Now().Format(time.RFC3339),
		})

	return userID, nil
}

// RevokeRefreshToken revokes a refresh token
func RevokeRefreshToken(client *mongo.Client, token string) error {
	tokenHash := hashToken(token)
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.NewQuery("refresh_tokens").
		Eq("token_hash", tokenHash).
		UpdateOne(ctx, map[string]interface{}{
			"revoked_at": time.Now().Format(time.RFC3339),
		})

	return err
}

// RevokeAllUserTokens revokes all refresh tokens for a user
func RevokeAllUserTokens(client *mongo.Client, userID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.NewQuery("refresh_tokens").
		Eq("user_id", userID).
		IsNull("revoked_at").
		Update(ctx, map[string]interface{}{
			"revoked_at": time.Now().Format(time.RFC3339),
		})

	return err
}

func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

