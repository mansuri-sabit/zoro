package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/troikatech/calling-agent/pkg/errors"
)

type ExotelWebhookPayload struct {
	CallSid           string `json:"CallSid" form:"CallSid"`
	From              string `json:"From" form:"From"`
	To                string `json:"To" form:"To"`
	Direction         string `json:"Direction" form:"Direction"`
	Status            string `json:"Status" form:"Status"`
	StartTime         string `json:"StartTime" form:"StartTime"`
	EndTime           string `json:"EndTime" form:"EndTime"`
	Duration          string `json:"Duration" form:"Duration"`
	RecordingUrl      string `json:"RecordingUrl" form:"RecordingUrl"`
	DialCallStatus    string `json:"DialCallStatus" form:"DialCallStatus"`
	DialCallDuration  string `json:"DialCallDuration" form:"DialCallDuration"`
	Digits            string `json:"Digits" form:"Digits"`
}

func (h *Handler) ExotelWebhook(c *gin.Context) {
	var payload ExotelWebhookPayload
	if err := c.ShouldBind(&payload); err != nil {
		errors.BadRequest(c, "invalid payload")
		return
	}

	if payload.CallSid == "" {
		errors.BadRequest(c, "CallSid is required")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	existingCall, _ := h.mongoClient.NewQuery("calls").
		Select("call_sid").
		Eq("call_sid", payload.CallSid).
		FindOne(ctx)

	callData := map[string]interface{}{
		"call_sid":      payload.CallSid,
		"from_number":   payload.From,
		"to_number":     payload.To,
		"direction":     payload.Direction,
		"status":        payload.Status,
		"started_at":    payload.StartTime,
		"ended_at":      payload.EndTime,
		"recording_url": payload.RecordingUrl,
	}

	if existingCall != nil {
		h.mongoClient.NewQuery("calls").
			Eq("call_sid", payload.CallSid).
			UpdateOne(ctx, callData)
	} else {
		callData["created_at"] = time.Now().Format(time.RFC3339)
		h.mongoClient.NewQuery("calls").Insert(ctx, callData)
	}

	if payload.Digits != "" {
		if payload.Digits == "3" {
			// Use To (called number) for suppression, not From
			suppressionData := map[string]interface{}{
				"msisdn_e164": payload.To,
				"source":      "ivr",
				"reason":      "opt-out via DTMF",
				"created_at":  time.Now().Format(time.RFC3339),
			}
			h.mongoClient.NewQuery("suppression").Insert(ctx, suppressionData)
		}

		h.mongoClient.NewQuery("campaign_contacts").
			Eq("call_sid", payload.CallSid).
			UpdateOne(ctx, map[string]interface{}{
				"ivr_digits": payload.Digits,
			})
	}

	c.JSON(http.StatusOK, gin.H{"message": "webhook processed"})
}
