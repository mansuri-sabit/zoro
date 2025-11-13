package env

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv         string
	AppPort        string
	TZ             string
	JWTSecret      string
	JWTIssuer      string
	JWTAudience    string
	AccessTTLMin   int
	RefreshTTLDays int

	RedisURL string

	MongoURI string
	DBName   string

	AIBaseURL   string
	AITimeoutMs int
	FeatureAI   bool

	// AI Provider API Keys
	OpenAIApiKey    string
	OpenAIModel     string
	OpenAIMaxTokens int

	GeminiApiKey string
	GeminiModel  string

	AnthropicApiKey    string
	AnthropicModel     string
	AnthropicMaxTokens int

	// TTS Service (ElevenLabs)
	ElevenLabsApiKey       string
	ElevenLabsVoiceID      string
	ElevenLabsModel        string
	ElevenLabsOutputFormat string

	// STT Service (OpenAI Whisper)
	WhisperModel    string
	WhisperLanguage string

	ExotelSubdomain     string
	ExotelAccountSID    string
	ExotelAPIKey        string
	ExotelAPIToken      string
	ExotelExophone      string
	ExotelFlowURL       string
	ExotelVoicebotAppletID string
	ExotelWebhookSecret string
	ExotelVoicebotToken string // Bearer token for WebSocket authentication (optional)
	VoicebotBaseURL     string // Public WSS URL for Exotel (e.g., https://api.example.com)

	DialBusinessStartHour int
	DialBusinessEndHour   int
	DialMaxConcurrency    int
	APIRateLimitRPM       int

	RetryNoAnswerMax    int
	RetryNoAnswerGapMin int

	StorageDriver    string
	LocalStoragePath string

	LogLevel           string
	CORSAllowedOrigins string
	AllowSelfRegister  bool

	OTELEndpoint string
	OTELEnabled  bool
}

