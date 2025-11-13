package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/troikatech/calling-agent/pkg/errors"
	"github.com/troikatech/calling-agent/pkg/middleware"
	"github.com/troikatech/calling-agent/pkg/utils"
	"github.com/troikatech/calling-agent/pkg/validation"
)

type ImportContactsRequest struct {
	Contacts []ContactImport `json:"contacts" binding:"required"`
}

type ContactImport struct {
	Phone         string   `json:"msisdn_e164" binding:"required"`
	Name          string   `json:"name"`
	Tags          []string `json:"tags"`
	Consent       bool     `json:"consent"`
	ConsentSource string   `json:"consent_source"`
}

type ContactResponse struct {
	ID               int64    `json:"id"`
	Phone            string   `json:"msisdn_e164"`
	Name             string   `json:"name"`
	Tags             []string `json:"tags"`
	DND              bool     `json:"dnd"`
	Consent          bool     `json:"consent"`
	ConsentSource    string   `json:"consent_source"`
	ConsentTimestamp string   `json:"consent_timestamp"`
	LastCallAt       string   `json:"last_call_at"`
}

func (h *Handler) ImportContacts(c *gin.Context) {
	var req ImportContactsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.BadRequest(c, err.Error())
		return
	}

	var validContacts []map[string]interface{}
	var validationErrors []string

	for i, contact := range req.Contacts {
		normalized, err := validation.NormalizeE164(contact.Phone)
		if err != nil {
			validationErrors = append(validationErrors, "contact "+strconv.Itoa(i)+": "+err.Error())
			continue
		}

		// Sanitize name
		sanitizedName := middleware.SanitizeString(contact.Name)

		validContacts = append(validContacts, map[string]interface{}{
			"msisdn_e164":       normalized,
			"name":              sanitizedName,
			"tags":              contact.Tags,
			"consent":           contact.Consent,
			"consent_source":    middleware.SanitizeString(contact.ConsentSource),
			"consent_timestamp": time.Now().Format(time.RFC3339),
		})
	}

	if len(validContacts) == 0 {
		errors.BadRequest(c, "no valid contacts to import")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	// Upsert each contact (MongoDB upsert by msisdn_e164)
	for _, contact := range validContacts {
		filter := map[string]interface{}{"msisdn_e164": contact["msisdn_e164"]}
		_, err := h.mongoClient.NewQuery("contacts").
			Upsert(ctx, filter, contact)
		if err != nil {
			h.logger.Error("Failed to upsert contact", zap.Error(err), zap.String("phone", contact["msisdn_e164"].(string)))
		}
	}

	c.JSON(http.StatusCreated, gin.H{
		"imported": len(validContacts),
		"errors":   validationErrors,
	})
}

