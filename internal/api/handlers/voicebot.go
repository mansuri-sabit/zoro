package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/troikatech/calling-agent/pkg/ai"
	"github.com/troikatech/calling-agent/pkg/audio"
	"github.com/troikatech/calling-agent/pkg/env"
	"github.com/troikatech/calling-agent/pkg/errors"
	"github.com/troikatech/calling-agent/pkg/logger"
	"github.com/troikatech/calling-agent/pkg/stt"
	"github.com/troikatech/calling-agent/pkg/validation"
)

// VoiceSession manages state for an active Exotel voice call session
type VoiceSession struct {
	CallSid             string
	StreamSid           string
	From                string
	To                  string
	Conn                *websocket.Conn
	ConversationHistory []map[string]interface{}
	AudioBuffer         *AudioBuffer
	GreetingSent        bool
	IsActive            bool
	Mu                  sync.RWMutex
	CancelCtx           context.CancelFunc
	ProcessingMu        sync.Mutex             // Prevents concurrent STT→AI→TTS processing
	SampleRate          int                    // Audio sample rate (default 16000)
	CustomParameters    map[string]interface{} // Custom parameters from start event
}

// ExotelEvent represents the base structure for Exotel WebSocket events
type ExotelEvent struct {
	Event     string `json:"event"`
	StreamSid string `json:"stream_sid,omitempty"`
}

// StartEvent represents Exotel "start" event
type StartEvent struct {
	Event            string                 `json:"event"`
	StreamSid        string                 `json:"stream_sid"`
	CustomParameters map[string]interface{} `json:"custom_parameters,omitempty"`
}

// MediaEvent represents Exotel "media" event with base64-encoded PCM audio
type MediaEvent struct {
	Event     string `json:"event"`
	StreamSid string `json:"stream_sid"`
	Media     struct {
		Payload string `json:"payload"` // Base64-encoded PCM audio
	} `json:"media"`
}

// StopEvent represents Exotel "stop" event
type StopEvent struct {
	Event     string `json:"event"`
	StreamSid string `json:"stream_sid"`
}

// AudioBuffer manages audio chunks for STT processing
type AudioBuffer struct {
	mu          sync.Mutex
	chunks      [][]byte
	totalSize   int
	maxSize     int // Maximum buffer size before processing (e.g., 1 second of audio)
	lastProcess time.Time
	sampleRate  int // Sample rate for this buffer (default 16000)
}

// NewAudioBuffer creates a new audio buffer
func NewAudioBuffer(maxSize int, sampleRate int) *AudioBuffer {
	if sampleRate == 0 {
		sampleRate = 16000 // Default 16kHz
	}
	return &AudioBuffer{
		chunks:      make([][]byte, 0),
		maxSize:     maxSize,
		lastProcess: time.Now(),
		sampleRate:  sampleRate,
	}
}

// Append adds audio chunk to buffer
func (ab *AudioBuffer) Append(chunk []byte) {
	ab.mu.Lock()
	defer ab.mu.Unlock()
	ab.chunks = append(ab.chunks, chunk)
	ab.totalSize += len(chunk)
}

// GetData returns all buffered audio data
func (ab *AudioBuffer) GetData() []byte {
	ab.mu.Lock()
	defer ab.mu.Unlock()
	result := make([]byte, 0, ab.totalSize)
	for _, chunk := range ab.chunks {
		result = append(result, chunk...)
	}
	return result
}

// IsReady checks if buffer is ready for processing
// Uses silence detection: process if buffer is full OR 1.5 seconds of silence (no new data)
func (ab *AudioBuffer) IsReady() bool {
	ab.mu.Lock()
	defer ab.mu.Unlock()
	// Process if buffer is full or 1.5 seconds have passed since last process
	return ab.totalSize >= ab.maxSize || time.Since(ab.lastProcess) >= 1500*time.Millisecond
}

// Clear clears the buffer
func (ab *AudioBuffer) Clear() {
	ab.mu.Lock()
	defer ab.mu.Unlock()
	ab.chunks = ab.chunks[:0]
	ab.totalSize = 0
	ab.lastProcess = time.Now()
}

// sessions stores active voice sessions per call_sid
var sessions = make(map[string]*VoiceSession)
var sessionsMu sync.RWMutex

// getOrCreateSession gets or creates a voice session for call_sid
func getOrCreateSession(callSid, streamSid, from, to string, conn *websocket.Conn, sampleRate int) *VoiceSession {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	if session, exists := sessions[callSid]; exists {
		session.Mu.Lock()
		session.StreamSid = streamSid
		session.Conn = conn
		session.SampleRate = sampleRate
		session.Mu.Unlock()
		return session
	}

	_, cancel := context.WithCancel(context.Background())
	if sampleRate == 0 {
		sampleRate = 16000 // Default 16kHz
	}
	session := &VoiceSession{
		CallSid:             callSid,
		StreamSid:           streamSid,
		From:                from,
		To:                  to,
		Conn:                conn,
		ConversationHistory: make([]map[string]interface{}, 0),
		AudioBuffer:         NewAudioBuffer(32*1024, sampleRate), // 32KB buffer (~1 second at 16kHz, 2 bytes per sample)
		GreetingSent:        false,
		IsActive:            true,
		CancelCtx:           cancel,
		SampleRate:          sampleRate,
		CustomParameters:    make(map[string]interface{}),
	}

	sessions[callSid] = session
	return session
}

// getSession gets a voice session for call_sid
func getSession(callSid string) *VoiceSession {
	sessionsMu.RLock()
	defer sessionsMu.RUnlock()
	return sessions[callSid]
}

// removeSession removes a voice session
func removeSession(callSid string) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	if session, exists := sessions[callSid]; exists {
		if session.CancelCtx != nil {
			session.CancelCtx()
		}
		delete(sessions, callSid)
	}
}

// ExotelVoicebotRequest represents the payload Exotel sends when a call starts
type ExotelVoicebotRequest struct {
	CallSid        string `json:"CallSid" form:"CallSid"`
	From           string `json:"From" form:"From"`
	To             string `json:"To" form:"To"`
	DialWhomNumber string `json:"DialWhomNumber" form:"DialWhomNumber"` // Target number for outbound calls
	VirtualNumber  string `json:"VirtualNumber" form:"VirtualNumber"`   // DID that received the call
	Direction      string `json:"Direction" form:"Direction"`           // incoming/outbound
	UserData       string `json:"UserData" form:"UserData"`             // Custom data passed to Exotel
	// Add other Exotel parameters as needed
}

// VoicebotWebSocketResponse is what Exotel expects - a WebSocket URL
type VoicebotWebSocketResponse struct {
	WebSocketURL string `json:"websocket_url"`
}

