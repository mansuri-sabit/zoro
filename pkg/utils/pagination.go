package utils

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

type PaginationParams struct {
	Page  int
	Limit int
}

type PaginatedResponse struct {
	Data       interface{} `json:"data"`
	Page       int         `json:"page"`
	Limit      int         `json:"limit"`
	Total      int64       `json:"total,omitempty"`
	NextCursor string      `json:"next_cursor,omitempty"`
	Count      int         `json:"count,omitempty"`
}

// ParsePagination extracts pagination params from query string
func ParsePagination(c *gin.Context) PaginationParams {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))

	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	return PaginationParams{
		Page:  page,
		Limit: limit,
	}
}

// ParseCursor extracts cursor-based pagination
func ParseCursor(c *gin.Context) (string, int) {
	cursor := c.Query("next_cursor")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))

	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	return cursor, limit
}

