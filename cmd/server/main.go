package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"go.uber.org/zap"

	"github.com/troikatech/calling-agent/internal/api/handlers"
	"github.com/troikatech/calling-agent/pkg/ai"
	"github.com/troikatech/calling-agent/pkg/env"
	"github.com/troikatech/calling-agent/pkg/exotel"
	"github.com/troikatech/calling-agent/pkg/logger"
	"github.com/troikatech/calling-agent/pkg/middleware"
	"github.com/troikatech/calling-agent/pkg/mongo"
	"github.com/troikatech/calling-agent/pkg/otel"
	"github.com/troikatech/calling-agent/pkg/storage"
	"github.com/troikatech/calling-agent/pkg/utils"
	"github.com/troikatech/calling-agent/pkg/webhook"
)

// UnifiedServer combines API Gateway, Dialer, and Jobs services
type UnifiedServer struct {
	cfg          *env.Config
	exotelClient *exotel.Client
	mongoClient  *mongo.Client
	redisClient  *redis.Client
	storage      storage.Driver
	handler      *handlers.Handler
}

func main() {
	cfg, err := env.Load(".env")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if err := logger.Init(cfg.LogLevel, cfg.AppEnv); err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	// Initialize OpenTelemetry if enabled
	if cfg.OTELEnabled {
		shutdown, err := otel.InitTracing("unified-server", "1.0.0", cfg.OTELEndpoint)
		if err != nil {
			logger.Log.Warn("Failed to initialize OpenTelemetry", zap.Error(err))
		} else {
			defer shutdown()
			logger.Log.Info("OpenTelemetry tracing enabled", zap.String("endpoint", cfg.OTELEndpoint))
		}
	}

	logger.Log.Info("Starting Unified Server (API Gateway + Dialer + Jobs)",
		zap.String("env", cfg.AppEnv),
		zap.String("port", cfg.AppPort),
	)

	// Initialize Redis
	opt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		logger.Log.Fatal("Failed to parse Redis URL", zap.Error(err))
	}
	redisClient := redis.NewClient(opt)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Log.Fatal("Failed to connect to Redis", zap.Error(err))
	}

	// Initialize MongoDB
	mongoClient, err := mongo.NewClient(cfg.MongoURI, cfg.DBName)
	if err != nil {
		logger.Log.Fatal("Failed to connect to MongoDB", zap.Error(err))
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := mongoClient.Disconnect(ctx); err != nil {
			logger.Log.Warn("Failed to disconnect MongoDB", zap.Error(err))
		}
	}()

	// Initialize Exotel client
	exotelClient := exotel.NewClient(
		cfg.ExotelSubdomain,
		cfg.ExotelAccountSID,
		cfg.ExotelAPIKey,
		cfg.ExotelAPIToken,
	)

	// Initialize storage driver
	storageDriver, err := storage.NewDriver(
		cfg.StorageDriver,
		cfg.ExotelAccountSID,
		cfg.LocalStoragePath,
	)
	if err != nil {
		logger.Log.Fatal("Failed to create storage driver", zap.Error(err))
	}

	// Initialize AI services if enabled
	var aiManager *ai.Manager
	var ttsService *ai.TTSService
	var sttService *ai.STTService
	var personaLoader *ai.PersonaLoader

	if cfg.FeatureAI {
		timeout := time.Duration(cfg.AITimeoutMs) * time.Millisecond
		if timeout == 0 {
			timeout = 30 * time.Second
		}

		// Initialize AI providers
		providers := []ai.Provider{}

		// OpenAI provider
		if cfg.OpenAIApiKey != "" {
			openAIProvider := ai.NewOpenAIProvider(
				cfg.OpenAIApiKey,
				cfg.OpenAIModel,
				cfg.OpenAIMaxTokens,
				timeout,
				logger.Log,
			)
			if openAIProvider.IsAvailable() {
				providers = append(providers, openAIProvider)
				logger.Log.Info("OpenAI provider initialized", zap.String("model", cfg.OpenAIModel))
			}
		}

		// Gemini provider
		if cfg.GeminiApiKey != "" {
			geminiProvider := ai.NewGeminiProvider(
				cfg.GeminiApiKey,
				cfg.GeminiModel,
				timeout,
				logger.Log,
			)
			if geminiProvider.IsAvailable() {
				providers = append(providers, geminiProvider)
				logger.Log.Info("Gemini provider initialized", zap.String("model", cfg.GeminiModel))
			}
		}

		// Anthropic provider
		if cfg.AnthropicApiKey != "" {
			anthropicProvider := ai.NewAnthropicProvider(
				cfg.AnthropicApiKey,
				cfg.AnthropicModel,
				cfg.AnthropicMaxTokens,
				timeout,
				logger.Log,
			)
			if anthropicProvider.IsAvailable() {
				providers = append(providers, anthropicProvider)
				logger.Log.Info("Anthropic provider initialized", zap.String("model", cfg.AnthropicModel))
			}
		}

		// Initialize AI manager
		if len(providers) > 0 {
			aiManager = ai.NewManager(providers, logger.Log)
			logger.Log.Info("AI manager initialized", zap.Int("providers", len(providers)))
		} else {
			logger.Log.Warn("No AI providers available - AI features will be disabled")
		}

		// Initialize TTS service (ElevenLabs)
		if cfg.ElevenLabsApiKey != "" {
			ttsService = ai.NewTTSService(
				cfg.ElevenLabsApiKey,
				cfg.ElevenLabsVoiceID,
				cfg.ElevenLabsModel,
				cfg.ElevenLabsOutputFormat,
				timeout,
				logger.Log,
			)
			if ttsService.IsAvailable() {
				logger.Log.Info("TTS service initialized", zap.String("voice_id", cfg.ElevenLabsVoiceID))
			}
		}

		// Initialize STT service (OpenAI Whisper)
		if cfg.OpenAIApiKey != "" {
			sttService = ai.NewSTTService(
				cfg.OpenAIApiKey,
				cfg.WhisperModel,
				cfg.WhisperLanguage,
				timeout,
				logger.Log,
			)
			if sttService.IsAvailable() {
				logger.Log.Info("STT service initialized", zap.String("model", cfg.WhisperModel))
			}
		}

		// Initialize document loader
		docLoader := ai.NewDocumentLoader(cfg.LocalStoragePath, logger.Log)

		// Initialize persona loader
		if mongoClient != nil {
			personaLoader = ai.NewPersonaLoader(mongoClient, docLoader, logger.Log)
			logger.Log.Info("Persona loader initialized")
		}
	} else {
		logger.Log.Info("AI features are disabled")
	}

	// Initialize API Gateway handler
	apiHandler := handlers.NewHandler(cfg, redisClient, mongoClient, aiManager, ttsService, sttService, personaLoader)

	// Create unified server
	server := &UnifiedServer{
		cfg:          cfg,
		exotelClient: exotelClient,
		mongoClient:  mongoClient,
		redisClient:  redisClient,
		storage:      storageDriver,
		handler:      apiHandler,
	}

	// Setup router
	router := server.setupRouter()

	// Start Jobs background worker
	go server.startJobsWorker()

	// Start HTTP server
	srv := &http.Server{
		Addr:         ":" + cfg.AppPort,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Log.Info("Unified Server listening", zap.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Log.Fatal("Server failed to start", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Log.Info("Shutting down server...")

	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Log.Fatal("Server forced to shutdown", zap.Error(err))
	}

	logger.Log.Info("Server exited")
}

func (s *UnifiedServer) setupRouter() *gin.Engine {
	if s.cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(middleware.TraceMiddleware())
	router.Use(middleware.SecurityHeaders())
	router.Use(middleware.RequestSizeLimit(1 << 20)) // 1 MB limit

	// Add OpenTelemetry middleware if enabled
	if s.cfg.OTELEnabled {
		router.Use(otel.GinMiddleware())
	}

	router.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		return fmt.Sprintf("[%s] %s %s %d %s\n",
			param.TimeStamp.Format(time.RFC3339),
			param.Method,
			param.Path,
			param.StatusCode,
			param.Latency,
		)
	}))

	// CORS
	corsConfig := cors.DefaultConfig()
	if s.cfg.CORSAllowedOrigins == "*" {
		corsConfig.AllowAllOrigins = true
	} else {
		corsConfig.AllowOrigins = []string{s.cfg.CORSAllowedOrigins}
	}
	corsConfig.AllowCredentials = true
	corsConfig.AllowHeaders = []string{"Origin", "Content-Type", "Authorization"}
	router.Use(cors.New(corsConfig))

	rateLimiter := middleware.NewRateLimiter(s.redisClient, s.cfg.APIRateLimitRPM)

	// Health check
	router.GET("/health", s.handler.HealthCheck)
	router.GET("/metrics", s.handler.GetMetrics)
	router.GET("/metrics/prometheus", s.handler.GetPrometheusMetrics)

	// Auth endpoints
	authGroup := router.Group("/auth")
	{
		authGroup.POST("/login", s.handler.Login)
		authGroup.POST("/register", s.handler.Register)
		authGroup.POST("/refresh", s.handler.Refresh)
		authGroup.POST("/logout", middleware.AuthMiddleware(s.cfg.JWTSecret), s.handler.Logout)
	}

	// API endpoints (protected)
	api := router.Group("/api")
	api.Use(middleware.AuthMiddleware(s.cfg.JWTSecret))
	api.Use(middleware.IdempotencyMiddleware(s.redisClient))
	api.Use(rateLimiter.Middleware())
	{
		contacts := api.Group("/contacts")
		{
			contacts.POST("/import", s.handler.ImportContacts)
			contacts.GET("/search", s.handler.SearchContacts)
			contacts.GET("/:id", middleware.ValidateIDParam("id"), s.handler.GetContact)
			contacts.PUT("/:id", middleware.ValidateIDParam("id"), s.handler.UpdateContact)
			contacts.DELETE("/:id", middleware.ValidateIDParam("id"), s.handler.DeleteContact)
		}

		suppression := api.Group("/suppression")
		{
			suppression.POST("/add", s.handler.AddSuppression)
			suppression.GET("/check/:phone", middleware.ValidatePhoneParam("phone"), s.handler.CheckSuppression)
		}

		calls := api.Group("/calls")
		{
			calls.POST("", s.CreateCall) // Use unified dialer directly
			calls.GET("/:call_sid", s.handler.GetCall)
		}

		campaigns := api.Group("/campaigns")
		{
			campaigns.POST("", middleware.RoleMiddleware("admin", "manager"), s.handler.CreateCampaign)
			campaigns.GET("", s.handler.ListCampaigns)
			campaigns.GET("/:id", middleware.ValidateIDParam("id"), s.handler.GetCampaign)
			campaigns.POST("/:id/pause", middleware.ValidateIDParam("id"), middleware.RoleMiddleware("admin", "manager"), s.handler.PauseCampaign)
			campaigns.POST("/:id/resume", middleware.ValidateIDParam("id"), middleware.RoleMiddleware("admin", "manager"), s.handler.ResumeCampaign)
			campaigns.POST("/:id/cancel", middleware.ValidateIDParam("id"), middleware.RoleMiddleware("admin", "manager"), s.handler.CancelCampaign)
			campaigns.GET("/:id/contacts", middleware.ValidateIDParam("id"), s.handler.GetCampaignContacts)
		}

		recordings := api.Group("/recordings")
		{
			recordings.GET("/:call_sid", s.handler.GetRecording)
		}

		analytics := api.Group("/analytics")
		{
			analytics.GET("/overview", s.handler.GetAnalyticsOverview)
			analytics.GET("/drilldown", s.handler.GetAnalyticsDrilldown)
		}

		personas := api.Group("/personas")
		{
			personas.GET("", s.handler.ListPersonas)
			personas.GET("/:id", middleware.ValidateIDParam("id"), s.handler.GetPersona)
			personas.POST("", middleware.RoleMiddleware("admin"), s.handler.CreatePersona)
			personas.PUT("/:id", middleware.ValidateIDParam("id"), middleware.RoleMiddleware("admin"), s.handler.UpdatePersona)
			personas.POST("/:id/documents", middleware.ValidateIDParam("id"), middleware.RoleMiddleware("admin"), s.handler.LinkDocumentToPersona)
		}

		documents := api.Group("/documents")
		{
			documents.POST("", middleware.RoleMiddleware("admin", "manager"), s.handler.UploadDocument)
			documents.GET("", s.handler.ListDocuments)
			documents.GET("/:id", middleware.ValidateIDParam("id"), s.handler.GetDocument)
			documents.DELETE("/:id", middleware.ValidateIDParam("id"), middleware.RoleMiddleware("admin", "manager"), s.handler.DeleteDocument)
			documents.GET("/download/:filename", s.handler.DownloadDocument)
		}

		users := api.Group("/users")
		{
			users.GET("", middleware.RoleMiddleware("admin"), s.handler.ListUsers)
			users.GET("/:id", handlers.ValidateUUIDParam("id"), s.handler.GetUser)
			users.PUT("/:id", handlers.ValidateUUIDParam("id"), middleware.RoleMiddleware("admin"), s.handler.UpdateUser)
			users.DELETE("/:id", handlers.ValidateUUIDParam("id"), middleware.RoleMiddleware("admin"), s.handler.DeleteUser)
			users.POST("/:id/activate", handlers.ValidateUUIDParam("id"), middleware.RoleMiddleware("admin"), s.handler.ActivateUser)
			users.POST("/:id/deactivate", handlers.ValidateUUIDParam("id"), middleware.RoleMiddleware("admin"), s.handler.DeactivateUser)
		}

		auditLogs := api.Group("/audit-logs")
		{
			auditLogs.GET("", middleware.RoleMiddleware("admin", "auditor"), s.handler.ListAuditLogs)
		}

		if s.cfg.FeatureAI {
			ai := api.Group("/ai")
			{
				ai.POST("/script", s.handler.GenerateScript)
				ai.POST("/summarize", s.handler.SummarizeCall)
				ai.POST("/conversation", s.handler.Conversation)
				ai.POST("/tts", s.handler.TextToSpeech)
				ai.GET("/tts/voices", s.handler.GetTTSVoices)
				ai.POST("/stt", s.handler.SpeechToText)
			}
		}
	}

	// Internal dialer endpoints (no auth, internal use)
	internal := router.Group("/internal")
	{
		internal.POST("/calls/initiate", s.InitiateCall)
		internal.POST("/campaigns/schedule", s.ScheduleCampaign)
	}

	// Webhook endpoint (public, HMAC verified)
	router.POST("/webhooks/exotel", s.ProcessWebhook)

	// Exotel Voicebot endpoints (public, no auth)
	// Support both GET and POST for /voicebot/init (Exotel may use either)
	router.GET("/voicebot/init", s.handler.ExotelVoicebotEndpoint)
	router.POST("/voicebot/init", s.handler.ExotelVoicebotEndpoint)
	router.GET("/voicebot/ws", s.handler.VoicebotWebSocket)

	// Also support /was endpoint as per original requirements
	router.GET("/was", s.handler.VoicebotWebSocket)

	return router
}

