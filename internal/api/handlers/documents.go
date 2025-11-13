package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/troikatech/calling-agent/pkg/audit"
	"github.com/troikatech/calling-agent/pkg/errors"
	"github.com/troikatech/calling-agent/pkg/utils"
)

type UploadDocumentRequest struct {
	PersonaID int64  `form:"persona_id"`
	Name      string `form:"name" binding:"required"`
	Type      string `form:"type"` // "script", "knowledge_base", "training"
}

type DocumentResponse struct {
	ID        interface{} `json:"id"`
	Name      string      `json:"name"`
	Type      string      `json:"type"`
	PersonaID interface{} `json:"persona_id,omitempty"`
	FileURL   string      `json:"file_url"`
	FileSize  int64       `json:"file_size"`
	MimeType  string      `json:"mime_type"`
	CreatedAt string      `json:"created_at"`
	UpdatedAt string      `json:"updated_at"`
}

// UploadDocument handles file upload for documents
func (h *Handler) UploadDocument(c *gin.Context) {
	userID, _ := c.Get("user_id")
	userIDStr := fmt.Sprintf("%v", userID)

	var req UploadDocumentRequest
	if err := c.ShouldBind(&req); err != nil {
		errors.BadRequest(c, err.Error())
		return
	}

	// Get uploaded file
	file, err := c.FormFile("file")
	if err != nil {
		errors.BadRequest(c, "file is required")
		return
	}

	// Validate file type
	allowedTypes := []string{".pdf", ".docx", ".doc", ".txt", ".md"}
	ext := strings.ToLower(filepath.Ext(file.Filename))
	allowed := false
	for _, t := range allowedTypes {
		if ext == t {
			allowed = true
			break
		}
	}
	if !allowed {
		errors.BadRequest(c, fmt.Sprintf("file type not allowed. allowed: %v", allowedTypes))
		return
	}

	// Validate file size (max 10MB)
	if file.Size > 10*1024*1024 {
		errors.BadRequest(c, "file size exceeds 10MB limit")
		return
	}

	// Create uploads directory if not exists
	uploadDir := "uploads/documents"
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		h.logger.Error("Failed to create upload directory", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	// Generate unique filename
	filename := fmt.Sprintf("%d_%s", time.Now().Unix(), file.Filename)
	filePath := filepath.Join(uploadDir, filename)

	// Save file
	if err := c.SaveUploadedFile(file, filePath); err != nil {
		h.logger.Error("Failed to save file", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	// Determine MIME type
	mimeType := file.Header.Get("Content-Type")
	if mimeType == "" {
		switch ext {
		case ".pdf":
			mimeType = "application/pdf"
		case ".docx":
			mimeType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		case ".doc":
			mimeType = "application/msword"
		case ".txt", ".md":
			mimeType = "text/plain"
		default:
			mimeType = "application/octet-stream"
		}
	}

	// Save document metadata to database
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	docData := map[string]interface{}{
		"name":        req.Name,
		"type":        req.Type,
		"persona_id":  req.PersonaID,
		"file_path":   filePath,
		"file_url":    fmt.Sprintf("/api/documents/%s/download", filename),
		"file_size":   file.Size,
		"mime_type":   mimeType,
		"uploaded_by": userIDStr,
		"created_at":  time.Now().Format(time.RFC3339),
		"updated_at":  time.Now().Format(time.RFC3339),
	}

	docID, err := h.mongoClient.NewQuery("documents").Insert(ctx, docData)
	if err != nil {
		// Clean up uploaded file on error
		os.Remove(filePath)
		h.logger.Error("Failed to save document metadata", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	docData["id"] = docID

	// Audit log
	audit.Log(h.mongoClient, userIDStr, string(audit.ActionCreate), "document", fmt.Sprintf("%v", docID), map[string]interface{}{
		"name": req.Name,
		"type": req.Type,
	})

	c.JSON(http.StatusCreated, docData)
}

// ListDocuments lists all documents with optional filters
func (h *Handler) ListDocuments(c *gin.Context) {
	pagination := utils.ParsePagination(c)
	personaID := c.Query("persona_id")
	docType := c.Query("type")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	query := h.mongoClient.NewQuery("documents").
		Select("id", "name", "type", "persona_id", "file_url", "file_size", "mime_type", "created_at", "updated_at").
		Limit(int64(pagination.Limit))

	if personaID != "" {
		query = query.Eq("persona_id", personaID)
	}
	if docType != "" {
		query = query.Eq("type", docType)
	}

	documents, err := query.Find(ctx)
	if err != nil {
		h.logger.Error("Failed to fetch documents", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	c.JSON(http.StatusOK, utils.PaginatedResponse{
		Data:  documents,
		Page:  pagination.Page,
		Limit: pagination.Limit,
		Count: len(documents),
	})
}

// GetDocument gets document by ID
func (h *Handler) GetDocument(c *gin.Context) {
	id, _ := c.Get("id_int")
	idStr := fmt.Sprintf("%d", id.(int64))

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	document, err := h.mongoClient.NewQuery("documents").
		Select("*").
		Eq("id", idStr).
		FindOne(ctx)

	if err != nil || document == nil {
		errors.NotFound(c, "document not found")
		return
	}

	c.JSON(http.StatusOK, document)
}

// DownloadDocument downloads the document file
func (h *Handler) DownloadDocument(c *gin.Context) {
	filename := c.Param("filename")
	if filename == "" {
		errors.BadRequest(c, "filename is required")
		return
	}

	// Security: prevent path traversal
	if strings.Contains(filename, "..") || strings.Contains(filename, "/") {
		errors.BadRequest(c, "invalid filename")
		return
	}

	filePath := filepath.Join("uploads/documents", filename)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		errors.NotFound(c, "file not found")
		return
	}

	// Get document metadata from database
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	documents, err := h.mongoClient.NewQuery("documents").
		Select("mime_type", "name").
		Eq("file_path", filePath).
		Find(ctx)

	if err != nil || len(documents) == 0 {
		errors.NotFound(c, "document not found")
		return
	}

	doc := documents[0]
	mimeType := "application/octet-stream"
	if mt, ok := doc["mime_type"].(string); ok {
		mimeType = mt
	}

	fileName := filename
	if name, ok := doc["name"].(string); ok && name != "" {
		fileName = name + filepath.Ext(filename)
	}

	// Open and serve file
	file, err := os.Open(filePath)
	if err != nil {
		h.logger.Error("Failed to open file", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}
	defer file.Close()

	c.Header("Content-Type", mimeType)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", fileName))
	c.Header("Content-Transfer-Encoding", "binary")

	io.Copy(c.Writer, file)
}

// DeleteDocument deletes a document
func (h *Handler) DeleteDocument(c *gin.Context) {
	id, _ := c.Get("id_int")
	idStr := fmt.Sprintf("%d", id.(int64))
	userID, _ := c.Get("user_id")
	userIDStr := fmt.Sprintf("%v", userID)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	// Get document to find file path
	document, err := h.mongoClient.NewQuery("documents").
		Select("file_path", "name").
		Eq("id", idStr).
		FindOne(ctx)

	if err != nil || document == nil {
		errors.NotFound(c, "document not found")
		return
	}

	// Delete file
	if filePath, ok := document["file_path"].(string); ok && filePath != "" {
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			h.logger.Warn("Failed to delete file", zap.String("path", filePath), zap.Error(err))
		}
	}

	// Delete from database
	_, err = h.mongoClient.NewQuery("documents").
		Eq("id", idStr).
		DeleteOne(ctx)

	if err != nil {
		h.logger.Error("Failed to delete document", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	// Audit log
	audit.Log(h.mongoClient, userIDStr, string(audit.ActionDelete), "document", idStr, map[string]interface{}{
		"name": document["name"],
	})

	c.JSON(http.StatusOK, gin.H{"message": "document deleted"})
}

// LinkDocumentToPersona links a document to a persona
func (h *Handler) LinkDocumentToPersona(c *gin.Context) {
	personaID, _ := c.Get("id_int")
	personaIDInt := personaID.(int64)

	var req struct {
		DocumentID int64 `json:"document_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		errors.BadRequest(c, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	// Update document with persona_id
	_, err := h.mongoClient.NewQuery("documents").
		Eq("id", fmt.Sprintf("%d", req.DocumentID)).
		UpdateOne(ctx, map[string]interface{}{
			"persona_id": personaIDInt,
			"updated_at": time.Now().Format(time.RFC3339),
		})

	if err != nil {
		h.logger.Error("Failed to link document to persona", zap.Error(err))
		errors.InternalError(c, err, h.logger)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "document linked to persona"})
}
