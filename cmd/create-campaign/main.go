package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"bufio"

	"github.com/troikatech/calling-agent/pkg/env"
	"github.com/troikatech/calling-agent/pkg/logger"
	"github.com/troikatech/calling-agent/pkg/mongo"
)

func main() {
	// Load environment variables
	envFiles := []string{".env", "../.env", "../../.env", "../.envc", "../../.envc", ".envc"}
	for _, envFile := range envFiles {
		if _, err := os.Stat(envFile); err == nil {
			if err := loadEnvFile(envFile); err == nil {
				log.Printf("Loaded environment from: %s", envFile)
			}
		}
	}

	if os.Getenv("JWT_SECRET") == "" {
		os.Setenv("JWT_SECRET", "temp-secret-for-script-only")
	}

	cfg, err := env.Load("")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if err := logger.Init(cfg.LogLevel, cfg.AppEnv); err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

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
	fmt.Println("Creating Clear Perceptions Campaign")
	fmt.Println("========================================")
	fmt.Println()

	// Persona ID (MongoDB ObjectID string)
	personaID := "6915c2b68a8c27c5cc171dbd"

	// Verify persona exists - try multiple ways to find it
	var persona map[string]interface{}
	
	// Try finding by name first (more reliable)
	persona, _ = mongoClient.NewQuery("personas").
		Eq("name", "Clear Perceptions AI Assistant").
		FindOne(ctx)

	if persona == nil {
		// Try with _id field
		persona, _ = mongoClient.NewQuery("personas").
			Eq("_id", personaID).
			FindOne(ctx)
	}

	if persona == nil {
		// Try with id field
		persona, _ = mongoClient.NewQuery("personas").
			Eq("id", personaID).
			FindOne(ctx)
	}

	if persona == nil {
		log.Fatalf("Persona with ID %s not found. Please run 'make upload-persona' first.", personaID)
	}

	fmt.Printf("✅ Found persona: %v\n", persona["name"])
	fmt.Println()

	// Campaign data
	campaignData := map[string]interface{}{
		"name":          "Clear Perceptions AI Assistant Campaign",
		"status":        "draft",
		"window_start":  9,
		"window_end":    21,
		"max_retries":   2,
		"retry_gap_min": 20,
		"flow_id":       "FLOW_AI_WP_DEMO", // You can change this to your actual flow ID
		"persona_id":    personaID,         // Set persona_id as string (ObjectID)
		"created_at":    time.Now().Format(time.RFC3339),
		"updated_at":    time.Now().Format(time.RFC3339),
	}

	// Check if campaign with same name already exists
	existingCampaign, _ := mongoClient.NewQuery("campaigns").
		Eq("name", campaignData["name"]).
		FindOne(ctx)

	if existingCampaign != nil {
		fmt.Printf("⚠️  Campaign '%s' already exists\n", campaignData["name"])
		
		// Get campaign ID
		var campaignID interface{}
		if id, ok := existingCampaign["_id"]; ok {
			campaignID = id
		} else if id, ok := existingCampaign["id"]; ok {
			campaignID = id
		}

		if campaignID != nil {
			// Update persona_id
			updateData := map[string]interface{}{
				"persona_id": personaID,
				"updated_at": time.Now().Format(time.RFC3339),
			}

			query := mongoClient.NewQuery("campaigns")
			if _, hasID := existingCampaign["_id"]; hasID {
				query = query.Eq("_id", campaignID)
			} else {
				query = query.Eq("id", campaignID)
			}

			_, err = query.UpdateOne(ctx, updateData)
			if err != nil {
				log.Fatalf("Failed to update campaign: %v", err)
			}

			fmt.Printf("✅ Updated existing campaign with persona_id: %s\n", personaID)
			fmt.Printf("Campaign ID: %v\n", campaignID)
			return
		}
	}

	// Create new campaign
	campaignID, err := mongoClient.NewQuery("campaigns").Insert(ctx, campaignData)
	if err != nil {
		log.Fatalf("Failed to create campaign: %v", err)
	}

	fmt.Printf("✅ Campaign created successfully!\n")
	fmt.Printf("Campaign ID: %v\n", campaignID)
	fmt.Printf("Campaign Name: %s\n", campaignData["name"])
	fmt.Printf("Persona ID: %s\n", personaID)
	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("✅ Campaign Ready!")
	fmt.Println("========================================")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("1. Add contacts to this campaign")
	fmt.Println("2. Update flow_id if needed")
	fmt.Println("3. Start the campaign when ready")
	fmt.Println()
	fmt.Println("To add contacts, use:")
	fmt.Println("  POST /api/campaigns/{campaign_id}/contacts")
	fmt.Println("  OR update the campaign_contacts collection directly")
}

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