// InitiateCall - Dialer service endpoint (integrated)
func (s *UnifiedServer) InitiateCall(c *gin.Context) {
	var req struct {
		From     string `json:"from" binding:"required"`
		To       string `json:"to" binding:"required"`
		FlowID   string `json:"flow_id" binding:"required"`
		CallerID string `json:"caller_id"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.CallerID == "" {
		req.CallerID = s.cfg.ExotelExophone
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	// Compliance Check 1: Suppression list
	suppressed, _ := s.mongoClient.NewQuery("suppression").
		Select("msisdn_e164").
		Eq("msisdn_e164", req.To).
		Find(ctx)

	if len(suppressed) > 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "number is suppressed"})
		return
	}

	// Compliance Check 2: DND and Consent
	contact, _ := s.mongoClient.NewQuery("contacts").
		Select("dnd", "consent").
		Eq("msisdn_e164", req.To).
		FindOne(ctx)

	if contact != nil {
		if dnd, ok := contact["dnd"].(bool); ok && dnd {
			c.JSON(http.StatusForbidden, gin.H{"error": "contact has DND enabled"})
			return
		}
		if consent, ok := contact["consent"].(bool); ok && !consent {
			c.JSON(http.StatusForbidden, gin.H{"error": "contact has not provided consent"})
			return
		}
	}

	// Compliance Check 3: Business hours
	hour := time.Now().In(time.Local).Hour()
	if hour < s.cfg.DialBusinessStartHour || hour >= s.cfg.DialBusinessEndHour {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "outside business hours",
			"window": fmt.Sprintf("%d:00 - %d:00 IST",
				s.cfg.DialBusinessStartHour,
				s.cfg.DialBusinessEndHour,
			),
		})
		return
	}

	// Use flow_id if provided, otherwise use configured Applet ID
	appletID := req.FlowID
	if appletID == "" {
		appletID = s.cfg.ExotelVoicebotAppletID
	}

	callReq := exotel.ConnectCallRequest{
		From:        req.From,
		To:          req.To,
		CallerID:    req.CallerID,
		CallType:    "trans",
		CallbackURL: "",
		AppletID:    appletID,               // Use flow_id or configured Applet ID for voicebot
		AccountSID:  s.cfg.ExotelAccountSID, // Required for building voicebot URL
	}

	resp, err := s.exotelClient.ConnectCall(callReq)
	if err != nil {
		logger.Log.Error("Failed to initiate call",
			zap.Error(err),
			zap.String("from", req.From),
			zap.String("to", req.To),
			zap.String("applet_id", appletID),
			zap.String("account_sid", s.cfg.ExotelAccountSID),
		)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to initiate call",
			"details": err.Error(),
		})
		return
	}

	// Log successful call initiation with details
	logger.Log.Info("Call initiated successfully",
		zap.String("call_sid", resp.Call.Sid),
		zap.String("status", resp.Call.Status),
		zap.String("direction", resp.Call.Direction),
		zap.String("from", req.From),
		zap.String("to", req.To),
		zap.String("applet_id", appletID),
	)

	callData := map[string]interface{}{
		"call_sid":    resp.Call.Sid,
		"direction":   "outbound",
		"from_number": req.From,
		"to_number":   req.To,
		"flow_id":     req.FlowID,
		"caller_id":   req.CallerID,
		"status":      resp.Call.Status,
		"started_at":  time.Now().Format(time.RFC3339),
		"created_at":  time.Now().Format(time.RFC3339),
	}

	s.mongoClient.NewQuery("calls").Insert(ctx, callData)

	c.JSON(http.StatusOK, gin.H{
		"call_sid": resp.Call.Sid,
		"status":   resp.Call.Status,
		"message":  "call initiated",
	})
}

// CreateCall - API Gateway endpoint that uses unified dialer directly
func (s *UnifiedServer) CreateCall(c *gin.Context) {
	var req struct {
		From   string `json:"from" binding:"required"`
		To     string `json:"to" binding:"required"`
		FlowID string `json:"flow_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Call InitiateCall logic directly (no HTTP call needed)
	// We'll reuse the InitiateCall function but with proper request structure
	internalReq := struct {
		From     string `json:"from"`
		To       string `json:"to"`
		FlowID   string `json:"flow_id"`
		CallerID string `json:"caller_id"`
	}{
		From:     req.From,
		To:       req.To,
		FlowID:   req.FlowID,
		CallerID: s.cfg.ExotelExophone,
	}

	// Manually call the dialer logic
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	// Compliance Check 1: Suppression list
	suppressed, _ := s.mongoClient.NewQuery("suppression").
		Select("msisdn_e164").
		Eq("msisdn_e164", internalReq.To).
		Find(ctx)

	if len(suppressed) > 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "number is suppressed"})
		return
	}

	// Compliance Check 2: DND and Consent
	contact, _ := s.mongoClient.NewQuery("contacts").
		Select("dnd", "consent").
		Eq("msisdn_e164", internalReq.To).
		FindOne(ctx)

	if contact != nil {
		if dnd, ok := contact["dnd"].(bool); ok && dnd {
			c.JSON(http.StatusForbidden, gin.H{"error": "contact has DND enabled"})
			return
		}
		if consent, ok := contact["consent"].(bool); ok && !consent {
			c.JSON(http.StatusForbidden, gin.H{"error": "contact has not provided consent"})
			return
		}
	}

	// Compliance Check 3: Business hours
	hour := time.Now().In(time.Local).Hour()
	if hour < s.cfg.DialBusinessStartHour || hour >= s.cfg.DialBusinessEndHour {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "outside business hours",
			"window": fmt.Sprintf("%d:00 - %d:00 IST",
				s.cfg.DialBusinessStartHour,
				s.cfg.DialBusinessEndHour,
			),
		})
		return
	}

	// Use flow_id if provided, otherwise use configured Applet ID
	appletID := internalReq.FlowID
	if appletID == "" {
		appletID = s.cfg.ExotelVoicebotAppletID
	}

	callReq := exotel.ConnectCallRequest{
		From:        internalReq.From,
		To:          internalReq.To,
		CallerID:    internalReq.CallerID,
		CallType:    "trans",
		CallbackURL: "",
		AppletID:    appletID,               // Use flow_id or configured Applet ID for voicebot
		AccountSID:  s.cfg.ExotelAccountSID, // Required for building voicebot URL
	}

	resp, err := s.exotelClient.ConnectCall(callReq)
	if err != nil {
		logger.Log.Error("Failed to initiate call",
			zap.Error(err),
			zap.String("from", internalReq.From),
			zap.String("to", internalReq.To),
			zap.String("applet_id", internalReq.FlowID),
		)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to initiate call",
			"details": err.Error(),
		})
		return
	}

	callData := map[string]interface{}{
		"call_sid":    resp.Call.Sid,
		"direction":   "outbound",
		"from_number": internalReq.From,
		"to_number":   internalReq.To,
		"flow_id":     internalReq.FlowID,
		"caller_id":   internalReq.CallerID,
		"status":      resp.Call.Status,
		"started_at":  time.Now().Format(time.RFC3339),
		"created_at":  time.Now().Format(time.RFC3339),
	}

	s.mongoClient.NewQuery("calls").Insert(ctx, callData)

	c.JSON(http.StatusOK, gin.H{
		"call_sid": resp.Call.Sid,
		"status":   resp.Call.Status,
		"message":  "call initiated",
	})
}

