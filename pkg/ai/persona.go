package ai

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/troikatech/calling-agent/pkg/mongo"
)

// PersonaLoader loads persona and document data from MongoDB
type PersonaLoader struct {
	mongoClient *mongo.Client
	docLoader   *DocumentLoader
	logger      *zap.Logger
}

// NewPersonaLoader creates a new persona loader
func NewPersonaLoader(mongoClient *mongo.Client, docLoader *DocumentLoader, logger *zap.Logger) *PersonaLoader {
	return &PersonaLoader{
		mongoClient: mongoClient,
		docLoader:   docLoader,
		logger:      logger,
	}
}

// LoadPersonaData loads persona data from MongoDB
func (l *PersonaLoader) LoadPersonaData(ctx context.Context, personaID int64) (map[string]interface{}, error) {
	if l.mongoClient == nil {
		l.logger.Warn("MongoDB not connected, cannot load persona")
		return nil, fmt.Errorf("MongoDB client not available")
	}

	// Try to find persona by ID (could be int64 or string)
	persona, err := l.mongoClient.NewQuery("personas").
		Eq("id", personaID).
		FindOne(ctx)
	
	if err != nil {
		return nil, fmt.Errorf("failed to query persona: %w", err)
	}

	if persona == nil {
		// Try as string
		persona, err = l.mongoClient.NewQuery("personas").
			Eq("id", fmt.Sprintf("%d", personaID)).
			FindOne(ctx)
		
		if err != nil {
			return nil, fmt.Errorf("failed to query persona: %w", err)
		}
	}

	if persona != nil {
		l.logger.Info("Loaded persona",
			zap.Int64("persona_id", personaID),
			zap.Any("persona", persona),
		)
		return persona, nil
	}

	l.logger.Warn("Persona not found",
		zap.Int64("persona_id", personaID),
	)
	return nil, fmt.Errorf("persona %d not found", personaID)
}

// LoadDocumentsForPersona loads documents linked to persona from MongoDB
func (l *PersonaLoader) LoadDocumentsForPersona(ctx context.Context, personaID int64) ([]map[string]interface{}, error) {
	if l.mongoClient == nil {
		l.logger.Warn("MongoDB not connected, cannot load documents")
		return nil, fmt.Errorf("MongoDB client not available")
	}

	// Find documents linked to this persona
	documents, err := l.mongoClient.NewQuery("documents").
		Eq("persona_id", personaID).
		Find(ctx)
	
	if err != nil {
		return nil, fmt.Errorf("failed to query documents: %w", err)
	}

	if len(documents) == 0 {
		// Try as string
		documents, err = l.mongoClient.NewQuery("documents").
			Eq("persona_id", fmt.Sprintf("%d", personaID)).
			Find(ctx)
		
		if err != nil {
			return nil, fmt.Errorf("failed to query documents: %w", err)
		}
	}

	l.logger.Info("Loaded documents for persona",
		zap.Int64("persona_id", personaID),
		zap.Int("count", len(documents)),
	)
	return documents, nil
}

// ExtractDocumentTexts extracts text content from document files
func (l *PersonaLoader) ExtractDocumentTexts(ctx context.Context, documents []map[string]interface{}) (string, error) {
	if l.docLoader == nil {
		l.logger.Warn("Document loader not available, cannot extract text")
		return "", fmt.Errorf("document loader not available")
	}

	documentPaths := []string{}

	for _, doc := range documents {
		// Try different field names for file path
		filePath := ""
		if fp, ok := doc["file_path"].(string); ok && fp != "" {
			filePath = fp
		} else if fp, ok := doc["filepath"].(string); ok && fp != "" {
			filePath = fp
		} else if fp, ok := doc["path"].(string); ok && fp != "" {
			filePath = fp
		}

		if filePath != "" {
			documentPaths = append(documentPaths, filePath)
		}
	}

	if len(documentPaths) == 0 {
		l.logger.Warn("No document file paths found")
		return "", nil
	}

	combinedText, err := l.docLoader.ExtractFromDocuments(documentPaths)
	if err != nil {
		l.logger.Warn("Failed to extract text from documents",
			zap.Error(err),
		)
		return "", err
	}

	l.logger.Info("Extracted text from documents",
		zap.Int("document_count", len(documentPaths)),
		zap.Int("text_length", len(combinedText)),
	)
	return combinedText, nil
}

// BuildRAGContext builds RAG context from persona and documents
func (l *PersonaLoader) BuildRAGContext(ctx context.Context, personaID *int64) (map[string]interface{}, error) {
	context := map[string]interface{}{
		"persona_data": nil,
		"document_text": "",
		"has_context": false,
	}

	if personaID == nil {
		return context, nil
	}

	// Load persona data
	personaData, err := l.LoadPersonaData(ctx, *personaID)
	if err != nil {
		l.logger.Warn("Failed to load persona data",
			zap.Int64("persona_id", *personaID),
			zap.Error(err),
		)
	} else if personaData != nil {
		context["persona_data"] = personaData
		context["has_context"] = true
	}

	// Load and extract documents
	documents, err := l.LoadDocumentsForPersona(ctx, *personaID)
	if err != nil {
		l.logger.Warn("Failed to load documents",
			zap.Int64("persona_id", *personaID),
			zap.Error(err),
		)
	} else if len(documents) > 0 {
		documentText, err := l.ExtractDocumentTexts(ctx, documents)
		if err != nil {
			l.logger.Warn("Failed to extract document text",
				zap.Error(err),
			)
		} else if documentText != "" {
			context["document_text"] = documentText
			context["has_context"] = true
		}
	}

	return context, nil
}

