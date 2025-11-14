package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	CallSid string `json:"CallSid" form:"CallSid"`
	From    string `json:"From" form:"From"`
	To      string `json:"To" form:"To"`
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

	// Try to bind from query params (GET) or form/json (POST)
	if err := c.ShouldBind(&req); err != nil {
		// If binding fails, try to get from query params directly
		req.CallSid = c.Query("CallSid")
		req.From = c.Query("From")
		req.To = c.Query("To")
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

	if req.CallSid == "" {
		h.logger.Warn("ExotelVoicebotEndpoint called without CallSid",
			zap.String("method", c.Request.Method),
			zap.String("url", c.Request.URL.String()),
			zap.Any("query", c.Request.URL.Query()),
		)
		errors.BadRequest(c, "CallSid is required")
		return
	}

	h.logger.Info("ExotelVoicebotEndpoint called",
		zap.String("call_sid", req.CallSid),
		zap.String("from", req.From),
		zap.String("to", req.To),
		zap.String("method", c.Request.Method),
	)

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

	// WebSocket endpoint with call parameters
	wsURL := fmt.Sprintf("%s/voicebot/ws?call_sid=%s&from=%s&to=%s",
		wsBaseURL,
		req.CallSid,
		req.From,
		req.To,
	)

	h.logger.Info("Generated WebSocket URL for Exotel",
		zap.String("call_sid", req.CallSid),
		zap.String("ws_url", wsURL),
	)

	response := VoicebotWebSocketResponse{
		WebSocketURL: wsURL,
	}

	c.JSON(http.StatusOK, response)
}

// createWebSocketUpgrader creates a secure WebSocket upgrader with origin validation
func createWebSocketUpgrader(cfg *env.Config) websocket.Upgrader {
	return websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")

			// In development, allow all origins
			if cfg.AppEnv == "development" {
				return true
			}

			// In production, validate Exotel origins
			// Exotel WebSocket connections come from their infrastructure
			// Allow common Exotel domains and your own domain
			allowedOrigins := []string{
				"https://my.exotel.com",
				"https://api.exotel.com",
				"https://" + cfg.ExotelSubdomain + ".exotel.com",
			}

			// Also allow if origin matches our base URL
			if cfg.VoicebotBaseURL != "" {
				allowedOrigins = append(allowedOrigins, cfg.VoicebotBaseURL)
			}

			for _, allowed := range allowedOrigins {
				if origin == allowed || origin == "" {
					return true
				}
			}

			// Log rejected origins for security monitoring
			logger.Log.Warn("WebSocket connection rejected - invalid origin",
				zap.String("origin", origin),
				zap.String("remote_addr", r.RemoteAddr),
			)
			return false
		},
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		// Enable compression for better performance
		EnableCompression: true,
	}
}