// ScheduleCampaign - Dialer service endpoint (integrated)
func (s *UnifiedServer) ScheduleCampaign(c *gin.Context) {
	var req struct {
		CampaignID int64 `json:"campaign_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	_, err := s.mongoClient.NewQuery("campaigns").
		Eq("id", fmt.Sprintf("%d", req.CampaignID)).
		UpdateOne(ctx, map[string]interface{}{"status": "scheduled"})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to schedule campaign"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"campaign_id": req.CampaignID,
		"status":      "scheduled",
		"message":     "campaign scheduled for processing",
	})
}

// startJobsWorker - Jobs service background worker (integrated)
func (s *UnifiedServer) startJobsWorker() {
	logger.Log.Info("Jobs Service worker started, processing campaigns every 30s")

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for {
		select {
		case <-ticker.C:
			s.processCampaigns(ctx)
		case <-ctx.Done():
			logger.Log.Info("Jobs Service worker stopped")
			return
		}
	}
}

func (s *UnifiedServer) processCampaigns(ctx context.Context) {
	logger.Log.Info("Processing scheduled campaigns...")

	campaigns, err := s.mongoClient.NewQuery("campaigns").
		Select("id", "name", "status", "window_start", "window_end", "flow_id").
		In("status", []string{"scheduled", "running"}).
		Find(ctx)

	if err != nil {
		logger.Log.Error("Failed to fetch campaigns", zap.Error(err))
		return
	}

	hour := time.Now().In(time.Local).Hour()

	for _, campaign := range campaigns {
		campaignID := fmt.Sprintf("%v", campaign["id"])
		status := fmt.Sprintf("%v", campaign["status"])
		windowStart := int(campaign["window_start"].(float64))
		windowEnd := int(campaign["window_end"].(float64))
		flowID := fmt.Sprintf("%v", campaign["flow_id"])

		if hour < windowStart || hour >= windowEnd {
			logger.Log.Debug("Campaign outside business hours",
				zap.String("campaign_id", campaignID),
				zap.Int("hour", hour),
			)
			continue
		}

		if status == "scheduled" {
			s.mongoClient.NewQuery("campaigns").
				Eq("id", campaignID).
				UpdateOne(ctx, map[string]interface{}{"status": "running"})

			logger.Log.Info("Campaign started",
				zap.String("campaign_id", campaignID),
			)
		}

		pendingContacts, err := s.mongoClient.NewQuery("campaign_contacts").
			Select("campaign_id", "contact_id", "attempts", "next_attempt_at").
			Eq("campaign_id", campaignID).
			Eq("status", "pending").
			Limit(10).
			Find(ctx)

		if err != nil {
			logger.Log.Error("Failed to fetch pending contacts", zap.Error(err))
			continue
		}

		for _, contact := range pendingContacts {
			nextAttempt := contact["next_attempt_at"]
			if nextAttempt != nil {
				attemptTime, _ := time.Parse(time.RFC3339, nextAttempt.(string))
				if time.Now().Before(attemptTime) {
					continue
				}
			}

			contactID := fmt.Sprintf("%v", contact["contact_id"])

			key := fmt.Sprintf("campaign:%s:contact:%s", campaignID, contactID)
			taken, _ := s.redisClient.SetNX(ctx, key, "processing", 5*time.Minute).Result()

			if !taken {
				continue
			}

			// Fetch contact details
			contactData, err := s.mongoClient.NewQuery("contacts").
				Select("msisdn_e164", "dnd", "consent").
				Eq("id", contactID).
				FindOne(ctx)

			if err != nil || contactData == nil {
				logger.Log.Warn("Contact not found, skipping",
					zap.String("contact_id", contactID),
				)
				s.mongoClient.NewQuery("campaign_contacts").
					Eq("campaign_id", campaignID).
					Eq("contact_id", contactID).
					UpdateOne(ctx, map[string]interface{}{"status": "skipped"})
				continue
			}
			phone := fmt.Sprintf("%v", contactData["msisdn_e164"])

			// Compliance checks
			suppressed, _ := s.mongoClient.NewQuery("suppression").
				Select("msisdn_e164").
				Eq("msisdn_e164", phone).
				Find(ctx)

			if len(suppressed) > 0 {
				logger.Log.Info("Contact suppressed, skipping",
					zap.String("contact_id", contactID),
					zap.String("phone", utils.MaskPhoneNumber(phone)),
				)
				s.mongoClient.NewQuery("campaign_contacts").
					Eq("campaign_id", campaignID).
					Eq("contact_id", contactID).
					UpdateOne(ctx, map[string]interface{}{"status": "skipped"})
				continue
			}

			if dnd, ok := contactData["dnd"].(bool); ok && dnd {
				logger.Log.Info("Contact has DND enabled, skipping",
					zap.String("contact_id", contactID),
				)
				s.mongoClient.NewQuery("campaign_contacts").
					Eq("campaign_id", campaignID).
					Eq("contact_id", contactID).
					UpdateOne(ctx, map[string]interface{}{"status": "skipped"})
				continue
			}

			if consent, ok := contactData["consent"].(bool); ok && !consent {
				logger.Log.Info("Contact has no consent, skipping",
					zap.String("contact_id", contactID),
				)
				s.mongoClient.NewQuery("campaign_contacts").
					Eq("campaign_id", campaignID).
					Eq("contact_id", contactID).
					UpdateOne(ctx, map[string]interface{}{"status": "skipped"})
				continue
			}

			// Update status
			attempts := 0
			if attemptsVal, ok := contact["attempts"]; ok {
				switch v := attemptsVal.(type) {
				case float64:
					attempts = int(v)
				case int:
					attempts = v
				case int64:
					attempts = int(v)
				}
			}
			s.mongoClient.NewQuery("campaign_contacts").
				Eq("campaign_id", campaignID).
				Eq("contact_id", contactID).
				UpdateOne(ctx, map[string]interface{}{
					"status":          "calling",
					"last_attempt_at": time.Now().Format(time.RFC3339),
					"attempts":        attempts + 1,
				})

			// Initiate call directly (no HTTP call needed)
			callReq := exotel.ConnectCallRequest{
				From:        s.cfg.ExotelExophone,
				To:          phone,
				CallerID:    s.cfg.ExotelExophone,
				CallType:    "trans",
				CallbackURL: "",
			}

			resp, err := s.exotelClient.ConnectCall(callReq)
			if err != nil {
				logger.Log.Error("Failed to initiate call",
					zap.String("contact_id", contactID),
					zap.Error(err),
				)
				s.mongoClient.NewQuery("campaign_contacts").
					Eq("campaign_id", campaignID).
					Eq("contact_id", contactID).
					UpdateOne(ctx, map[string]interface{}{"status": "failed"})
				continue
			}

			// Save call record
			callData := map[string]interface{}{
				"call_sid":    resp.Call.Sid,
				"direction":   "outbound",
				"from_number": s.cfg.ExotelExophone,
				"to_number":   phone,
				"flow_id":     flowID,
				"caller_id":   s.cfg.ExotelExophone,
				"status":      resp.Call.Status,
				"started_at":  time.Now().Format(time.RFC3339),
				"created_at":  time.Now().Format(time.RFC3339),
			}
			s.mongoClient.NewQuery("calls").Insert(ctx, callData)

			// Update campaign contact
			s.mongoClient.NewQuery("campaign_contacts").
				Eq("campaign_id", campaignID).
				Eq("contact_id", contactID).
				UpdateOne(ctx, map[string]interface{}{"call_sid": resp.Call.Sid})

			logger.Log.Info("Call initiated successfully",
				zap.String("contact_id", contactID),
				zap.String("call_sid", resp.Call.Sid),
			)
		}
	}

	logger.Log.Info("Campaign processing cycle complete")
}

// ProcessWebhook - Webhook handler (integrated from dialer service)
func (s *UnifiedServer) ProcessWebhook(c *gin.Context) {
	// Parse form data
	if err := c.Request.ParseForm(); err != nil {
		logger.Log.Error("Failed to parse form", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid form data"})
		return
	}

	// Verify HMAC signature (only if secret is configured)
	if s.cfg.ExotelWebhookSecret != "" {
		signature := c.GetHeader("X-Exotel-Signature")
		if err := webhook.VerifyExotelSignature(s.cfg.ExotelWebhookSecret, c.Request.PostForm, signature); err != nil {
			logger.Log.Warn("Webhook signature verification failed",
				zap.Error(err),
				zap.String("signature", signature),
			)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
			return
		}
	} else {
		logger.Log.Info("Webhook signature verification skipped (secret not configured)")
	}

	// Extract CallSid for idempotency
	callSid := c.Request.PostForm.Get("CallSid")
	if callSid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "CallSid is required"})
		return
	}

	// Idempotency check using Redis (TTL: 24 hours)
	ctx := context.Background()
	idempotencyKey := fmt.Sprintf("webhook:exotel:%s", callSid)

	// Check if already processed
	exists, err := s.redisClient.Exists(ctx, idempotencyKey).Result()
	if err == nil && exists > 0 {
		logger.Log.Info("Webhook already processed (idempotent)",
			zap.String("call_sid", callSid),
		)
		c.JSON(http.StatusOK, gin.H{"message": "webhook already processed"})
		return
	}

	// Mark as processing (TTL: 24 hours)
	s.redisClient.Set(ctx, idempotencyKey, "processing", 24*time.Hour)

	// Bind payload
	var payload ExotelWebhookPayload
	if err := c.ShouldBind(&payload); err != nil {
		logger.Log.Error("Failed to bind webhook payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	// Process webhook
	if err := s.processWebhookPayload(ctx, &payload); err != nil {
		logger.Log.Error("Failed to process webhook",
			zap.Error(err),
			zap.String("call_sid", callSid),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process webhook"})
		return
	}

	// Mark as completed
	s.redisClient.Set(ctx, idempotencyKey, "completed", 24*time.Hour)

	c.JSON(http.StatusOK, gin.H{"message": "webhook processed"})
}

// ExotelWebhookPayload represents Exotel webhook payload
type ExotelWebhookPayload struct {
	CallSid          string `form:"CallSid"`
	From             string `form:"From"`
	To               string `form:"To"`
	Direction        string `form:"Direction"`
	Status           string `form:"Status"`
	StartTime        string `form:"StartTime"`
	EndTime          string `form:"EndTime"`
	Duration         string `form:"Duration"`
	RecordingUrl     string `form:"RecordingUrl"`
	DialCallStatus   string `form:"DialCallStatus"`
	DialCallDuration string `form:"DialCallDuration"`
	Digits           string `form:"Digits"`
}

func (s *UnifiedServer) processWebhookPayload(ctx context.Context, payload *ExotelWebhookPayload) error {
	// Parse timestamps
	var startedAt, endedAt interface{}
	if payload.StartTime != "" {
		startedAt = payload.StartTime
	}
	if payload.EndTime != "" {
		endedAt = payload.EndTime
	}

	// Parse duration
	var durationSec int
	if payload.Duration != "" {
		fmt.Sscanf(payload.Duration, "%d", &durationSec)
	}

	// Upsert call record (idempotent by CallSid)
	callData := map[string]interface{}{
		"call_sid":            payload.CallSid,
		"from_number":         payload.From,
		"to_number":           payload.To,
		"direction":           payload.Direction,
		"status":              payload.Status,
		"started_at":          startedAt,
		"ended_at":            endedAt,
		"duration_sec":        durationSec,
		"recording_url":       payload.RecordingUrl,
		"webhook_received_at": time.Now().Format(time.RFC3339),
	}

	// Check if call exists
	existingCall, _ := s.mongoClient.NewQuery("calls").
		Select("call_sid").
		Eq("call_sid", payload.CallSid).
		FindOne(ctx)

	if existingCall != nil {
		// Update existing
		_, err := s.mongoClient.NewQuery("calls").
			Eq("call_sid", payload.CallSid).
			UpdateOne(ctx, callData)
		if err != nil {
			return fmt.Errorf("failed to update call: %w", err)
		}
	} else {
		// Insert new
		callData["created_at"] = time.Now().Format(time.RFC3339)
		_, err := s.mongoClient.NewQuery("calls").Insert(ctx, callData)
		if err != nil {
			return fmt.Errorf("failed to insert call: %w", err)
		}
	}

	// Handle DTMF digits (opt-out on "3")
	if payload.Digits != "" {
		// Update campaign_contacts with DTMF
		s.mongoClient.NewQuery("campaign_contacts").
			Eq("call_sid", payload.CallSid).
			UpdateOne(ctx, map[string]interface{}{
				"ivr_digits": payload.Digits,
			})

		// Opt-out handling
		if payload.Digits == "3" {
			// Add to suppression list (idempotent by msisdn_e164)
			suppressionData := map[string]interface{}{
				"msisdn_e164": payload.To,
				"source":      "ivr",
				"reason":      "opt-out via DTMF",
				"created_at":  time.Now().Format(time.RFC3339),
			}
			s.mongoClient.NewQuery("suppression").Insert(ctx, suppressionData)

			// Update campaign_contacts status to skipped
			s.mongoClient.NewQuery("campaign_contacts").
				Eq("call_sid", payload.CallSid).
				UpdateOne(ctx, map[string]interface{}{
					"status":      "skipped",
					"disposition": "opt_out",
				})
		}
	}

	// Update campaign_contacts status based on call status
	if payload.Status == "completed" {
		s.mongoClient.NewQuery("campaign_contacts").
			Eq("call_sid", payload.CallSid).
			UpdateOne(ctx, map[string]interface{}{
				"status":      "completed",
				"disposition": "answered",
			})
	} else if payload.Status == "no-answer" || payload.Status == "busy" || payload.Status == "failed" {
		s.mongoClient.NewQuery("campaign_contacts").
			Eq("call_sid", payload.CallSid).
			UpdateOne(ctx, map[string]interface{}{
				"status":      "failed",
				"disposition": payload.Status,
			})
	}

	return nil
}
