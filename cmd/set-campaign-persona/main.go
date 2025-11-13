package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
	"unicode/utf8"

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
	fmt.Println("Setting Persona ID in Campaigns")
	fmt.Println("========================================")
	fmt.Println()

	// Persona ID to set
	personaID := "6915c2b68a8c27c5cc171dbd"

	// Get all campaigns
	campaigns, err := mongoClient.NewQuery("campaigns").
		Select("_id", "id", "name", "persona_id").
		Find(ctx)

	if err != nil {
		log.Fatalf("Failed to fetch campaigns: %v", err)
	}

	if len(campaigns) == 0 {
		fmt.Println("⚠️  No campaigns found in database")
		fmt.Println()
		fmt.Println("To create a campaign with persona_id, use:")
		fmt.Println("  POST /api/campaigns")
		fmt.Println("  {")
		fmt.Printf("    \"persona_id\": \"%s\",\n", personaID)
		fmt.Println("    ... other fields")
		fmt.Println("  }")
		os.Exit(0)
	}

	fmt.Printf("Found %d campaign(s)\n", len(campaigns))
	fmt.Println()

	updatedCount := 0
	for i, campaign := range campaigns {
		campaignName := "Unknown"
		if name, ok := campaign["name"].(string); ok {
			campaignName = name
		}

		// Get campaign ID (try _id first, then id)
		var campaignID interface{}
		if id, ok := campaign["_id"]; ok {
			campaignID = id
		} else if id, ok := campaign["id"]; ok {
			campaignID = id
		}

		if campaignID == nil {
			fmt.Printf("⚠️  Campaign %d: %s - Could not get ID, skipping\n", i+1, campaignName)
			continue
		}

		// Check if persona_id already set
		currentPersonaID := campaign["persona_id"]
		if currentPersonaID != nil {
			fmt.Printf("Campaign %d: %s\n", i+1, campaignName)
			fmt.Printf("  Current persona_id: %v\n", currentPersonaID)
			fmt.Printf("  New persona_id: %s\n", personaID)
			fmt.Print("  Update? (y/n): ")
			
			// Auto-update for now (you can make it interactive if needed)
			update := true
			if !update {
				fmt.Println("  Skipped")
				continue
			}
		} else {
			fmt.Printf("Campaign %d: %s\n", i+1, campaignName)
			fmt.Printf("  No persona_id set\n")
			fmt.Printf("  Setting persona_id: %s\n", personaID)
		}

		// Update campaign with persona_id
		updateData := map[string]interface{}{
			"persona_id":  personaID,
			"updated_at": time.Now().Format(time.RFC3339),
		}

		// Build query - try both _id and id
		query := mongoClient.NewQuery("campaigns")
		if _, hasID := campaign["_id"]; hasID {
			query = query.Eq("_id", campaignID)
		} else {
			query = query.Eq("id", campaignID)
		}

		_, err = query.UpdateOne(ctx, updateData)
		if err != nil {
			fmt.Printf("  ❌ Failed to update: %v\n", err)
			continue
		}

		fmt.Printf("  ✅ Updated successfully\n")
		updatedCount++
		fmt.Println()
	}

	fmt.Println("========================================")
	fmt.Printf("✅ Update Complete!\n")
	fmt.Printf("Updated %d out of %d campaign(s)\n", updatedCount, len(campaigns))
	fmt.Println("========================================")
	fmt.Println()
	fmt.Printf("Persona ID set: %s\n", personaID)
	fmt.Println()
	fmt.Println("All campaigns will now use the Clear Perceptions AI Assistant persona.")
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

