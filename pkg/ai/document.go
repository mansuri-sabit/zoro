package ai

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

// DocumentLoader handles document text extraction
type DocumentLoader struct {
	basePath string
	logger   *zap.Logger
}

// NewDocumentLoader creates a new document loader
func NewDocumentLoader(basePath string, logger *zap.Logger) *DocumentLoader {
	if basePath == "" {
		basePath = "uploads/documents"
	}

	return &DocumentLoader{
		basePath: basePath,
		logger:   logger,
	}
}

// ExtractText extracts text from a document file
func (d *DocumentLoader) ExtractText(filePath string) (string, error) {
	// Handle relative paths
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(d.basePath, filePath)
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		d.logger.Warn("Document file not found", zap.String("path", filePath))
		return "", fmt.Errorf("document file not found: %s", filePath)
	}

	// Get file extension
	ext := strings.ToLower(filepath.Ext(filePath))

	// Extract text based on file type
	switch ext {
	case ".txt", ".md":
		return d.extractTextFile(filePath)
	case ".pdf":
		return d.extractPDF(filePath)
	case ".docx", ".doc":
		return d.extractDOCX(filePath)
	default:
		d.logger.Warn("Unsupported file format", zap.String("ext", ext))
		return "", fmt.Errorf("unsupported file format: %s", ext)
	}
}

// extractTextFile extracts text from a plain text file
func (d *DocumentLoader) extractTextFile(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	return string(data), nil
}

// extractPDF extracts text from a PDF file
// Note: This is a simplified version. For production, use a PDF library like:
// - github.com/ledongthuc/pdf
// - github.com/gen2brain/go-fitz
// - github.com/unidoc/unipdf
func (d *DocumentLoader) extractPDF(filePath string) (string, error) {
	// TODO: Implement PDF extraction using a PDF library
	// For now, return an error indicating PDF support is not yet implemented
	d.logger.Warn("PDF extraction not yet implemented", zap.String("path", filePath))
	return "", fmt.Errorf("PDF extraction not yet implemented. Please use a PDF library like github.com/ledongthuc/pdf")
}

// extractDOCX extracts text from a DOCX file
// Note: This is a simplified version. For production, use a DOCX library like:
// - github.com/unidoc/unioffice
func (d *DocumentLoader) extractDOCX(filePath string) (string, error) {
	// TODO: Implement DOCX extraction using a DOCX library
	// For now, return an error indicating DOCX support is not yet implemented
	d.logger.Warn("DOCX extraction not yet implemented", zap.String("path", filePath))
	return "", fmt.Errorf("DOCX extraction not yet implemented. Please use a DOCX library like github.com/unidoc/unioffice")
}

// ExtractFromDocuments extracts text from multiple documents
func (d *DocumentLoader) ExtractFromDocuments(filePaths []string) (string, error) {
	var texts []string

	for _, filePath := range filePaths {
		text, err := d.ExtractText(filePath)
		if err != nil {
			d.logger.Warn("Failed to extract text from document",
				zap.String("path", filePath),
				zap.Error(err),
			)
			continue
		}

		if text != "" {
			texts = append(texts, text)
		}
	}

	if len(texts) == 0 {
		return "", fmt.Errorf("no text extracted from any document")
	}

	return strings.Join(texts, "\n\n---\n\n"), nil
}

