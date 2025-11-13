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
	fmt.Println("Updating Campaign Flow ID")
	fmt.Println("========================================")
	fmt.Println()

	// Flow ID (APP_ID from Exotel)
	flowID := "1116870"

	// Find the Clear Perceptions campaign
	campaign, _ := mongoClient.NewQuery("campaigns").
		Eq("name", "Clear Perceptions AI Assistant Campaign").
		FindOne(ctx)

	if campaign == nil {
		log.Fatalf("Campaign not found. Please run 'make create-campaign' first.")
	}

	// Get campaign ID
	var campaignID interface{}
	if id, ok := campaign["_id"]; ok {
		campaignID = id
	} else if id, ok := campaign["id"]; ok {
		campaignID = id
	}

	if campaignID == nil {
		log.Fatalf("Could not get campaign ID")
	}

	fmt.Printf("✅ Found campaign: %v\n", campaign["name"])
	fmt.Printf("Campaign ID: %v\n", campaignID)
	fmt.Printf("Current flow_id: %v\n", campaign["flow_id"])
	fmt.Printf("New flow_id: %s\n", flowID)
	fmt.Println()

	// Update campaign with flow_id
	updateData := map[string]interface{}{
		"flow_id":    flowID,
		"updated_at": time.Now().Format(time.RFC3339),
	}

	query := mongoClient.NewQuery("campaigns")
	if _, hasID := campaign["_id"]; hasID {
		query = query.Eq("_id", campaignID)
	} else {
		query = query.Eq("id", campaignID)
	}

	_, err = query.UpdateOne(ctx, updateData)
	if err != nil {
		log.Fatalf("Failed to update campaign: %v", err)
	}

	fmt.Printf("✅ Campaign flow_id updated successfully!\n")
	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("✅ Complete!")
	fmt.Println("========================================")
	fmt.Println()
	fmt.Printf("Campaign: %v\n", campaign["name"])
	fmt.Printf("Flow ID (APP_ID): %s\n", flowID)
	fmt.Println()
	fmt.Println("Campaign is now configured with Exotel Applet ID: 1116870")
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

