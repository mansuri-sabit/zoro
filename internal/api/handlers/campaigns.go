package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/troikatech/calling-agent/pkg/audit"
	"github.com/troikatech/calling-agent/pkg/client"
	"github.com/troikatech/calling-agent/pkg/errors"
	"github.com/troikatech/calling-agent/pkg/middleware"
	"github.com/troikatech/calling-agent/pkg/utils"
)

type CreateCampaignRequest struct {
	Name      string         `json:"name" binding:"required"`
	Contacts  []ContactRef   `json:"contacts" binding:"required"`
	Window    CampaignWindow `json:"window"`
	Retries   RetryConfig    `json:"retries"`
	FlowID    string         `json:"flow_id" binding:"required"`
	PersonaID int64          `json:"persona_id"`
}

type ContactRef struct {
	Phone string   `json:"msisdn_e164" binding:"required"`
	Name  string   `json:"name"`
	Tags  []string `json:"tags"`
}

type CampaignWindow struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type RetryConfig struct {
	Max    int `json:"max"`
	GapMin int `json:"gap_min"`
}

func (h *Handler) CreateCampaign(c *gin.Context) {
	var req CreateCampaignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.BadRequest(c, err.Error())
		return
	}

	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	if req.Window.Start == 0 {
		req.Window.Start = h.cfg.DialBusinessStartHour
	}
	if req.Window.End == 0 {
		req.Window.End = h.cfg.DialBusinessEndHour
	}
	if req.Retries.Max == 0 {
		req.Retries.Max = h.cfg.RetryNoAnswerMax
	}
	if req.Retries.GapMin == 0 {
		req.Retries.GapMin = h.cfg.RetryNoAnswerGapMin
	}

	// Validate window hours
	if err := middleware.ValidateCampaignWindow(req.Window.Start, req.Window.End); err != nil {
		errors.BadRequest(c, err.Error())
		return
	}

	campaignData := map[string]interface{}{
		"name":          req.Name,
		"status":        "draft",
		"window_start":  req.Window.Start,
		"window_end":    req.Window.End,
		"max_retries":   req.Retries.Max,
		"retry_gap_min": req.Retries.GapMin,
		"flow_id":       req.FlowID,
		"persona_id":    req.PersonaID,
		"created_by":    userID,
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	campaignData["created_at"] = time.Now().Format(time.RFC3339)
	campaignID, err := h.mongoClient.NewQuery("campaigns").Insert(ctx, campaignData)
	if err != nil {
		h.logger.Error("Failed to create campaign", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	for _, contact := range req.Contacts {
		// Find or create contact
		contactDoc, _ := h.mongoClient.NewQuery("contacts").
			Select("id").
			Eq("msisdn_e164", contact.Phone).
			FindOne(ctx)

		var contactID interface{}
		if contactDoc != nil {
			contactID = contactDoc["id"]
		} else {
			newContact := map[string]interface{}{
				"msisdn_e164": contact.Phone,
				"name":        contact.Name,
				"tags":        contact.Tags,
				"created_at":  time.Now().Format(time.RFC3339),
			}
			contactID, _ = h.mongoClient.NewQuery("contacts").Insert(ctx, newContact)
		}

		if contactID != nil {
			campaignContactData := map[string]interface{}{
				"campaign_id": campaignID,
				"contact_id":  contactID,
				"status":      "pending",
				"created_at":  time.Now().Format(time.RFC3339),
			}
			h.mongoClient.NewQuery("campaign_contacts").Insert(ctx, campaignContactData)
		}
	}

	// Schedule campaign via dialer service with retry and circuit breaker
	dialerReq := map[string]interface{}{
		"campaign_id": campaignID,
	}

	httpClient := client.NewHTTPClient("dialer-service", 10*time.Second)
	resp, err := httpClient.Post(c.Request.Context(), "http://dialer:8090/internal/campaigns/schedule", dialerReq)

	if err != nil {
		h.logger.Error("Failed to schedule campaign with dialer service", zap.Error(err))
		// Don't fail the request - campaign is created, scheduling can retry
	} else if resp != nil {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			h.logger.Warn("Dialer service returned non-OK status",
				zap.Int("status_code", resp.StatusCode),
				zap.Int64("campaign_id", campaignID.(int64)),
			)
		}
	}

	// Audit log
	audit.Log(h.mongoClient, userIDStr, string(audit.ActionCreate), "campaign", fmt.Sprintf("%v", campaignID), map[string]interface{}{
		"name":    req.Name,
		"flow_id": req.FlowID,
	})

	c.JSON(http.StatusCreated, gin.H{
		"campaign_id": campaignID,
		"message":     "campaign created and scheduled",
	})
}

func (h *Handler) ListCampaigns(c *gin.Context) {
	// Parse pagination
	pagination := utils.ParsePagination(c)
	status := c.Query("status")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	query := h.mongoClient.NewQuery("campaigns").Select("*")

	if status != "" {
		query = query.Eq("status", status)
	}

	// Apply pagination
	query = query.Limit(int64(pagination.Limit))

	campaigns, err := query.Find(ctx)
	if err != nil {
		h.logger.Error("Failed to fetch campaigns", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	c.JSON(http.StatusOK, utils.PaginatedResponse{
		Data:  campaigns,
		Page:  pagination.Page,
		Limit: pagination.Limit,
		Count: len(campaigns),
	})
}

func (h *Handler) GetCampaign(c *gin.Context) {
	id, _ := c.Get("id_int")
	idStr := fmt.Sprintf("%d", id.(int64))

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	campaign, err := h.mongoClient.NewQuery("campaigns").
		Select("*").
		Eq("id", idStr).
		FindOne(ctx)

	if err != nil || campaign == nil {
		errors.NotFound(c, "campaign not found")
		return
	}

	stats, _ := h.mongoClient.NewQuery("campaign_contacts").
		Select("status").
		Eq("campaign_id", idStr).
		Find(ctx)

	statusCounts := make(map[string]int)
	for _, s := range stats {
		if status, ok := s["status"].(string); ok {
			statusCounts[status]++
		}
	}

	campaign["stats"] = statusCounts
	campaign["total_contacts"] = len(stats)

	c.JSON(http.StatusOK, campaign)
}

func (h *Handler) PauseCampaign(c *gin.Context) {
	id, _ := c.Get("id_int")
	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	_, err := h.mongoClient.NewQuery("campaigns").
		Eq("id", fmt.Sprintf("%d", id.(int64))).
		UpdateOne(ctx, map[string]interface{}{"status": "paused"})

	if err != nil {
		h.logger.Error("Failed to pause campaign", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	// Audit log
	audit.Log(h.mongoClient, userIDStr, string(audit.ActionPause), "campaign", fmt.Sprintf("%d", id.(int64)), nil)

	c.JSON(http.StatusOK, gin.H{"message": "campaign paused"})
}

func (h *Handler) ResumeCampaign(c *gin.Context) {
	id, _ := c.Get("id_int")
	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	_, err := h.mongoClient.NewQuery("campaigns").
		Eq("id", fmt.Sprintf("%d", id.(int64))).
		UpdateOne(ctx, map[string]interface{}{"status": "running"})

	if err != nil {
		h.logger.Error("Failed to resume campaign", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	// Audit log
	audit.Log(h.mongoClient, userIDStr, string(audit.ActionResume), "campaign", fmt.Sprintf("%d", id.(int64)), nil)

	c.JSON(http.StatusOK, gin.H{"message": "campaign resumed"})
}

func (h *Handler) CancelCampaign(c *gin.Context) {
	id, _ := c.Get("id_int")
	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	_, err := h.mongoClient.NewQuery("campaigns").
		Eq("id", fmt.Sprintf("%d", id.(int64))).
		UpdateOne(ctx, map[string]interface{}{"status": "cancelled"})

	if err != nil {
		h.logger.Error("Failed to cancel campaign", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	// Audit log
	audit.Log(h.mongoClient, userIDStr, string(audit.ActionCancel), "campaign", fmt.Sprintf("%d", id.(int64)), nil)

	c.JSON(http.StatusOK, gin.H{"message": "campaign cancelled"})
}

func (h *Handler) GetCampaignContacts(c *gin.Context) {
	id, _ := c.Get("id_int")
	idStr := fmt.Sprintf("%d", id.(int64))

	// Parse pagination
	pagination := utils.ParsePagination(c)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	results, err := h.mongoClient.NewQuery("campaign_contacts").
		Select("*").
		Eq("campaign_id", idStr).
		Limit(int64(pagination.Limit)).
		Find(ctx)

	if err != nil {
		h.logger.Error("Failed to fetch campaign contacts", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	// Populate contact details for each campaign_contact
	for i, result := range results {
		if contactID, ok := result["contact_id"]; ok {
			contact, _ := h.mongoClient.NewQuery("contacts").
				Select("msisdn_e164", "name").
				Eq("id", contactID).
				FindOne(ctx)
			if contact != nil {
				result["contacts"] = contact
			}
			results[i] = result
		}
	}

	c.JSON(http.StatusOK, utils.PaginatedResponse{
		Data:  results,
		Page:  pagination.Page,
		Limit: pagination.Limit,
		Count: len(results),
	})
}