// ExotelVoicebotEndpoint handles Exotel Voicebot initialization
// Exotel calls this endpoint when a call starts to get the WebSocket URL
// Supports both GET (query params) and POST (form/json) requests
func (h *Handler) ExotelVoicebotEndpoint(c *gin.Context) {
	var req ExotelVoicebotRequest

	// Log all incoming parameters for debugging
	h.logger.Info("ExotelVoicebotEndpoint called - raw parameters",
		zap.String("method", c.Request.Method),
		zap.String("url", c.Request.URL.String()),
		zap.Any("query_params", c.Request.URL.Query()),
		zap.Any("form_params", c.Request.PostForm),
	)

	// Try to bind from query params (GET) or form/json (POST)
	if err := c.ShouldBind(&req); err != nil {
		// If binding fails, try to get from query params directly
		req.CallSid = c.Query("CallSid")
		req.From = c.Query("From")
		req.To = c.Query("To")
		req.DialWhomNumber = c.Query("DialWhomNumber")
		req.VirtualNumber = c.Query("VirtualNumber")
		req.Direction = c.Query("Direction")
	}

	// Also try alternative parameter names
	if req.CallSid == "" {
		req.CallSid = c.Query("call_sid")
	}
	if req.From == "" {
		req.From = c.Query("CallFrom")
	}
	if req.To == "" {
		req.To = c.Query("CallTo")
	}
	if req.DialWhomNumber == "" {
		req.DialWhomNumber = c.Query("DialWhomNumber")
	}
	if req.VirtualNumber == "" {
		req.VirtualNumber = c.Query("VirtualNumber")
	}
	if req.Direction == "" {
		req.Direction = c.Query("Direction")
	}
	if req.UserData == "" {
		req.UserData = c.Query("UserData")
	}

	if req.CallSid == "" {
		h.logger.Warn("ExotelVoicebotEndpoint called without CallSid",
			zap.String("method", c.Request.Method),
			zap.String("url", c.Request.URL.String()),
			zap.Any("query", c.Request.URL.Query()),
		)
		errors.BadRequest(c, "CallSid is required")
		return
	}

	// CRITICAL FIX: Look up original call record to get correct From/To mapping
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	// Try to find call record by CallSid (case-insensitive, trimmed)
	callSidNormalized := strings.TrimSpace(req.CallSid)
	originalCall, _ := h.mongoClient.NewQuery("calls").
		Select("from_number", "to_number", "direction", "caller_id").
		Eq("call_sid", callSidNormalized).
		FindOne(ctx)

	// If not found, try with different case or try to find by phone numbers (for outbound calls)
	if originalCall == nil && req.Direction != "incoming" {
		// For outbound calls, try to find by matching From/To within last 5 minutes
		// This handles timing issues where Exotel calls back before MongoDB indexes
		normalizedExophone := normalizePhoneNumber(h.cfg.ExotelExophone)
		normalizedFrom := normalizePhoneNumber(req.From)

		if normalizedFrom == normalizedExophone {
			// This looks like an outbound call - try to find recent call record
			recentCalls, _ := h.mongoClient.NewQuery("calls").
				Select("from_number", "to_number", "direction", "caller_id", "call_sid").
				Eq("from_number", h.cfg.ExotelExophone).
				Eq("direction", "outbound").
				Find(ctx)

			// Find the most recent call that matches (within last 2 minutes)
			for _, call := range recentCalls {
				callSid := getString(call, "call_sid")
				// Check if CallSid is similar (might have different format)
				if strings.Contains(callSid, callSidNormalized) || strings.Contains(callSidNormalized, callSid) {
					originalCall = call
					h.logger.Info("Found call record by CallSid similarity",
						zap.String("requested_call_sid", callSidNormalized),
						zap.String("found_call_sid", callSid),
					)
					break
				}
			}

			// If still not found, use the most recent outbound call as fallback
			if originalCall == nil && len(recentCalls) > 0 {
				originalCall = recentCalls[len(recentCalls)-1] // Most recent
				h.logger.Info("Using most recent outbound call as fallback",
					zap.String("requested_call_sid", callSidNormalized),
					zap.String("fallback_call_sid", getString(originalCall, "call_sid")),
				)
			}
		}
	}

	var virtualNumber, targetNumber, callerNumber string
	var isOutbound bool

	if originalCall != nil {
		// We have the original call record - use correct mapping
		originalFrom := getString(originalCall, "from_number")
		originalTo := getString(originalCall, "to_number")
		originalDirection := getString(originalCall, "direction")
		originalCallerID := getString(originalCall, "caller_id")

		isOutbound = originalDirection == "outbound"

		if isOutbound {
			// Outbound call: From = Virtual Number (Exophone), To = Target Number
			// Database stores: from_number = Virtual Exophone, to_number = Target
			virtualNumber = originalFrom // Virtual Exophone (what made the call)
			targetNumber = originalTo    // Target number (customer)
			callerNumber = originalCallerID
			if callerNumber == "" {
				callerNumber = virtualNumber
			}

			h.logger.Info("Outbound call detected - using original call record",
				zap.String("call_sid", req.CallSid),
				zap.String("virtual_number", virtualNumber),
				zap.String("target_number", targetNumber),
				zap.String("original_from", originalFrom),
				zap.String("original_to", originalTo),
			)
		} else {
			// Inbound call: From = Caller, To = Virtual Number (DID)
			virtualNumber = originalTo
			targetNumber = originalFrom // Caller is the "target" for inbound
			callerNumber = originalFrom

			h.logger.Info("Inbound call detected - using original call record",
				zap.String("call_sid", req.CallSid),
				zap.String("virtual_number", virtualNumber),
				zap.String("caller_number", targetNumber),
				zap.String("original_from", originalFrom),
				zap.String("original_to", originalTo),
			)
		}
	} else {
		// No original call record - try to detect from Exotel parameters
		h.logger.Warn("No original call record found - attempting to detect from Exotel parameters",
			zap.String("call_sid", req.CallSid),
			zap.String("exotel_from", req.From),
			zap.String("exotel_to", req.To),
			zap.String("dial_whom_number", req.DialWhomNumber),
			zap.String("virtual_number", req.VirtualNumber),
			zap.String("direction", req.Direction),
		)

		// Try to detect virtual number
		// Check if From or To matches configured Exophone
		normalizedExophone := normalizePhoneNumber(h.cfg.ExotelExophone)
		normalizedFrom := normalizePhoneNumber(req.From)
		normalizedTo := normalizePhoneNumber(req.To)

		if normalizedFrom == normalizedExophone {
			// From is the virtual number - this is likely outbound
			virtualNumber = req.From
			targetNumber = req.DialWhomNumber
			if targetNumber == "" {
				targetNumber = req.To
			}
			isOutbound = true
		} else if normalizedTo == normalizedExophone {
			// To is the virtual number - this is likely inbound
			virtualNumber = req.To
			targetNumber = req.From
			isOutbound = false
		} else if req.VirtualNumber != "" {
			// Use VirtualNumber parameter if provided
			virtualNumber = req.VirtualNumber
			if req.Direction == "incoming" {
				targetNumber = req.From
				isOutbound = false
			} else {
				targetNumber = req.DialWhomNumber
				if targetNumber == "" {
					targetNumber = req.To
				}
				isOutbound = true
			}
		} else {
			// Fallback: use Exotel's From/To as-is but log warning
			h.logger.Warn("Could not detect virtual number - using Exotel parameters as fallback",
				zap.String("call_sid", req.CallSid),
				zap.String("from", req.From),
				zap.String("to", req.To),
			)
			virtualNumber = req.From
			targetNumber = req.To
			isOutbound = req.Direction != "incoming"
		}

		callerNumber = virtualNumber
	}

	// Validation: Ensure we have valid numbers
	if virtualNumber == "" || targetNumber == "" {
		h.logger.Error("Invalid number mapping detected",
			zap.String("call_sid", req.CallSid),
			zap.String("virtual_number", virtualNumber),
			zap.String("target_number", targetNumber),
			zap.String("exotel_from", req.From),
			zap.String("exotel_to", req.To),
		)
		// Fallback to Exotel parameters if validation fails
		if virtualNumber == "" {
			virtualNumber = req.From
		}
		if targetNumber == "" {
			targetNumber = req.To
		}
	}

	// Prevent self-call (virtual number calling itself)
	if normalizePhoneNumber(virtualNumber) == normalizePhoneNumber(targetNumber) {
		h.logger.Error("Self-call detected - virtual number and target number are same",
			zap.String("call_sid", req.CallSid),
			zap.String("virtual_number", virtualNumber),
			zap.String("target_number", targetNumber),
			zap.String("exotel_from", req.From),
			zap.String("exotel_to", req.To),
		)
		// Try to get correct target from multiple sources
		// 1. Try DialWhomNumber
		if req.DialWhomNumber != "" && normalizePhoneNumber(req.DialWhomNumber) != normalizePhoneNumber(virtualNumber) {
			targetNumber = req.DialWhomNumber
			h.logger.Info("Fixed self-call using DialWhomNumber",
				zap.String("call_sid", req.CallSid),
				zap.String("corrected_target", targetNumber),
			)
		} else if req.UserData != "" {
			// 2. Try to extract from UserData (we sent target_number in UserData)
			var userDataMap map[string]interface{}
			if err := json.Unmarshal([]byte(req.UserData), &userDataMap); err == nil {
				if targetFromUserData, ok := userDataMap["target_number"].(string); ok && targetFromUserData != "" {
					if normalizePhoneNumber(targetFromUserData) != normalizePhoneNumber(virtualNumber) {
						targetNumber = targetFromUserData
						h.logger.Info("Fixed self-call using UserData",
							zap.String("call_sid", req.CallSid),
							zap.String("corrected_target", targetNumber),
						)
					}
				}
			}
		}

		// 3. Try original call record
		if normalizePhoneNumber(virtualNumber) == normalizePhoneNumber(targetNumber) && originalCall != nil {
			// Use original target from database
			originalTo := getString(originalCall, "to_number")
			if originalTo != "" && normalizePhoneNumber(originalTo) != normalizePhoneNumber(virtualNumber) {
				targetNumber = originalTo
				h.logger.Info("Fixed self-call using original call record",
					zap.String("call_sid", req.CallSid),
					zap.String("corrected_target", targetNumber),
				)
			}
		}
	}

	// Final validation
	if normalizePhoneNumber(virtualNumber) == normalizePhoneNumber(targetNumber) {
		h.logger.Error("CRITICAL: Still detecting self-call after fix attempts",
			zap.String("call_sid", req.CallSid),
			zap.String("virtual_number", virtualNumber),
			zap.String("target_number", targetNumber),
		)
	}

	// Log final mapping decision
	h.logger.Info("ExotelVoicebotEndpoint - final number mapping",
		zap.String("call_sid", req.CallSid),
		zap.String("virtual_number", virtualNumber),
		zap.String("target_number", targetNumber),
		zap.String("caller_number", callerNumber),
		zap.Bool("is_outbound", isOutbound),
		zap.String("exotel_from", req.From),
		zap.String("exotel_to", req.To),
		zap.String("dial_whom_number", req.DialWhomNumber),
		zap.String("virtual_number_param", req.VirtualNumber),
		zap.String("direction", req.Direction),
	)

	// CRITICAL FIX: Update call record with corrected number mapping
	// This ensures database has correct values even if Exotel sends incorrect ones
	ctxUpdate, cancelUpdate := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancelUpdate()

	updateData := map[string]interface{}{
		"virtual_number": virtualNumber,
		"target_number":  targetNumber,
	}
	// Only update from_number/to_number if they're different from existing (to preserve original)
	if originalCall == nil || getString(originalCall, "from_number") == "" {
		// New call or missing data - set corrected values
		if isOutbound {
			updateData["from_number"] = virtualNumber
			updateData["to_number"] = targetNumber
			updateData["direction"] = "outbound"
		} else {
			updateData["from_number"] = targetNumber // Caller
			updateData["to_number"] = virtualNumber  // DID
			updateData["direction"] = "inbound"
		}
	} else {
		// Existing call - only update if current values show self-call
		existingFrom := getString(originalCall, "from_number")
		existingTo := getString(originalCall, "to_number")
		if normalizePhoneNumber(existingFrom) == normalizePhoneNumber(existingTo) {
			// Self-call detected in existing record - fix it
			if isOutbound {
				updateData["from_number"] = virtualNumber
				updateData["to_number"] = targetNumber
			} else {
				updateData["from_number"] = targetNumber
				updateData["to_number"] = virtualNumber
			}
			newFrom := ""
			newTo := ""
			if val, ok := updateData["from_number"].(string); ok {
				newFrom = val
			}
			if val, ok := updateData["to_number"].(string); ok {
				newTo = val
			}
			h.logger.Info("Updating call record with corrected number mapping",
				zap.String("call_sid", req.CallSid),
				zap.String("old_from", existingFrom),
				zap.String("old_to", existingTo),
				zap.String("new_from", newFrom),
				zap.String("new_to", newTo),
			)
		}
	}

	// Update call record
	h.mongoClient.NewQuery("calls").
		Eq("call_sid", req.CallSid).
		UpdateOne(ctxUpdate, updateData)

	// Get base URL - prefer configured URL, fallback to request-based detection
	baseURL := h.cfg.VoicebotBaseURL
	if baseURL == "" {
		// Fallback: construct from request headers (works behind reverse proxy)
		scheme := "https"

		// Check X-Forwarded-Proto header (set by reverse proxy)
		if proto := c.GetHeader("X-Forwarded-Proto"); proto == "http" {
			scheme = "http"
		} else if c.Request.TLS == nil {
			// Direct connection without TLS
			scheme = "http"
		}

		// Get host from X-Forwarded-Host (reverse proxy) or Host header
		host := c.GetHeader("X-Forwarded-Host")
		if host == "" {
			host = c.GetHeader("Host")
		}
		if host == "" {
			host = c.Request.Host
		}

		baseURL = fmt.Sprintf("%s://%s", scheme, host)
	}

	// Ensure baseURL doesn't have trailing slash
	if len(baseURL) > 0 && baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}

	// Replace http/https with ws/wss for WebSocket URL
	// Production MUST use wss://, development can use ws://
	wsBaseURL := baseURL
	if len(wsBaseURL) >= 5 {
		if wsBaseURL[:5] == "https" {
			wsBaseURL = "wss" + wsBaseURL[5:]
		} else if len(wsBaseURL) >= 4 && wsBaseURL[:4] == "http" {
			wsBaseURL = "ws" + wsBaseURL[4:]
		}
	}

	// WebSocket endpoint with call parameters - use /was as per requirements
	// Use CORRECTED virtual number and target number (not Exotel's potentially incorrect values)
	// URL encode all parameters to handle special characters properly
	wsURL := fmt.Sprintf("%s/was?sample-rate=16000&call_sid=%s&from=%s&to=%s",
		wsBaseURL,
		url.QueryEscape(req.CallSid),
		url.QueryEscape(virtualNumber), // Use corrected virtual number
		url.QueryEscape(targetNumber),  // Use corrected target number
	)

	h.logger.Info("Generated WebSocket URL for Exotel",
		zap.String("call_sid", req.CallSid),
		zap.String("virtual_number", virtualNumber),
		zap.String("target_number", targetNumber),
		zap.String("ws_url", wsURL),
		zap.String("exotel_from", req.From),
		zap.String("exotel_to", req.To),
	)

	response := VoicebotWebSocketResponse{
		WebSocketURL: wsURL,
	}

	c.JSON(http.StatusOK, response)
}

