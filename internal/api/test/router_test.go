package test

import (
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/troikatech/calling-agent/internal/api/handlers"
	"github.com/troikatech/calling-agent/pkg/ai"
	"github.com/troikatech/calling-agent/pkg/env"
	"github.com/troikatech/calling-agent/pkg/middleware"
	"github.com/troikatech/calling-agent/pkg/mongo"
)

// buildTestRouter creates a router for testing (simplified version of unified server)
func buildTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Mock dependencies (in real tests, use test doubles)
	cfg := &env.Config{
		JWTSecret: "test-secret",
		FeatureAI: true,
	}
	// Create a mock MongoDB client for testing
	mongoClient, _ := mongo.NewClient("mongodb://localhost:27017", "test")
	redisClient := redis.NewClient(&redis.Options{Addr: "localhost:6379"})

	// Initialize AI services for testing (using no-op logger and empty configs)
	logger := zap.NewNop()
	timeout := 5 * time.Second
	aiManager := ai.NewManager([]ai.Provider{}, logger)
	ttsService := ai.NewTTSService("", "", "", "", timeout, logger)
	sttService := ai.NewSTTService("", "", "", timeout, logger)
	docLoader := ai.NewDocumentLoader("", logger)
	personaLoader := ai.NewPersonaLoader(mongoClient, docLoader, logger)

	h := handlers.NewHandler(cfg, redisClient, mongoClient, aiManager, ttsService, sttService, personaLoader)
	rateLimiter := middleware.NewRateLimiter(redisClient, 60)
	authRateLimiter := middleware.NewAuthRateLimiter(redisClient, 5, 900, 1800)

	// Register routes (matching unified server structure)
	router.GET("/health", h.HealthCheck)
	router.GET("/metrics", h.GetMetrics)
	router.GET("/metrics/prometheus", h.GetPrometheusMetrics)

	authGroup := router.Group("/auth")
	authGroup.Use(authRateLimiter.Middleware())
	{
		authGroup.POST("/login", h.Login)
		authGroup.POST("/register", h.Register)
		authGroup.POST("/refresh", h.Refresh)
		authGroup.POST("/logout", middleware.AuthMiddleware(cfg.JWTSecret), h.Logout)
	}

	api := router.Group("/api")
	api.Use(middleware.AuthMiddleware(cfg.JWTSecret))
	api.Use(middleware.IdempotencyMiddleware(redisClient))
	api.Use(rateLimiter.Middleware())
	{
		contacts := api.Group("/contacts")
		{
			contacts.POST("/import", h.ImportContacts)
			contacts.GET("/search", h.SearchContacts)
			contacts.GET("/:id", middleware.ValidateIDParam("id"), h.GetContact)
			contacts.PUT("/:id", middleware.ValidateIDParam("id"), h.UpdateContact)
			contacts.DELETE("/:id", middleware.ValidateIDParam("id"), h.DeleteContact)
		}

		suppression := api.Group("/suppression")
		{
			suppression.POST("/add", h.AddSuppression)
			suppression.GET("/check/:phone", middleware.ValidatePhoneParam("phone"), h.CheckSuppression)
		}

		calls := api.Group("/calls")
		{
			calls.GET("/:call_sid", h.GetCall)
		}

		campaigns := api.Group("/campaigns")
		{
			campaigns.POST("", middleware.RoleMiddleware("admin", "manager"), h.CreateCampaign)
			campaigns.GET("", h.ListCampaigns)
			campaigns.GET("/:id", middleware.ValidateIDParam("id"), h.GetCampaign)
			campaigns.POST("/:id/pause", middleware.ValidateIDParam("id"), middleware.RoleMiddleware("admin", "manager"), h.PauseCampaign)
			campaigns.POST("/:id/resume", middleware.ValidateIDParam("id"), middleware.RoleMiddleware("admin", "manager"), h.PauseCampaign)
			campaigns.POST("/:id/cancel", middleware.ValidateIDParam("id"), middleware.RoleMiddleware("admin", "manager"), h.CancelCampaign)
			campaigns.GET("/:id/contacts", middleware.ValidateIDParam("id"), h.GetCampaignContacts)
		}

		recordings := api.Group("/recordings")
		{
			recordings.GET("/:call_sid", h.GetRecording)
		}

		analytics := api.Group("/analytics")
		{
			analytics.GET("/overview", h.GetAnalyticsOverview)
			analytics.GET("/drilldown", h.GetAnalyticsDrilldown)
		}

		personas := api.Group("/personas")
		{
			personas.GET("", h.ListPersonas)
			personas.GET("/:id", middleware.ValidateIDParam("id"), h.GetPersona)
			personas.POST("", middleware.RoleMiddleware("admin"), h.CreatePersona)
			personas.PUT("/:id", middleware.ValidateIDParam("id"), middleware.RoleMiddleware("admin"), h.UpdatePersona)
		}

		users := api.Group("/users")
		{
			users.GET("", middleware.RoleMiddleware("admin"), h.ListUsers)
			users.GET("/:id", handlers.ValidateUUIDParam("id"), h.GetUser)
			users.PUT("/:id", handlers.ValidateUUIDParam("id"), middleware.RoleMiddleware("admin"), h.UpdateUser)
			users.DELETE("/:id", handlers.ValidateUUIDParam("id"), middleware.RoleMiddleware("admin"), h.DeleteUser)
			users.POST("/:id/activate", handlers.ValidateUUIDParam("id"), middleware.RoleMiddleware("admin"), h.ActivateUser)
			users.POST("/:id/deactivate", handlers.ValidateUUIDParam("id"), middleware.RoleMiddleware("admin"), h.DeactivateUser)
		}

		auditLogs := api.Group("/audit-logs")
		{
			auditLogs.GET("", middleware.RoleMiddleware("admin", "auditor"), h.ListAuditLogs)
		}

		if cfg.FeatureAI {
			ai := api.Group("/ai")
			{
				ai.POST("/script", h.GenerateScript)
				ai.POST("/summarize", h.SummarizeCall)
			}
		}
	}

	// Internal endpoints
	internal := router.Group("/internal")
	{
		internal.POST("/calls/initiate", func(c *gin.Context) {
			// Mock handler for unified server
		})
		internal.POST("/campaigns/schedule", func(c *gin.Context) {
			// Mock handler for unified server
		})
	}

	// Webhook endpoint
	router.POST("/webhooks/exotel", func(c *gin.Context) {
		// Mock handler for unified server
	})

	// Voicebot endpoints
	router.POST("/voicebot/init", h.ExotelVoicebotEndpoint)
	router.GET("/voicebot/ws", h.VoicebotWebSocket)

	return router
}