func (h *Handler) SearchContacts(c *gin.Context) {
	// Parse pagination
	pagination := utils.ParsePagination(c)
	tag := c.Query("tag")
	status := c.Query("status")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	query := h.mongoClient.NewQuery("contacts").Select("*")

	if tag != "" {
		// MongoDB array contains: tags field contains the tag value
		query = query.Eq("tags", tag)
	}

	if status == "dnd" {
		query = query.Eq("dnd", true)
	} else if status == "consented" {
		query = query.Eq("consent", true)
	}

	// Get total count
	totalCount, countErr := query.Count(ctx)
	if countErr != nil {
		h.logger.Warn("Failed to get total count", zap.Error(countErr))
		totalCount = 0
	}

	// Apply pagination
	query = query.Limit(int64(pagination.Limit))

	contacts, err := query.Find(ctx)
	if err != nil {
		h.logger.Error("Failed to search contacts", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	// Convert to ContactResponse
	contactResponses := make([]ContactResponse, 0, len(contacts))
	for _, c := range contacts {
		contactResponses = append(contactResponses, ContactResponse{
			ID:               getInt64(c, "id"),
			Phone:            getString(c, "msisdn_e164"),
			Name:             getString(c, "name"),
			Tags:             getStringSlice(c, "tags"),
			DND:              getBool(c, "dnd"),
			Consent:          getBool(c, "consent"),
			ConsentSource:    getString(c, "consent_source"),
			ConsentTimestamp: getString(c, "consent_timestamp"),
			LastCallAt:       getString(c, "last_call_at"),
		})
	}

	c.JSON(http.StatusOK, utils.PaginatedResponse{
		Data:  contactResponses,
		Page:  pagination.Page,
		Limit: pagination.Limit,
		Total: totalCount,
		Count: len(contactResponses),
	})
}

func (h *Handler) GetContact(c *gin.Context) {
	id, _ := c.Get("id_int")
	idStr := fmt.Sprintf("%d", id.(int64))

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	contact, err := h.mongoClient.NewQuery("contacts").
		Select("*").
		Eq("id", idStr).
		FindOne(ctx)

	if err != nil || contact == nil {
		errors.NotFound(c, "contact not found")
		return
	}

	contactResponse := ContactResponse{
		ID:               getInt64(contact, "id"),
		Phone:            getString(contact, "msisdn_e164"),
		Name:             getString(contact, "name"),
		Tags:             getStringSlice(contact, "tags"),
		DND:              getBool(contact, "dnd"),
		Consent:          getBool(contact, "consent"),
		ConsentSource:    getString(contact, "consent_source"),
		ConsentTimestamp: getString(contact, "consent_timestamp"),
		LastCallAt:       getString(contact, "last_call_at"),
	}

	c.JSON(http.StatusOK, contactResponse)
}

func (h *Handler) UpdateContact(c *gin.Context) {
	id, _ := c.Get("id_int")
	idStr := fmt.Sprintf("%d", id.(int64))

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		errors.BadRequest(c, err.Error())
		return
	}

	if phone, ok := updates["msisdn_e164"].(string); ok {
		normalized, err := validation.NormalizeE164(phone)
		if err != nil {
			errors.BadRequest(c, err.Error())
			return
		}
		updates["msisdn_e164"] = normalized
	}

	// Sanitize string fields
	if name, ok := updates["name"].(string); ok {
		updates["name"] = middleware.SanitizeString(name)
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	_, err := h.mongoClient.NewQuery("contacts").
		Eq("id", idStr).
		UpdateOne(ctx, updates)

	if err != nil {
		h.logger.Error("Failed to update contact", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "contact updated"})
}

func (h *Handler) DeleteContact(c *gin.Context) {
	id, _ := c.Get("id_int")
	idStr := fmt.Sprintf("%d", id.(int64))

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	_, err := h.mongoClient.NewQuery("contacts").
		Eq("id", idStr).
		DeleteOne(ctx)

	if err != nil {
		h.logger.Error("Failed to delete contact", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "contact deleted"})
}

func (h *Handler) AddSuppression(c *gin.Context) {
	var req struct {
		Phone  string `json:"msisdn_e164" binding:"required"`
		Source string `json:"source" binding:"required"`
		Reason string `json:"reason"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		errors.BadRequest(c, err.Error())
		return
	}

	normalized, err := validation.NormalizeE164(req.Phone)
	if err != nil {
		errors.BadRequest(c, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	suppressionData := map[string]interface{}{
		"msisdn_e164": normalized,
		"source":      req.Source,
		"reason":      req.Reason,
		"created_at":  time.Now().Format(time.RFC3339),
	}

	_, err = h.mongoClient.NewQuery("suppression").Insert(ctx, suppressionData)
	if err != nil {
		h.logger.Error("Failed to add suppression", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "number added to suppression list"})
}

func (h *Handler) CheckSuppression(c *gin.Context) {
	phone := c.Param("phone")

	normalized, err := validation.NormalizeE164(phone)
	if err != nil {
		errors.BadRequest(c, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	results, _ := h.mongoClient.NewQuery("suppression").
		Select("*").
		Eq("msisdn_e164", normalized).
		Find(ctx)

	suppressed := len(results) > 0

	c.JSON(http.StatusOK, gin.H{
		"msisdn_e164": normalized,
		"suppressed":  suppressed,
	})
}

// Helper functions (shared with users.go)
func getInt64(m map[string]interface{}, key string) int64 {
	if val, ok := m[key]; ok {
		switch v := val.(type) {
		case int64:
			return v
		case int:
			return int64(v)
		case float64:
			return int64(v)
		}
	}
	return 0
}

func getStringSlice(m map[string]interface{}, key string) []string {
	if val, ok := m[key]; ok {
		if arr, ok := val.([]interface{}); ok {
			result := make([]string, 0, len(arr))
			for _, v := range arr {
				if str, ok := v.(string); ok {
					result = append(result, str)
				}
			}
			return result
		}
		if arr, ok := val.([]string); ok {
			return arr
		}
	}
	return []string{}
}