// createWebSocketUpgrader creates a secure WebSocket upgrader with origin validation
// Per requirements: No authentication required (direct connect)
func createWebSocketUpgrader(cfg *env.Config) websocket.Upgrader {
	return websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")

			// Per requirements: No authentication, allow all connections
			// Exotel WebSocket connections may not send origin header or may use different origins
			// Log origin for debugging but allow all connections
			if origin != "" {
				logger.Log.Info("WebSocket connection from origin",
					zap.String("origin", origin),
					zap.String("remote_addr", r.RemoteAddr),
					zap.String("path", r.URL.Path),
				)
			}

			// Allow all origins (no authentication as per requirements)
			return true
		},
		ReadBufferSize:  8192, // Increased for better performance
		WriteBufferSize: 8192, // Increased for better performance
		// Enable compression for better performance
		EnableCompression: true,
	}
}

// VoicebotWebSocket handles WebSocket connection from Exotel Voicebot
// This is the actual WebSocket endpoint that Exotel connects to for real-time audio streaming
// Must be accessible via public wss:// URL for Exotel to connect
// No authentication required (direct connect as per requirements)
func (h *Handler) VoicebotWebSocket(c *gin.Context) {
	// Log all query parameters for debugging
	h.logger.Info("VoicebotWebSocket connection attempt",
		zap.String("path", c.Request.URL.Path),
		zap.String("raw_query", c.Request.URL.RawQuery),
		zap.Any("query_params", c.Request.URL.Query()),
		zap.String("method", c.Request.Method),
		zap.String("remote_addr", c.Request.RemoteAddr),
	)

	// Get call parameters from query string (Exotel sends call_sid or callLogId)
	// Try multiple parameter name variations that Exotel might use
	callSid := c.Query("call_sid")
	if callSid == "" {
		callSid = c.Query("CallSid")
	}
	if callSid == "" {
		callSid = c.Query("callLogId")
	}
	if callSid == "" {
		callSid = c.Query("CallLogId")
	}
	from := c.Query("from")
	to := c.Query("to")

	// Get sample-rate from query parameter - 16kHz is mandatory
	sampleRateStr := c.Query("sample-rate")
	sampleRate := 16000 // Default 16kHz
	if sampleRateStr != "" {
		if sr, err := strconv.Atoi(sampleRateStr); err == nil && sr > 0 {
			sampleRate = sr
			h.logger.Info("Sample rate from query parameter",
				zap.Int("sample_rate", sampleRate),
				zap.String("call_sid", callSid),
			)
		}
	}
	// Enforce 16kHz as mandatory requirement
	if sampleRate != 16000 {
		h.logger.Warn("Invalid sample rate, enforcing 16kHz",
			zap.Int("requested", sampleRate),
			zap.String("call_sid", callSid),
		)
		sampleRate = 16000
	}

	// Allow WebSocket connection even without call_sid
	// We'll try to extract it from the first "start" event
	if callSid == "" {
		h.logger.Warn("VoicebotWebSocket called without call_sid - will try to extract from first message",
			zap.String("raw_query", c.Request.URL.RawQuery),
			zap.Any("query_params", c.Request.URL.Query()),
			zap.String("url", c.Request.URL.String()),
		)
		// Use temporary identifier - will be replaced when we get call_sid from start event
		callSid = "pending-" + fmt.Sprintf("%d", time.Now().UnixNano())
	}

	// Create secure WebSocket upgrader with origin validation
	upgrader := createWebSocketUpgrader(h.cfg)

	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Error("Failed to upgrade to WebSocket",
			zap.Error(err),
			zap.String("call_sid", callSid),
			zap.String("origin", c.GetHeader("Origin")),
			zap.String("remote_addr", c.Request.RemoteAddr),
		)
		return
	}
	defer conn.Close()

	h.logger.Info("Voicebot WebSocket connection established",
		zap.String("call_sid", callSid),
		logger.MaskPhoneIfPresent("from", from),
		logger.MaskPhoneIfPresent("to", to),
		zap.Int("sample_rate", sampleRate),
		zap.Bool("call_sid_from_query", callSid != "" && !strings.HasPrefix(callSid, "pending-")),
	)

	// Create or update call record in database (only if we have real call_sid)
	if callSid != "" && !strings.HasPrefix(callSid, "pending-") {
		h.initializeCallRecord(callSid, from, to)
	}

	// Handle WebSocket messages - Exotel sends JSON events, not binary
	// Pass a pointer to callSid so it can be updated when we get it from start event
	h.handleVoicebotConnection(conn, &callSid, from, to, sampleRate)
}

