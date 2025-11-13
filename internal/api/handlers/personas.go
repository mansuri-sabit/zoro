package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/troikatech/calling-agent/pkg/errors"
	"github.com/troikatech/calling-agent/pkg/utils"
)

func (h *Handler) ListPersonas(c *gin.Context) {
	// Parse pagination
	pagination := utils.ParsePagination(c)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	personas, err := h.mongoClient.NewQuery("personas").
		Select("*").
		Limit(int64(pagination.Limit)).
		Find(ctx)

	if err != nil {
		h.logger.Error("Failed to fetch personas", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	c.JSON(http.StatusOK, utils.PaginatedResponse{
		Data:  personas,
		Page:  pagination.Page,
		Limit: pagination.Limit,
		Count: len(personas),
	})
}

func (h *Handler) GetPersona(c *gin.Context) {
	id, _ := c.Get("id_int")
	idStr := fmt.Sprintf("%d", id.(int64))

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	persona, err := h.mongoClient.NewQuery("personas").
		Select("*").
		Eq("id", idStr).
		FindOne(ctx)

	if err != nil || persona == nil {
		errors.NotFound(c, "persona not found")
		return
	}

	c.JSON(http.StatusOK, persona)
}

func (h *Handler) CreatePersona(c *gin.Context) {
	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.BadRequest(c, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	req["created_at"] = time.Now().Format(time.RFC3339)
	personaID, err := h.mongoClient.NewQuery("personas").Insert(ctx, req)
	if err != nil {
		h.logger.Error("Failed to create persona", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	req["id"] = personaID
	c.JSON(http.StatusCreated, req)
}

func (h *Handler) UpdatePersona(c *gin.Context) {
	id, _ := c.Get("id_int")
	idStr := fmt.Sprintf("%d", id.(int64))

	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.BadRequest(c, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	_, err := h.mongoClient.NewQuery("personas").
		Eq("id", idStr).
		UpdateOne(ctx, req)

	if err != nil {
		h.logger.Error("Failed to update persona", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "persona updated"})
}
