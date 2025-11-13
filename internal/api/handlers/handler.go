package handlers

import (
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/troikatech/calling-agent/pkg/ai"
	"github.com/troikatech/calling-agent/pkg/env"
	"github.com/troikatech/calling-agent/pkg/logger"
	"github.com/troikatech/calling-agent/pkg/mongo"
)

type Handler struct {
	cfg           *env.Config
	redisClient   *redis.Client
	mongoClient   *mongo.Client
	logger        *zap.Logger
	aiManager     *ai.Manager
	ttsService    *ai.TTSService
	sttService    *ai.STTService
	personaLoader *ai.PersonaLoader
}

func NewHandler(
	cfg *env.Config,
	redisClient *redis.Client,
	mongoClient *mongo.Client,
	aiManager *ai.Manager,
	ttsService *ai.TTSService,
	sttService *ai.STTService,
	personaLoader *ai.PersonaLoader,
) *Handler {
	return &Handler{
		cfg:           cfg,
		redisClient:   redisClient,
		mongoClient:   mongoClient,
		logger:        logger.Log,
		aiManager:     aiManager,
		ttsService:    ttsService,
		sttService:    sttService,
		personaLoader: personaLoader,
	}
}