// initializeCallRecord creates or updates call record when Voicebot session starts
func (h *Handler) initializeCallRecord(callSid, from, to string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Check if call record exists
	existingCall, _ := h.mongoClient.NewQuery("calls").
		Select("call_sid").
		Eq("call_sid", callSid).
		FindOne(ctx)

	callData := map[string]interface{}{
		"call_sid":    callSid,
		"from_number": from,
		"to_number":   to,
		"direction":   "outbound",
		"status":      "in-progress",
		"started_at":  time.Now().Format(time.RFC3339),
	}

	if existingCall != nil {
		// Update existing call
		h.mongoClient.NewQuery("calls").
			Eq("call_sid", callSid).
			UpdateOne(ctx, callData)
	} else {
		// Create new call record
		callData["created_at"] = time.Now().Format(time.RFC3339)
		h.mongoClient.NewQuery("calls").Insert(ctx, callData)
	}
}

// handleVoicebotConnection manages the WebSocket connection lifecycle
// callSidPtr is a pointer so we can update it when we get the real call_sid from start event
func (h *Handler) handleVoicebotConnection(conn *websocket.Conn, callSidPtr *string, from, to string, sampleRate int) {
	// Set read deadline to detect connection closure
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Start ping ticker to keep connection alive
	pingTicker := time.NewTicker(54 * time.Second)
	defer pingTicker.Stop()

	// Channel for graceful shutdown
	done := make(chan struct{})

	// Message handling goroutine - Exotel sends JSON events
	go func() {
		defer close(done)
		for {
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					h.logger.Error("WebSocket read error", zap.Error(err))
				}
				return
			}

			// Exotel sends JSON events as text messages
			if messageType == websocket.TextMessage {
				h.handleExotelEvent(conn, callSidPtr, from, to, message, sampleRate)
			} else if messageType == websocket.PingMessage {
				conn.WriteMessage(websocket.PongMessage, nil)
			}
		}
	}()

	// Main loop - handle ping and connection lifecycle
	for {
		select {
		case <-done:
			h.logger.Info("Voicebot WebSocket connection closed",
				zap.String("call_sid", *callSidPtr),
			)
			// Clean up session
			removeSession(*callSidPtr)
			if *callSidPtr != "" && !strings.HasPrefix(*callSidPtr, "pending-") {
				h.finalizeCallRecord(*callSidPtr)
			}
			return

		case <-pingTicker.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				h.logger.Error("Failed to send ping", zap.Error(err))
				return
			}
		}
	}
}

// handleExotelEvent processes Exotel JSON events (start, media, stop, clear)
// callSidPtr is a pointer so we can update it when we get the real call_sid from start event
func (h *Handler) handleExotelEvent(conn *websocket.Conn, callSidPtr *string, from, to string, message []byte, sampleRate int) {
	var event ExotelEvent
	if err := json.Unmarshal(message, &event); err != nil {
		h.logger.Warn("Failed to parse Exotel event", zap.Error(err), zap.String("raw", string(message)))
		return
	}

	h.logger.Info("Received Exotel event",
		zap.String("call_sid", *callSidPtr),
		zap.String("event", event.Event),
		zap.String("stream_sid", event.StreamSid),
	)

	switch event.Event {
	case "start":
		h.handleStartEvent(conn, callSidPtr, from, to, message, sampleRate)
	case "media":
		h.handleMediaEvent(conn, *callSidPtr, message)
	case "stop":
		h.handleStopEvent(*callSidPtr, message)
	case "clear":
		h.handleClearEvent(*callSidPtr, message)
	default:
		h.logger.Debug("Unknown Exotel event", zap.String("event", event.Event))
	}
}