// VoicebotWebSocket handles WebSocket connection from Exotel Voicebot
// This is the actual WebSocket endpoint that Exotel connects to for real-time audio streaming
// Must be accessible via public wss:// URL for Exotel to connect
// No authentication required (direct connect as per requirements)
func (h *Handler) VoicebotWebSocket(c *gin.Context) {
	// Get call parameters from query string (Exotel sends call_sid or callLogId)
	callSid := c.Query("call_sid")
	if callSid == "" {
		// Try callLogId as fallback (Exotel may use either)
		callSid = c.Query("callLogId")
	}
	from := c.Query("from")
	to := c.Query("to")

	// Get sample-rate from query parameter (default 16000)
	sampleRateStr := c.Query("sample-rate")
	sampleRate := 16000 // Default 16kHz
	if sampleRateStr != "" {
		if sr, err := strconv.Atoi(sampleRateStr); err == nil && sr > 0 {
			sampleRate = sr
		}
	}

	if callSid == "" {
		errors.BadRequest(c, "call_sid or callLogId is required")
		return
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
	)

	// Create or update call record in database
	h.initializeCallRecord(callSid, from, to)

	// Handle WebSocket messages - Exotel sends JSON events, not binary
	h.handleVoicebotConnection(conn, callSid, from, to, sampleRate)
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
func (h *Handler) handleVoicebotConnection(conn *websocket.Conn, callSid, from, to string, sampleRate int) {
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
				h.handleExotelEvent(conn, callSid, from, to, message, sampleRate)
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
				zap.String("call_sid", callSid),
			)
			// Clean up session
			removeSession(callSid)
			h.finalizeCallRecord(callSid)
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
func (h *Handler) handleExotelEvent(conn *websocket.Conn, callSid, from, to string, message []byte, sampleRate int) {
	var event ExotelEvent
	if err := json.Unmarshal(message, &event); err != nil {
		h.logger.Warn("Failed to parse Exotel event", zap.Error(err), zap.String("raw", string(message)))
		return
	}

	h.logger.Info("Received Exotel event",
		zap.String("call_sid", callSid),
		zap.String("event", event.Event),
		zap.String("stream_sid", event.StreamSid),
	)

	switch event.Event {
	case "start":
		h.handleStartEvent(conn, callSid, from, to, message, sampleRate)
	case "media":
		h.handleMediaEvent(conn, callSid, message)
	case "stop":
		h.handleStopEvent(callSid, message)
	case "clear":
		h.handleClearEvent(callSid, message)
	default:
		h.logger.Debug("Unknown Exotel event", zap.String("event", event.Event))
	}
}

// handleStartEvent processes Exotel "start" event
// On start: create session, extract custom_parameters, trigger greeting TTS
func (h *Handler) handleStartEvent(conn *websocket.Conn, callSid, from, to string, message []byte, sampleRate int) {
	var startEvent StartEvent
	if err := json.Unmarshal(message, &startEvent); err != nil {
		h.logger.Warn("Failed to parse start event", zap.Error(err))
		return
	}

	// Create or get session
	session := getOrCreateSession(callSid, startEvent.StreamSid, from, to, conn, sampleRate)

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

	h.logger.Info("Handling start event, sending greeting",
		zap.String("call_sid", callSid),
		zap.String("stream_sid", startEvent.StreamSid),
		zap.Any("custom_parameters", startEvent.CustomParameters),
		zap.Int("sample_rate", sampleRate),
	)

	// Send greeting TTS in a goroutine
	go h.sendGreeting(session)
}

// sendGreeting sends TTS greeting to Exotel in chunked PCM format
func (h *Handler) sendGreeting(session *VoiceSession) {
	greetingText := "Hello! How can I help you today?"
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

	// Use OpenAI TTS service
	openAITTS := ai.NewOpenAITTSService(h.cfg.OpenAIApiKey, 10*time.Second, h.logger)
	ttsReq := &ai.OpenAITTSRequest{
		Text:   greetingText,
		Model:  "tts-1-hd", // Best quality
		Voice:  voice,
		Format: "mp3",
		Speed:  1.0,
	}

	pcmData, err := openAITTS.TextToSpeechPCM(ctx, ttsReq)
	if err != nil {
		h.logger.Warn("OpenAI TTS greeting failed", zap.Error(err))
		h.sendTextResponse(session, greetingText)
		return
	}

	// Stream PCM in 640-byte chunks
	h.streamPCMAudio(session, pcmData, "greeting_done")
}

// handleMediaEvent processes Exotel "media" event with base64-encoded PCM audio
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

	// Decode base64 PCM audio
	pcmData, err := audio.DecodeBase64PCM(mediaEvent.Media.Payload)
	if err != nil {
		h.logger.Warn("Failed to decode base64 PCM", zap.Error(err))
		return
	}

	// Append to audio buffer
	session.AudioBuffer.Append(pcmData)

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

// callSTTService calls OpenAI Whisper to convert audio to text
func (h *Handler) callSTTService(session *VoiceSession, audioData []byte) string {
	if !h.cfg.FeatureAI || h.cfg.OpenAIApiKey == "" {
		return ""
	}

	// Convert PCM to WAV format for STT service
	// Note: OpenAI Whisper accepts raw PCM, but we'll format it as WAV
	session.Mu.RLock()
	sampleRate := session.SampleRate
	session.Mu.RUnlock()
	if sampleRate == 0 {
		sampleRate = 16000 // Default 16kHz
	}
	wavData := h.convertPCMToWAV(audioData, sampleRate)

	// Use OpenAI Whisper directly
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Call OpenAI Whisper API
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

	// Use OpenAI TTS service
	openAITTS := ai.NewOpenAITTSService(h.cfg.OpenAIApiKey, 10*time.Second, h.logger)
	ttsReq := &ai.OpenAITTSRequest{
		Text:   text,
		Model:  "tts-1-hd", // Best quality
		Voice:  voice,
		Format: "mp3",
		Speed:  1.0,
	}

	pcmData, err := openAITTS.TextToSpeechPCM(ctx, ttsReq)
	if err != nil {
		h.logger.Warn("OpenAI TTS service failed", zap.Error(err))
		h.sendTextResponse(session, text)
		return
	}

	// Stream PCM in 640-byte chunks
	h.streamPCMAudio(session, pcmData, "response_done")
}

// streamPCMAudio streams raw 16-bit 16kHz PCM in 640-byte chunks to Exotel
// Sends media events with base64-encoded payload as required by Exotel
func (h *Handler) streamPCMAudio(session *VoiceSession, pcmData []byte, markName string) {
	session.Mu.RLock()
	conn := session.Conn
	streamSid := session.StreamSid
	session.Mu.RUnlock()

	if conn == nil {
		h.logger.Warn("No connection for streaming audio", zap.String("call_sid", session.CallSid))
		return
	}

	// Chunk PCM data into 640-byte chunks (no gaps)
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
	for _, chunk := range chunks {
		// Base64 encode the PCM chunk
		base64Payload := audio.EncodePCMChunkToBase64(chunk)

		mediaEvent := map[string]interface{}{
			"event":      "media",
			"stream_sid": streamSid,
			"media": map[string]interface{}{
				"payload": base64Payload,
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

	// Send mark event after all chunks
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
		zap.String("mark", markName),
	)
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