func Load(envFile string) (*Config, error) {
	if envFile != "" {
		// Try to load .env file, but don't fail if it doesn't exist
		// This allows the app to work with environment variables only (e.g., in production)
		if err := godotenv.Load(envFile); err != nil {
			// If file doesn't exist, that's okay - we'll use environment variables directly
			// Only fail if it's a different error (permission, etc.)
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to load .env file: %w", err)
			}
			// File doesn't exist - continue without it, use environment variables
		}
	}

	cfg := &Config{
		AppEnv:         getEnv("APP_ENV", "development"),
		AppPort:        getEnv("APP_PORT", "8080"),
		TZ:             getEnv("TZ", "Asia/Kolkata"),
		JWTSecret:      mustGetEnv("JWT_SECRET"),
		JWTIssuer:      getEnv("JWT_ISSUER", "troika-calling-platform"),
		JWTAudience:    getEnv("JWT_AUDIENCE", "troika-api"),
		AccessTTLMin:   getEnvInt("ACCESS_TTL_MIN", 15),
		RefreshTTLDays: getEnvInt("REFRESH_TTL_DAYS", 14),

		RedisURL: getEnv("REDIS_URL", "redis://localhost:6379/0"),

		MongoURI: getEnv("MONGO_URI", "mongodb://localhost:27017"),
		DBName:   getEnv("DB_NAME", "troika"),

		AIBaseURL:   getEnv("AI_BASE_URL", "http://localhost:8000"),
		AITimeoutMs: getEnvInt("AI_TIMEOUT_MS", 3500),
		FeatureAI:   getEnvBool("FEATURE_AI", true),

		// AI Provider API Keys
		OpenAIApiKey:    getEnv("OPENAI_API_KEY", ""),
		OpenAIModel:     getEnv("OPENAI_MODEL", "gpt-4o-mini"),
		OpenAIMaxTokens: getEnvInt("OPENAI_MAX_TOKENS", 2000),

		GeminiApiKey: getEnv("GEMINI_API_KEY", ""),
		GeminiModel:  getEnv("GEMINI_MODEL", "gemini-1.5-flash"),

		AnthropicApiKey:    getEnv("ANTHROPIC_API_KEY", ""),
		AnthropicModel:     getEnv("ANTHROPIC_MODEL", "claude-3-5-haiku-20241022"),
		AnthropicMaxTokens: getEnvInt("ANTHROPIC_MAX_TOKENS", 2000),

		// TTS Service (ElevenLabs)
		ElevenLabsApiKey:       getEnv("ELEVENLABS_API_KEY", ""),
		ElevenLabsVoiceID:      getEnv("ELEVENLABS_VOICE_ID", "21m00Tcm4TlvDq8ikWAM"),
		ElevenLabsModel:        getEnv("ELEVENLABS_MODEL", "eleven_multilingual_v2"),
		ElevenLabsOutputFormat: getEnv("ELEVENLABS_OUTPUT_FORMAT", "mp3_44100_128"),

		// STT Service (OpenAI Whisper)
		WhisperModel:    getEnv("WHISPER_MODEL", "whisper-1"),
		WhisperLanguage: getEnv("WHISPER_LANGUAGE", ""),

		ExotelSubdomain:     getEnv("EXOTEL_SUBDOMAIN", "api"),
		ExotelAccountSID:    getEnv("EXOTEL_ACCOUNT_SID", ""),
		ExotelAPIKey:        getEnv("EXOTEL_API_KEY", ""),
		ExotelAPIToken:      getEnv("EXOTEL_API_TOKEN", ""),
		ExotelExophone:      getEnv("EXOTEL_EXOPHONE", ""),
		ExotelFlowURL:       getEnv("EXOTEL_FLOW_URL", ""),
		ExotelVoicebotAppletID: getEnv("EXOTEL_VOICEBOT_APPLET_ID", ""),
		ExotelWebhookSecret: getEnv("EXOTEL_WEBHOOK_SIGNATURE_SECRET", ""),
		ExotelVoicebotToken: getEnv("EXOTEL_VOICEBOT_TOKEN", ""), // Bearer token for WebSocket auth (set in Exotel dashboard)
		VoicebotBaseURL:     getEnv("VOICEBOT_BASE_URL", ""), // Public HTTPS URL for WSS (e.g., https://api.example.com)

		DialBusinessStartHour: getEnvInt("DIAL_BUSINESS_START_HOUR", 9),
		DialBusinessEndHour:   getEnvInt("DIAL_BUSINESS_END_HOUR", 21),
		DialMaxConcurrency:    getEnvInt("DIAL_MAX_CONCURRENCY", 60),
		APIRateLimitRPM:       getEnvInt("API_RATE_LIMIT_RPM", 180),

		RetryNoAnswerMax:    getEnvInt("RETRY_NOANSWER_MAX", 2),
		RetryNoAnswerGapMin: getEnvInt("RETRY_NOANSWER_GAP_MIN", 20),

		StorageDriver:    getEnv("STORAGE_DRIVER", "exotel-proxy"),
		LocalStoragePath: getEnv("LOCAL_STORAGE_PATH", "/data/audio"),

		LogLevel:           getEnv("LOG_LEVEL", "info"),
		CORSAllowedOrigins: getEnv("CORS_ALLOWED_ORIGINS", "*"),
		AllowSelfRegister:  getEnvBool("ALLOW_SELF_REGISTER", false),

		OTELEndpoint: getEnv("OTEL_ENDPOINT", ""),
		OTELEnabled:  getEnvBool("OTEL_ENABLED", false),
	}

	loc, err := time.LoadLocation(cfg.TZ)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %s: %w", cfg.TZ, err)
	}
	time.Local = loc

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func mustGetEnv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		panic(fmt.Sprintf("required environment variable %s is not set", key))
	}
	return value
}

func getEnvInt(key string, defaultValue int) int {
	strValue := os.Getenv(key)
	if strValue == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(strValue)
	if err != nil {
		return defaultValue
	}
	return value
}

func getEnvBool(key string, defaultValue bool) bool {
	strValue := os.Getenv(key)
	if strValue == "" {
		return defaultValue
	}
	value, err := strconv.ParseBool(strValue)
	if err != nil {
		return defaultValue
	}
	return value
}