// Expected routes from unified server
var expectedRoutes = []struct {
	method string
	path   string
}{
	// Health & Metrics
	{"GET", "/health"},
	{"GET", "/metrics"},
	{"GET", "/metrics/prometheus"},

	// Auth
	{"POST", "/auth/login"},
	{"POST", "/auth/register"},
	{"POST", "/auth/refresh"},
	{"POST", "/auth/logout"},

	// Contacts
	{"POST", "/api/contacts/import"},
	{"GET", "/api/contacts/search"},
	{"GET", "/api/contacts/:id"},
	{"PUT", "/api/contacts/:id"},
	{"DELETE", "/api/contacts/:id"},

	// Suppression
	{"POST", "/api/suppression/add"},
	{"GET", "/api/suppression/check/:phone"},

	// Calls
	{"GET", "/api/calls/:call_sid"},

	// Recordings
	{"GET", "/api/recordings/:call_sid"},

	// Campaigns
	{"POST", "/api/campaigns"},
	{"GET", "/api/campaigns"},
	{"GET", "/api/campaigns/:id"},
	{"POST", "/api/campaigns/:id/pause"},
	{"POST", "/api/campaigns/:id/resume"},
	{"POST", "/api/campaigns/:id/cancel"},
	{"GET", "/api/campaigns/:id/contacts"},

	// Analytics
	{"GET", "/api/analytics/overview"},
	{"GET", "/api/analytics/drilldown"},

	// Personas
	{"GET", "/api/personas"},
	{"GET", "/api/personas/:id"},
	{"POST", "/api/personas"},
	{"PUT", "/api/personas/:id"},

	// Users
	{"GET", "/api/users"},
	{"GET", "/api/users/:id"},
	{"PUT", "/api/users/:id"},
	{"DELETE", "/api/users/:id"},
	{"POST", "/api/users/:id/activate"},
	{"POST", "/api/users/:id/deactivate"},

	// Audit Logs
	{"GET", "/api/audit-logs"},

	// AI (conditional)
	{"POST", "/api/ai/script"},
	{"POST", "/api/ai/summarize"},

	// Internal
	{"POST", "/internal/calls/initiate"},
	{"POST", "/internal/campaigns/schedule"},

	// Webhooks & Voicebot
	{"POST", "/webhooks/exotel"},
	{"POST", "/voicebot/init"},
	{"GET", "/voicebot/ws"},
}

func Test_Routes_Registered(t *testing.T) {
	r := buildTestRouter()
	routes := r.Routes()

	// Build map of registered routes
	registered := make(map[string]bool)
	for _, rt := range routes {
		key := rt.Method + " " + rt.Path
		registered[key] = true
	}

	// Check all expected routes are registered
	for _, expected := range expectedRoutes {
		key := expected.method + " " + expected.path
		if !registered[key] {
			t.Errorf("missing route: %s %s", expected.method, expected.path)
		}
	}
}

func Test_Routes_Count(t *testing.T) {
	r := buildTestRouter()
	routes := r.Routes()

	// Should have at least the expected number of routes
	// (may have more due to OPTIONS, etc.)
	if len(routes) < len(expectedRoutes) {
		t.Errorf("expected at least %d routes, got %d", len(expectedRoutes), len(routes))
	}
}