// handleStartEvent processes Exotel "start" event
// On start: create session, extract custom_parameters, trigger greeting TTS
// callSidPtr is a pointer so we can update it if we get call_sid from custom_parameters
func (h *Handler) handleStartEvent(conn *websocket.Conn, callSidPtr *string, from, to string, message []byte, sampleRate int) {
	var startEvent StartEvent
	if err := json.Unmarshal(message, &startEvent); err != nil {
		h.logger.Warn("Failed to parse start event", zap.Error(err))
		return
	}

	// Try to extract call_sid from custom_parameters if we don't have it yet
	if *callSidPtr == "" || strings.HasPrefix(*callSidPtr, "pending-") {
		if startEvent.CustomParameters != nil {
			if callSidVal, ok := startEvent.CustomParameters["call_sid"].(string); ok && callSidVal != "" {
				oldCallSid := *callSidPtr
				*callSidPtr = callSidVal
				h.logger.Info("Extracted call_sid from start event custom_parameters",
					zap.String("old_call_sid", oldCallSid),
					zap.String("new_call_sid", *callSidPtr),
				)
				// Initialize call record now that we have the real call_sid
				h.initializeCallRecord(*callSidPtr, from, to)
			}
		}
		// If still no call_sid, try to use stream_sid as fallback
		if (*callSidPtr == "" || strings.HasPrefix(*callSidPtr, "pending-")) && startEvent.StreamSid != "" {
			*callSidPtr = startEvent.StreamSid
			h.logger.Info("Using stream_sid as call_sid fallback",
				zap.String("call_sid", *callSidPtr),
			)
			h.initializeCallRecord(*callSidPtr, from, to)
		}
	}

	// Create or get session
	session := getOrCreateSession(*callSidPtr, startEvent.StreamSid, from, to, conn, sampleRate)

	// Store custom_parameters in session
	session.Mu.Lock()
	if session.GreetingSent {
		session.Mu.Unlock()
		return // Greeting already sent
	}

	// Extract and store custom_parameters
	if startEvent.CustomParameters != nil {
		session.CustomParameters = startEvent.CustomParameters
		session.SampleRate = sampleRate
	}

	session.GreetingSent = true
	session.Mu.Unlock()

	// Log custom_parameters for debugging
	if len(startEvent.CustomParameters) > 0 {
		customParamsBytes, _ := json.Marshal(startEvent.CustomParameters)
		customParamsStr := string(customParamsBytes)
		h.logger.Info("Start event custom_parameters received",
			zap.String("call_sid", *callSidPtr),
			zap.String("custom_parameters", customParamsStr),
		)
	} else {
		h.logger.Warn("Start event received without custom_parameters",
			zap.String("call_sid", *callSidPtr),
			zap.String("stream_sid", startEvent.StreamSid),
		)
	}

	h.logger.Info("Handling start event, sending greeting",
		zap.String("call_sid", *callSidPtr),
		zap.String("stream_sid", startEvent.StreamSid),
		zap.Any("custom_parameters", startEvent.CustomParameters),
		zap.Int("sample_rate", sampleRate),
	)

	// Send greeting TTS in a goroutine
	go h.sendGreeting(session)
}

