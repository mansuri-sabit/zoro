package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/troikatech/calling-agent/pkg/auth"
	"github.com/troikatech/calling-agent/pkg/errors"
)

func AuthMiddleware(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			errors.Unauthorized(c, "authorization header required")
			c.Abort()
			return
		}

		bearerToken := strings.Split(authHeader, " ")
		if len(bearerToken) != 2 || strings.ToLower(bearerToken[0]) != "bearer" {
			errors.Unauthorized(c, "invalid authorization format")
			c.Abort()
			return
		}

		tokenString := bearerToken[1]
		claims, err := auth.ParseToken(tokenString, jwtSecret)

		if err != nil {
			errors.Unauthorized(c, "invalid or expired token")
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("user_email", claims.Email)
		c.Set("user_role", claims.Role)
		c.Next()
	}
}

func RoleMiddleware(allowedRoles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole, exists := c.Get("user_role")
		if !exists {
			errors.Forbidden(c, "role not found in token")
			c.Abort()
			return
		}

		role := userRole.(string)
		for _, allowed := range allowedRoles {
			if role == allowed {
				c.Next()
				return
			}
		}

		errors.Forbidden(c, "insufficient permissions")
		c.Abort()
	}
}
