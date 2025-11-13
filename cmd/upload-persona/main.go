package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/troikatech/calling-agent/pkg/env"
	"github.com/troikatech/calling-agent/pkg/logger"
	"github.com/troikatech/calling-agent/pkg/mongo"
)

// loadEnvFile manually loads .env file handling BOM
func loadEnvFile(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Remove BOM if present
		if len(line) > 0 {
			r, size := utf8.DecodeRuneInString(line)
			if r == '\ufeff' {
				line = line[size:]
			}
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			os.Setenv(key, value)
		}
	}
	return scanner.Err()
}

func main() {
	// Load environment variables
	// Try to manually load .env file first (handles BOM)
	// Also try .envc file which might have the MongoDB connection
	envFiles := []string{".env", "../.env", "../../.env", "../.envc", "../../.envc", ".envc"}
	for _, envFile := range envFiles {
		if _, err := os.Stat(envFile); err == nil {
			if err := loadEnvFile(envFile); err == nil {
				log.Printf("Loaded environment from: %s", envFile)
			}
		}
	}

	// Set default JWT_SECRET if not set (required by env.Load but not needed for this script)
	if os.Getenv("JWT_SECRET") == "" {
		os.Setenv("JWT_SECRET", "temp-secret-for-upload-script-only")
	}

	// Now load config (will use environment variables we just set)
	cfg, err := env.Load("")
	if err != nil {
		log.Fatalf("Failed to load config: %v\nPlease ensure MONGO_URI and DB_NAME are set", err)
	}

	// Initialize logger
	if err := logger.Init(cfg.LogLevel, cfg.AppEnv); err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	// Connect to MongoDB
	mongoClient, err := mongo.NewClient(cfg.MongoURI, cfg.DBName)
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := mongoClient.Disconnect(ctx); err != nil {
			log.Printf("Failed to disconnect MongoDB: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("========================================")
	fmt.Println("Uploading Clear Perceptions Persona & Knowledge Base")
	fmt.Println("========================================")
	fmt.Println()

	// Step 1: Read persona file
	// Try multiple possible paths (from backend directory or root)
	possiblePaths := []string{
		"../Clear_Perceptions_AI_Assistant_Persona.txt", // From backend/ directory
		"../../Clear_Perceptions_AI_Assistant_Persona.txt", // From backend/cmd/ directory
		"../../../Clear_Perceptions_AI_Assistant_Persona.txt", // From backend/cmd/upload-persona/ directory
		"Clear_Perceptions_AI_Assistant_Persona.txt", // Current directory
	}

	var personaFilePath string
	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			personaFilePath = path
			break
		}
	}

	if personaFilePath == "" {
		log.Fatalf("Persona file not found. Tried paths: %v", possiblePaths)
	}

	personaContent, err := os.ReadFile(personaFilePath)
	if err != nil {
		log.Fatalf("Failed to read persona file: %v", err)
	}

	fmt.Printf("✅ Read persona file: %s\n", personaFilePath)

	// Step 2: Check if persona already exists
	existingPersona, _ := mongoClient.NewQuery("personas").
		Eq("name", "Clear Perceptions AI Assistant").
		FindOne(ctx)

	var personaID interface{}
	if existingPersona != nil {
		// Try to get ID from _id (MongoDB primary key) or id field
		if id, ok := existingPersona["_id"]; ok {
			personaID = id
		} else if id, ok := existingPersona["id"]; ok {
			personaID = id
		}
		
		if personaID != nil {
			fmt.Printf("⚠️  Persona already exists with ID: %v\n", personaID)
			fmt.Println("   Updating existing persona...")

			// Update persona - use _id for MongoDB query
			personaData := map[string]interface{}{
				"name":         "Clear Perceptions AI Assistant",
				"description":  "AI Assistant for SAT, GRE, GMAT coaching & counselling",
				"tone":         "Warm, clear, structured, student-friendly. Professional, encouraging, and academically supportive.",
				"instructions": string(personaContent),
				"updated_at":   time.Now().Format(time.RFC3339),
			}

			// Try both _id and id for the update query
			query := mongoClient.NewQuery("personas")
			if _, hasID := existingPersona["_id"]; hasID {
				query = query.Eq("_id", personaID)
			} else {
				query = query.Eq("id", personaID)
			}
			
			_, err = query.UpdateOne(ctx, personaData)

			if err != nil {
				log.Fatalf("Failed to update persona: %v", err)
			}
			fmt.Println("✅ Persona updated successfully")
		} else {
			fmt.Println("⚠️  Warning: Found persona but could not get ID, creating new one...")
			existingPersona = nil // Force creation of new persona
		}
	}
	
	if existingPersona == nil {
		// Create new persona
		personaData := map[string]interface{}{
			"name":         "Clear Perceptions AI Assistant",
			"description":  "AI Assistant for SAT, GRE, GMAT coaching & counselling",
			"tone":         "Warm, clear, structured, student-friendly. Professional, encouraging, and academically supportive.",
			"instructions": string(personaContent),
			"created_at":   time.Now().Format(time.RFC3339),
			"updated_at":   time.Now().Format(time.RFC3339),
		}

		personaID, err = mongoClient.NewQuery("personas").Insert(ctx, personaData)
		if err != nil {
			log.Fatalf("Failed to create persona: %v", err)
		}
		fmt.Printf("✅ Persona created successfully with ID: %v\n", personaID)
	}

	fmt.Println()

	// Step 3: Read knowledge base file
	// Try multiple possible paths (from backend directory or root)
	kbPossiblePaths := []string{
		"../Clear_Perceptions_Knowledge_Base.txt", // From backend/ directory
		"../../Clear_Perceptions_Knowledge_Base.txt", // From backend/cmd/ directory
		"../../../Clear_Perceptions_Knowledge_Base.txt", // From backend/cmd/upload-persona/ directory
		"Clear_Perceptions_Knowledge_Base.txt", // Current directory
	}

	var kbFilePath string
	for _, path := range kbPossiblePaths {
		if _, err := os.Stat(path); err == nil {
			kbFilePath = path
			break
		}
	}

	if kbFilePath == "" {
		log.Fatalf("Knowledge base file not found. Tried paths: %v", kbPossiblePaths)
	}

	kbContent, err := os.ReadFile(kbFilePath)
	if err != nil {
		log.Fatalf("Failed to read knowledge base file: %v", err)
	}

	fmt.Printf("✅ Read knowledge base file: %s\n", kbFilePath)

	// Step 4: Save knowledge base file to uploads directory
	// Try to find the backend directory (where uploads should be)
	uploadDir := "uploads/documents"
	// Try relative paths from different execution contexts
	possibleUploadDirs := []string{
		"uploads/documents",
		"../uploads/documents",
		"../../uploads/documents",
		"../../../uploads/documents",
	}
	
	// Find the first existing uploads directory or use the first one
	for _, dir := range possibleUploadDirs {
		if _, err := os.Stat(dir); err == nil {
			uploadDir = dir
			break
		}
	}
	
	// Create directory if it doesn't exist
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		log.Fatalf("Failed to create upload directory: %v", err)
	}

	filename := fmt.Sprintf("clear_perceptions_kb_%d.txt", time.Now().Unix())
	filePath := filepath.Join(uploadDir, filename)

	if err := os.WriteFile(filePath, kbContent, 0644); err != nil {
		log.Fatalf("Failed to save knowledge base file: %v", err)
	}

	fmt.Printf("✅ Saved knowledge base file: %s\n", filePath)

	// Step 5: Check if document already exists for this persona
	existingDoc, _ := mongoClient.NewQuery("documents").
		Select("id", "name").
		Eq("persona_id", personaID).
		Eq("name", "Clear Perceptions Knowledge Base").
		FindOne(ctx)

	if existingDoc != nil {
		fmt.Printf("⚠️  Knowledge base document already exists with ID: %v\n", existingDoc["id"])
		fmt.Println("   Updating existing document...")

		// Update document
		docData := map[string]interface{}{
			"name":       "Clear Perceptions Knowledge Base",
			"type":       "knowledge_base",
			"persona_id": personaID,
			"file_path":  filePath,
			"file_url":   fmt.Sprintf("/api/documents/%s/download", filename),
			"file_size":  int64(len(kbContent)),
			"mime_type":  "text/plain",
			"updated_at": time.Now().Format(time.RFC3339),
		}

		_, err = mongoClient.NewQuery("documents").
			Eq("id", existingDoc["id"]).
			UpdateOne(ctx, docData)

		if err != nil {
			log.Fatalf("Failed to update document: %v", err)
		}
		fmt.Println("✅ Knowledge base document updated successfully")
	} else {
		// Create new document
		docData := map[string]interface{}{
			"name":       "Clear Perceptions Knowledge Base",
			"type":       "knowledge_base",
			"persona_id": personaID,
			"file_path":  filePath,
			"file_url":   fmt.Sprintf("/api/documents/%s/download", filename),
			"file_size":  int64(len(kbContent)),
			"mime_type":  "text/plain",
			"created_at": time.Now().Format(time.RFC3339),
			"updated_at": time.Now().Format(time.RFC3339),
		}

		docID, err := mongoClient.NewQuery("documents").Insert(ctx, docData)
		if err != nil {
			log.Fatalf("Failed to create document: %v", err)
		}
		fmt.Printf("✅ Knowledge base document created successfully with ID: %v\n", docID)
	}

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("✅ Upload Complete!")
	fmt.Println("========================================")
	fmt.Printf("Persona ID: %v\n", personaID)
	fmt.Println()
	fmt.Println("You can now use this persona in your campaigns by setting persona_id to the ID above.")
}