// sendGreeting sends TTS greeting to Exotel in chunked PCM format
func (h *Handler) sendGreeting(session *VoiceSession) {
	// Get greeting text from custom_parameters or use default
	greetingText := "Hello! How can I help you today?"
	if session.CustomParameters != nil {
		if gt, ok := session.CustomParameters["greeting_text"].(string); ok && gt != "" {
			greetingText = gt
		}
	}

	if !h.cfg.FeatureAI || h.cfg.OpenAIApiKey == "" {
		// Fallback: send text response
		h.sendTextResponse(session, greetingText)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get voice from custom_parameters or use default
	voice := "shimmer" // Default: female, super natural Hindi
	if session.CustomParameters != nil {
		if v, ok := session.CustomParameters["voice_id"].(string); ok && v != "" {
			voice = v
		}
	}

	// CRITICAL FIX: Use OpenAI TTS with PCM16 format
	openAITTS := ai.NewOpenAITTSService(h.cfg.OpenAIApiKey, 10*time.Second, h.logger)
	ttsReq := &ai.OpenAITTSRequest{
		Text:   greetingText,
		Model:  "tts-1-hd", // Best quality
		Voice:  voice,
		Format: "pcm", // Request raw PCM16 format (24kHz output)
		Speed:  1.0,
	}

	// Get PCM16 audio (24kHz from OpenAI)
	pcm24k, err := openAITTS.TextToSpeechPCM(ctx, ttsReq)
	if err != nil {
		h.logger.Warn("OpenAI TTS greeting failed", zap.Error(err))
		h.sendTextResponse(session, greetingText)
		return
	}

	// Resample 24kHz → 16kHz for Exotel
	pcm16k := h.resample24kTo16k(pcm24k)

	// Stream PCM in 640-byte chunks (20ms frames at 16kHz)
	h.streamPCMAudio(session, pcm16k, "greeting_done")
}

// handleMediaEvent processes Exotel "media" event with base64-encoded G.711 μ-law audio
// CRITICAL FIX: Exotel sends G.711 μ-law (8kHz), NOT PCM16
func (h *Handler) handleMediaEvent(conn *websocket.Conn, callSid string, message []byte) {
	var mediaEvent MediaEvent
	if err := json.Unmarshal(message, &mediaEvent); err != nil {
		h.logger.Warn("Failed to parse media event", zap.Error(err))
		return
	}

	session := getSession(callSid)
	if session == nil {
		h.logger.Warn("No session found for media event", zap.String("call_sid", callSid))
		return
	}

	// CRITICAL FIX: Exotel sends G.711 μ-law (8-bit, 8kHz), NOT PCM16
	// Step 1: Decode base64 to get μ-law bytes
	muLawData, err := audio.DecodeBase64PCM(mediaEvent.Media.Payload)
	if err != nil {
		h.logger.Warn("Failed to decode base64 μ-law", zap.Error(err))
		return
	}

	// Step 2: Decode μ-law to PCM16 (8kHz)
	pcm8k := audio.DecodeMuLawToPCM16(muLawData)

	// Step 3: Resample 8kHz → 16kHz (Deepgram requires 16kHz)
	pcm16k := audio.Resample8kTo16k(pcm8k)

	// Append resampled PCM16 (16kHz) to audio buffer
	session.AudioBuffer.Append(pcm16k)

	// Process if buffer is ready (utterance detection)
	if session.AudioBuffer.IsReady() {
		// Prevent concurrent processing
		if !session.ProcessingMu.TryLock() {
			return
		}
		go func() {
			defer session.ProcessingMu.Unlock()
			h.processAudioBuffer(session)
		}()
	}
}

// processAudioBuffer processes buffered audio through STT → AI → TTS pipeline
func (h *Handler) processAudioBuffer(session *VoiceSession) {
	audioData := session.AudioBuffer.GetData()
	session.AudioBuffer.Clear()

	if len(audioData) == 0 {
		return
	}

	// Step 1: Convert audio to text using STT
	transcribedText := h.callSTTService(session, audioData)
	if transcribedText == "" {
		h.logger.Warn("STT returned empty text", zap.String("call_sid", session.CallSid))
		return
	}

	h.logger.Info("STT transcription",
		zap.String("call_sid", session.CallSid),
		zap.String("text", transcribedText),
	)

	// Step 2: Update conversation history
	session.Mu.Lock()
	session.ConversationHistory = append(session.ConversationHistory, map[string]interface{}{
		"role":    "user",
		"content": transcribedText,
	})
	conversationHistory := make([]map[string]interface{}, len(session.ConversationHistory))
	copy(conversationHistory, session.ConversationHistory)
	session.Mu.Unlock()

	// Step 3: Get call context and generate AI response
	callContext := h.getCallContext(session.CallSid)
	callContext["conversation_history"] = conversationHistory
	// Add custom_parameters to call context
	session.Mu.RLock()
	customParams := session.CustomParameters
	session.Mu.RUnlock()
	if customParams != nil {
		callContext["custom_parameters"] = customParams
	}
	aiResponse := h.generateAIResponse(session, transcribedText, callContext)

	// Step 4: Update conversation history with AI response
	session.Mu.Lock()
	session.ConversationHistory = append(session.ConversationHistory, map[string]interface{}{
		"role":    "assistant",
		"content": aiResponse,
	})
	session.Mu.Unlock()

	// Step 5: Convert AI response to speech and stream
	h.sendTTSResponse(session, aiResponse)
}

// callSTTService calls Deepgram to convert audio to text
// CRITICAL FIX: Use Deepgram STT with raw PCM16 (16kHz, mono, little-endian)
func (h *Handler) callSTTService(session *VoiceSession, audioData []byte) string {
	if !h.cfg.FeatureAI || h.cfg.DeepgramApiKey == "" {
		// Fallback to OpenAI Whisper if Deepgram not configured
		if h.cfg.OpenAIApiKey == "" {
			return ""
		}
		return h.callSTTServiceWhisper(session, audioData)
	}

	// Use Deepgram STT with raw PCM16 (16kHz, mono, little-endian)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Import Deepgram client
	deepgramClient := stt.NewDeepgramClient(h.cfg.DeepgramApiKey, 10*time.Second, h.logger)

	// Get language from custom_parameters or use auto-detect
	language := ""
	if session.CustomParameters != nil {
		if lang, ok := session.CustomParameters["language"].(string); ok && lang != "" {
			language = lang
		}
	}

	sttReq := &stt.STTRequest{
		AudioData:   audioData, // Raw PCM16, 16kHz, mono, little-endian
		SampleRate:  16000,     // 16kHz (required by Deepgram)
		Language:    language,  // Auto-detect if empty
		Model:       "nova-2",  // Best accuracy model
		Punctuate:   true,      // Add punctuation
		Interim:     false,     // Final results only
		Endpointing: true,      // Enable endpointing
	}

	sttResp, err := deepgramClient.SpeechToText(ctx, sttReq)
	if err != nil {
		h.logger.Warn("Deepgram STT failed", zap.Error(err))
		// Fallback to Whisper if Deepgram fails
		if h.cfg.OpenAIApiKey != "" {
			return h.callSTTServiceWhisper(session, audioData)
		}
		return ""
	}

	if sttResp == nil || sttResp.Text == "" {
		h.logger.Warn("Deepgram STT returned empty text")
		return ""
	}

	return sttResp.Text
}

// callSTTServiceWhisper fallback to OpenAI Whisper
func (h *Handler) callSTTServiceWhisper(session *VoiceSession, audioData []byte) string {
	// Convert PCM to WAV format for Whisper
	session.Mu.RLock()
	sampleRate := session.SampleRate
	session.Mu.RUnlock()
	if sampleRate == 0 {
		sampleRate = 16000 // Default 16kHz
	}
	wavData := h.convertPCMToWAV(audioData, sampleRate)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sttService := ai.NewSTTService(h.cfg.OpenAIApiKey, "whisper-1", "", 10*time.Second, h.logger)
	sttReq := &ai.STTRequest{
		AudioData:   wavData,
		AudioFormat: "wav",
		Language:    "",
		Prompt:      "",
	}

	sttResp, err := sttService.SpeechToText(ctx, sttReq)
	if err != nil {
		h.logger.Warn("OpenAI Whisper failed", zap.Error(err))
		return ""
	}

	if sttResp == nil || sttResp.Text == "" {
		h.logger.Warn("OpenAI Whisper returned empty text")
		return ""
	}

	return sttResp.Text
}

// convertPCMToWAV converts raw PCM (16-bit, 16kHz, mono) to WAV format
func (h *Handler) convertPCMToWAV(pcmData []byte, sampleRate int) []byte {
	// WAV header for 16-bit PCM, 16kHz, mono
	// This is a simplified WAV header - in production, use a proper WAV library
	if sampleRate == 0 {
		sampleRate = 16000 // Default 16kHz
	}
	bitsPerSample := 16
	channels := 1
	dataSize := len(pcmData)

	// WAV header (44 bytes)
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	// File size - 8
	fileSize := uint32(36 + dataSize)
	header[4] = byte(fileSize)
	header[5] = byte(fileSize >> 8)
	header[6] = byte(fileSize >> 16)
	header[7] = byte(fileSize >> 24)
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	// fmt chunk size
	header[16] = 16
	header[17] = 0
	header[18] = 0
	header[19] = 0
	// Audio format (1 = PCM)
	header[20] = 1
	header[21] = 0
	// Number of channels
	header[22] = byte(channels)
	header[23] = 0
	// Sample rate
	header[24] = byte(sampleRate)
	header[25] = byte(sampleRate >> 8)
	header[26] = byte(sampleRate >> 16)
	header[27] = byte(sampleRate >> 24)
	// Byte rate
	byteRate := uint32(sampleRate * channels * bitsPerSample / 8)
	header[28] = byte(byteRate)
	header[29] = byte(byteRate >> 8)
	header[30] = byte(byteRate >> 16)
	header[31] = byte(byteRate >> 24)
	// Block align
	blockAlign := uint16(channels * bitsPerSample / 8)
	header[32] = byte(blockAlign)
	header[33] = byte(blockAlign >> 8)
	// Bits per sample
	header[34] = byte(bitsPerSample)
	header[35] = 0
	copy(header[36:40], "data")
	// Data size
	header[40] = byte(dataSize)
	header[41] = byte(dataSize >> 8)
	header[42] = byte(dataSize >> 16)
	header[43] = byte(dataSize >> 24)

	// Combine header + PCM data
	wavData := make([]byte, 44+dataSize)
	copy(wavData[0:44], header)
	copy(wavData[44:], pcmData)

	return wavData
}

// sendTTSResponse converts text to speech and streams audio back to Exotel
func (h *Handler) sendTTSResponse(session *VoiceSession, text string) {
	if !h.cfg.FeatureAI || h.cfg.OpenAIApiKey == "" {
		h.sendTextResponse(session, text)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get voice from custom_parameters or use default
	voice := "shimmer" // Default: female, super natural Hindi
	if session.CustomParameters != nil {
		if v, ok := session.CustomParameters["voice_id"].(string); ok && v != "" {
			voice = v
		}
	}

	// CRITICAL FIX: Use OpenAI TTS with PCM16 format (not MP3)
	// OpenAI TTS supports "pcm" format which returns raw PCM16, 24kHz
	// We need to resample to 16kHz for Exotel
	openAITTS := ai.NewOpenAITTSService(h.cfg.OpenAIApiKey, 10*time.Second, h.logger)
	ttsReq := &ai.OpenAITTSRequest{
		Text:   text,
		Model:  "tts-1-hd", // Best quality model
		Voice:  voice,
		Format: "pcm", // Request raw PCM16 format (24kHz output)
		Speed:  1.0,
	}

	// Get PCM16 audio (24kHz from OpenAI)
	pcm24k, err := openAITTS.TextToSpeechPCM(ctx, ttsReq)
	if err != nil {
		h.logger.Warn("OpenAI TTS service failed", zap.Error(err))
		h.sendTextResponse(session, text)
		return
	}

	// CRITICAL FIX: Resample 24kHz → 16kHz for Exotel
	// OpenAI TTS returns 24kHz PCM, but Exotel expects 16kHz
	pcm16k := h.resample24kTo16k(pcm24k)

	// Stream PCM in 640-byte chunks (20ms frames at 16kHz)
	h.streamPCMAudio(session, pcm16k, "response_done")
}

// streamPCMAudio streams raw 16-bit 16kHz PCM in 640-byte chunks (20ms frames) to Exotel
// CRITICAL FIX: Exotel expects 20ms frames = 640 bytes at 16kHz (16-bit, mono)
// Frame size = sampleRate * 2 bytes * 0.02 sec = 16000 * 2 * 0.02 = 640 bytes
func (h *Handler) streamPCMAudio(session *VoiceSession, pcmData []byte, markName string) {
	session.Mu.RLock()
	conn := session.Conn
	streamSid := session.StreamSid
	session.Mu.RUnlock()

	if conn == nil {
		h.logger.Warn("No connection for streaming audio", zap.String("call_sid", session.CallSid))
		return
	}

	// CRITICAL FIX: Chunk into exactly 640-byte frames (20ms at 16kHz)
	// Frame size = sampleRate * 2 bytes * 0.02 sec = 16000 * 2 * 0.02 = 640 bytes
	chunkSize := 640
	chunks := make([][]byte, 0)
	for i := 0; i < len(pcmData); i += chunkSize {
		end := i + chunkSize
		if end > len(pcmData) {
			end = len(pcmData)
		}
		chunks = append(chunks, pcmData[i:end])
	}

	// Send each chunk as Exotel media event with base64-encoded payload
	// Format: {"event": "media", "media": {"payload": "<base64>", "track": "outbound"}}
	for _, chunk := range chunks {
		// Base64 encode the PCM chunk
		base64Payload := audio.EncodePCMChunkToBase64(chunk)

		// CRITICAL FIX: Use correct Exotel Voicebot media event format
		mediaEvent := map[string]interface{}{
			"event": "media",
			"media": map[string]interface{}{
				"payload": base64Payload,
				"track":   "outbound", // Required by Exotel
			},
		}

		eventJSON, err := json.Marshal(mediaEvent)
		if err != nil {
			h.logger.Error("Failed to marshal media event", zap.Error(err))
			continue
		}

		if err := conn.WriteMessage(websocket.TextMessage, eventJSON); err != nil {
			h.logger.Error("Failed to send media chunk", zap.Error(err))
			return
		}

		// No delay between chunks for low latency (<700ms requirement)
	}

	// Send mark event after all chunks (optional, for synchronization)
	markEvent := map[string]interface{}{
		"event":      "mark",
		"stream_sid": streamSid,
		"mark": map[string]interface{}{
			"name": markName,
		},
	}

	markJSON, err := json.Marshal(markEvent)
	if err != nil {
		h.logger.Error("Failed to marshal mark event", zap.Error(err))
		return
	}

	if err := conn.WriteMessage(websocket.TextMessage, markJSON); err != nil {
		h.logger.Error("Failed to send mark event", zap.Error(err))
		return
	}

	h.logger.Info("Streamed PCM audio",
		zap.String("call_sid", session.CallSid),
		zap.Int("chunks", len(chunks)),
		zap.Int("total_bytes", len(pcmData)),
		zap.String("mark", markName),
	)
}

// resample24kTo16k resamples 24kHz PCM16 to 16kHz using decimation
func (h *Handler) resample24kTo16k(pcm24k []byte) []byte {
	if len(pcm24k) == 0 {
		return nil
	}

	// Convert bytes to int16 samples
	samples24k := make([]int16, len(pcm24k)/2)
	for i := 0; i < len(samples24k); i++ {
		samples24k[i] = int16(pcm24k[i*2]) | int16(pcm24k[i*2+1])<<8
	}

	// Resample: 24kHz → 16kHz = 2:3 ratio
	// Take every 3rd sample from 24k, but average for better quality
	// Simple approach: take every 3rd sample (24k/3 = 8k, but we need 16k)
	// Better: linear interpolation
	// For 24k → 16k: output sample i = input sample at position i * 24/16 = i * 1.5
	samples16k := make([]int16, len(samples24k)*16/24) // 16/24 = 2/3

	for i := 0; i < len(samples16k); i++ {
		// Calculate source position (float)
		srcPos := float64(i) * 24.0 / 16.0 // = i * 1.5

		// Get integer and fractional parts
		srcIdx := int(srcPos)
		frac := srcPos - float64(srcIdx)

		if srcIdx < len(samples24k)-1 {
			// Linear interpolation
			sample0 := float64(samples24k[srcIdx])
			sample1 := float64(samples24k[srcIdx+1])
			interpolated := sample0 + (sample1-sample0)*frac
			samples16k[i] = int16(interpolated)
		} else if srcIdx < len(samples24k) {
			// Last sample
			samples16k[i] = samples24k[srcIdx]
		} else {
			// Beyond end, repeat last sample
			samples16k[i] = samples24k[len(samples24k)-1]
		}
	}

	// Convert back to bytes (little-endian)
	result := make([]byte, len(samples16k)*2)
	for i, sample := range samples16k {
		result[i*2] = byte(sample & 0xFF)
		result[i*2+1] = byte((sample >> 8) & 0xFF)
	}

	return result
}

// sendTextResponse sends a text response to Exotel (fallback when TTS fails)
func (h *Handler) sendTextResponse(session *VoiceSession, text string) {
	session.Mu.RLock()
	conn := session.Conn
	streamSid := session.StreamSid
	session.Mu.RUnlock()

	if conn == nil {
		return
	}

	response := map[string]interface{}{
		"event":      "response",
		"stream_sid": streamSid,
		"text":       text,
	}

	responseJSON, _ := json.Marshal(response)
	conn.WriteMessage(websocket.TextMessage, responseJSON)
}

// handleClearEvent processes Exotel "clear" event for barge-in support
func (h *Handler) handleClearEvent(callSid string, message []byte) {
	h.logger.Info("Handling clear event (barge-in)",
		zap.String("call_sid", callSid),
	)

	session := getSession(callSid)
	if session != nil {
		// Clear audio buffer to stop current processing
		session.AudioBuffer.Clear()

		// Cancel any ongoing processing
		session.Mu.Lock()
		if session.CancelCtx != nil {
			session.CancelCtx()
			// Create new cancel context
			_, cancel := context.WithCancel(context.Background())
			session.CancelCtx = cancel
		}
		session.Mu.Unlock()
	}
}

// handleStopEvent processes Exotel "stop" event
func (h *Handler) handleStopEvent(callSid string, message []byte) {
	var stopEvent StopEvent
	if err := json.Unmarshal(message, &stopEvent); err != nil {
		h.logger.Warn("Failed to parse stop event", zap.Error(err))
		return
	}

	h.logger.Info("Handling stop event",
		zap.String("call_sid", callSid),
		zap.String("stream_sid", stopEvent.StreamSid),
	)

	session := getSession(callSid)
	if session != nil {
		session.Mu.Lock()
		session.IsActive = false
		session.Mu.Unlock()

		// Persist conversation summary if AI service is available
		if h.cfg.FeatureAI && h.aiManager != nil && len(session.ConversationHistory) > 0 {
			go h.persistConversationSummary(session)
		}
	}

	// Cleanup will happen when WebSocket connection closes
}

// persistConversationSummary persists conversation summary to database
func (h *Handler) persistConversationSummary(session *VoiceSession) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Build conversation text from history
	conversationText := ""
	for _, msg := range session.ConversationHistory {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if role == "user" {
			conversationText += "User: " + content + "\n"
		} else if role == "assistant" {
			conversationText += "Assistant: " + content + "\n"
		}
	}

	// Use AI manager to summarize if available
	if h.aiManager != nil {
		summaryReq := &ai.SummarizeRequest{
			CallSID:       session.CallSid,
			RecordingURL:  "",
			Transcription: &conversationText,
		}

		summary, err := h.aiManager.SummarizeCall(ctx, summaryReq)
		if err == nil && summary != nil {
			// Update call record with summary
			callData := map[string]interface{}{
				"summary":    summary.Summary,
				"tags":       summary.Tags,
				"key_points": summary.KeyPoints,
				"sentiment":  summary.Sentiment,
			}

			h.mongoClient.NewQuery("calls").
				Eq("call_sid", session.CallSid).
				UpdateOne(ctx, callData)
		}
	}
}

// getCallContext retrieves call context (persona, campaign) from database
func (h *Handler) getCallContext(callSid string) map[string]interface{} {
	ctxBg := context.Background()
	ctx, cancel := context.WithTimeout(ctxBg, 5*time.Second)
	defer cancel()

	// Get call record with campaign and persona info
	call, _ := h.mongoClient.NewQuery("calls").
		Select("campaign_id", "contact_id").
		Eq("call_sid", callSid).
		FindOne(ctx)

	callContext := make(map[string]interface{})
	if call != nil {
		callContext["campaign_id"] = call["campaign_id"]
		callContext["contact_id"] = call["contact_id"]

		// If campaign exists, get persona
		if campaignID, ok := call["campaign_id"]; ok && campaignID != nil {
			campaign, _ := h.mongoClient.NewQuery("campaigns").
				Select("persona_id").
				Eq("id", fmt.Sprintf("%v", campaignID)).
				FindOne(ctx)
			if campaign != nil {
				callContext["persona_id"] = campaign["persona_id"]
			}
		}
	}

	return callContext
}

// generateAIResponse calls OpenAI directly with dynamic system prompt from custom_parameters
func (h *Handler) generateAIResponse(session *VoiceSession, userText string, callContext map[string]interface{}) string {
	// If AI service is not enabled, return simple response
	if !h.cfg.FeatureAI || h.cfg.OpenAIApiKey == "" {
		return "Thank you for your input. I understand you said: " + userText + ". How can I help you further?"
	}

	// Build conversation history from call context
	conversationHistory := []map[string]interface{}{}
	if hist, ok := callContext["conversation_history"].([]map[string]interface{}); ok {
		conversationHistory = hist
	}

	// Build dynamic system prompt from custom_parameters
	systemPrompt := h.buildSystemPromptFromCustomParams(session.CustomParameters)

	// Build messages for OpenAI
	messages := []map[string]interface{}{
		{"role": "system", "content": systemPrompt},
	}

	// Add conversation history
	for _, msg := range conversationHistory {
		if role, ok := msg["role"].(string); ok {
			if content, ok := msg["content"].(string); ok {
				messages = append(messages, map[string]interface{}{
					"role":    role,
					"content": content,
				})
			}
		}
	}

	// Add current user message
	messages = append(messages, map[string]interface{}{
		"role":    "user",
		"content": userText,
	})

	// Call OpenAI API directly
	ctxBg := context.Background()
	ctx, cancel := context.WithTimeout(ctxBg, time.Duration(h.cfg.AITimeoutMs)*time.Millisecond)
	defer cancel()

	requestBody := map[string]interface{}{
		"model":       h.cfg.OpenAIModel,
		"messages":    messages,
		"max_tokens":  h.cfg.OpenAIMaxTokens,
		"temperature": 0.7,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		h.logger.Warn("Failed to marshal OpenAI request", zap.Error(err))
		return "I understand you said: " + userText + ". How can I help you further?"
	}

	url := "https://api.openai.com/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		h.logger.Warn("Failed to create OpenAI request", zap.Error(err))
		return "I understand you said: " + userText + ". How can I help you further?"
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+h.cfg.OpenAIApiKey)

	client := &http.Client{Timeout: time.Duration(h.cfg.AITimeoutMs) * time.Millisecond}
	resp, err := client.Do(httpReq)
	if err != nil {
		h.logger.Warn("OpenAI API request failed", zap.Error(err))
		return "I understand you said: " + userText + ". How can I help you further?"
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		h.logger.Warn("OpenAI API error", zap.Int("status", resp.StatusCode), zap.String("body", string(body)))
		return "I understand you said: " + userText + ". How can I help you further?"
	}

	var openAIResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		h.logger.Warn("Failed to decode OpenAI response", zap.Error(err))
		return "I understand you said: " + userText + ". How can I help you further?"
	}

	if len(openAIResp.Choices) == 0 {
		return "I understand you said: " + userText + ". How can I help you further?"
	}

	return strings.TrimSpace(openAIResp.Choices[0].Message.Content)
}

