package errors

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ProblemDetail represents an RFC 7807 Problem Details response
type ProblemDetail struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	TraceID  string `json:"trace_id,omitempty"`
	Instance string `json:"instance,omitempty"`
}

// ErrorResponse sends a problem+json error response
func ErrorResponse(c *gin.Context, status int, title, detail string) {
	traceID := c.GetString("trace_id")
	if traceID == "" {
		traceID = c.GetString("request_id")
	}

	problem := ProblemDetail{
		Type:    getProblemType(status),
		Title:   title,
		Status:  status,
		Detail:  detail,
		TraceID: traceID,
		Instance: c.Request.URL.Path,
	}

	c.Header("Content-Type", "application/problem+json")
	c.JSON(status, problem)
}

// InternalError logs and sends a 500 error
func InternalError(c *gin.Context, err error, logger *zap.Logger) {
	logger.Error("Internal server error",
		zap.Error(err),
		zap.String("path", c.Request.URL.Path),
		zap.String("method", c.Request.Method),
	)

	ErrorResponse(c, http.StatusInternalServerError,
		"Internal Server Error",
		"An unexpected error occurred. Please try again later.",
	)
}

// BadRequest sends a 400 error
func BadRequest(c *gin.Context, detail string) {
	ErrorResponse(c, http.StatusBadRequest,
		"Bad Request",
		detail,
	)
}

// Unauthorized sends a 401 error
func Unauthorized(c *gin.Context, detail string) {
	ErrorResponse(c, http.StatusUnauthorized,
		"Unauthorized",
		detail,
	)
}

// Forbidden sends a 403 error
func Forbidden(c *gin.Context, detail string) {
	ErrorResponse(c, http.StatusForbidden,
		"Forbidden",
		detail,
	)
}

// NotFound sends a 404 error
func NotFound(c *gin.Context, detail string) {
	ErrorResponse(c, http.StatusNotFound,
		"Not Found",
		detail,
	)
}

// Conflict sends a 409 error
func Conflict(c *gin.Context, detail string) {
	ErrorResponse(c, http.StatusConflict,
		"Conflict",
		detail,
	)
}

// TooManyRequests sends a 429 error
func TooManyRequests(c *gin.Context, detail string) {
	ErrorResponse(c, http.StatusTooManyRequests,
		"Too Many Requests",
		detail,
	)
}

func getProblemType(status int) string {
	baseURL := "https://api.troikatech.in/problems"
	switch status {
	case http.StatusBadRequest:
		return baseURL + "/bad-request"
	case http.StatusUnauthorized:
		return baseURL + "/unauthorized"
	case http.StatusForbidden:
		return baseURL + "/forbidden"
	case http.StatusNotFound:
		return baseURL + "/not-found"
	case http.StatusConflict:
		return baseURL + "/conflict"
	case http.StatusTooManyRequests:
		return baseURL + "/rate-limit-exceeded"
	case http.StatusInternalServerError:
		return baseURL + "/internal-error"
	default:
		return baseURL + "/error"
	}
}

