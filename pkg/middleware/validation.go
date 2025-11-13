package middleware

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/troikatech/calling-agent/pkg/errors"
)

// ValidateIDParam validates that an ID parameter is a valid positive integer
func ValidateIDParam(paramName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param(paramName)
		if idStr == "" {
			errors.BadRequest(c, paramName+" parameter is required")
			c.Abort()
			return
		}

		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			errors.BadRequest(c, "invalid "+paramName+" parameter: must be a positive integer")
			c.Abort()
			return
		}

		c.Set(paramName+"_int", id)
		c.Set(paramName, idStr)
		c.Next()
	}
}

// ValidatePhoneParam validates that a phone parameter is in E.164 format
func ValidatePhoneParam(paramName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		phone := c.Param(paramName)
		if phone == "" {
			errors.BadRequest(c, paramName+" parameter is required")
			c.Abort()
			return
		}

		// Basic E.164 validation
		if !strings.HasPrefix(phone, "+") || len(phone) < 8 || len(phone) > 16 {
			errors.BadRequest(c, "invalid "+paramName+": must be in E.164 format (e.g., +919876543210)")
			c.Abort()
			return
		}

		c.Set(paramName, phone)
		c.Next()
	}
}

// SanitizeString removes potentially dangerous characters from strings
func SanitizeString(s string) string {
	// Remove null bytes
	s = strings.ReplaceAll(s, "\x00", "")
	// Trim whitespace
	s = strings.TrimSpace(s)
	return s
}

// ValidateCampaignWindow validates business hours (0-23)
func ValidateCampaignWindow(start, end int) error {
	if start < 0 || start > 23 {
		return &ValidationError{Message: "window start must be between 0 and 23"}
	}
	if end < 0 || end > 23 {
		return &ValidationError{Message: "window end must be between 0 and 23"}
	}
	if start >= end {
		return &ValidationError{Message: "window start must be less than window end"}
	}
	return nil
}

// ValidationError represents a validation error
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