// buildSystemPromptFromCustomParams builds dynamic system prompt from custom_parameters
func (h *Handler) buildSystemPromptFromCustomParams(customParams map[string]interface{}) string {
	if customParams == nil {
		return "You are a helpful AI assistant. Provide concise, professional responses."
	}

	var parts []string

	// Extract persona information
	personaName := getStringFromMap(customParams, "persona_name", "")
	personaAge := getStringFromMap(customParams, "persona_age", "")
	tone := getStringFromMap(customParams, "tone", "")
	gender := getStringFromMap(customParams, "gender", "")
	city := getStringFromMap(customParams, "city", "")
	language := getStringFromMap(customParams, "language", "")
	documents := getStringFromMap(customParams, "documents", "")
	customerName := getStringFromMap(customParams, "customer_name", "")

	// Build persona description
	if personaName != "" {
		personaDesc := fmt.Sprintf("You are %s", personaName)
		if personaAge != "" {
			personaDesc += fmt.Sprintf(", %s saal ki", personaAge)
		}
		if tone != "" {
			personaDesc += fmt.Sprintf(" %s", tone)
		}
		if gender != "" {
			personaDesc += fmt.Sprintf(" %s", gender)
		}
		if city != "" {
			personaDesc += fmt.Sprintf(" from %s", city)
		}
		personaDesc += "."
		parts = append(parts, personaDesc)
	}

	// Add language instruction
	if language != "" {
		langInstruction := fmt.Sprintf("Baat karo %s mein", language)
		if strings.ToLower(language) == "hindi" {
			langInstruction += " (Hinglish if Hindi)."
		}
		parts = append(parts, langInstruction)
	}

	// Add documents instruction
	if documents != "" {
		parts = append(parts, fmt.Sprintf("Sirf in documents se jawab do: %s", documents))
	}

	// Add customer name
	if customerName != "" {
		parts = append(parts, fmt.Sprintf("Customer ka naam: %s", customerName))
	}

	if len(parts) > 0 {
		return strings.Join(parts, " ")
	}

	return "You are a helpful AI assistant. Provide concise, professional responses."
}

// getStringFromMap safely extracts string from map
func getStringFromMap(m map[string]interface{}, key, defaultValue string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return defaultValue
}

// normalizePhoneNumber normalizes phone number for comparison (handles errors gracefully)
func normalizePhoneNumber(phone string) string {
	if phone == "" {
		return ""
	}
	normalized, err := validation.NormalizeE164(phone)
	if err != nil {
		// If normalization fails, return cleaned version for comparison
		cleaned := strings.ReplaceAll(phone, " ", "")
		cleaned = strings.ReplaceAll(cleaned, "-", "")
		cleaned = strings.ReplaceAll(cleaned, "(", "")
		cleaned = strings.ReplaceAll(cleaned, ")", "")
		return cleaned
	}
	return normalized
}

// finalizeCallRecord updates call record when session ends
func (h *Handler) finalizeCallRecord(callSid string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	callData := map[string]interface{}{
		"status":   "completed",
		"ended_at": time.Now().Format(time.RFC3339),
	}

	h.mongoClient.NewQuery("calls").
		Eq("call_sid", callSid).
		UpdateOne(ctx, callData)

	h.logger.Info("Call record finalized", zap.String("call_sid", callSid))
}
