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
	fmt.Println("Adding Contact to Campaign")
	fmt.Println("========================================")
	fmt.Println()

	// Contact details
	phoneNumber := "+919324606985"
	contactName := "Test Contact"

	// Find or create contact
	existingContact, _ := mongoClient.NewQuery("contacts").
		Eq("msisdn_e164", phoneNumber).
		FindOne(ctx)

	var contactID interface{}
	if existingContact != nil {
		if id, ok := existingContact["_id"]; ok {
			contactID = id
		} else if id, ok := existingContact["id"]; ok {
			contactID = id
		}
		fmt.Printf("✅ Found existing contact: %s\n", phoneNumber)
	} else {
		// Create new contact
		contactData := map[string]interface{}{
			"msisdn_e164": phoneNumber,
			"name":        contactName,
			"tags":        []string{"clear-perceptions"},
			"created_at":  time.Now().Format(time.RFC3339),
			"updated_at":  time.Now().Format(time.RFC3339),
		}

		contactID, err = mongoClient.NewQuery("contacts").Insert(ctx, contactData)
		if err != nil {
			log.Fatalf("Failed to create contact: %v", err)
		}
		fmt.Printf("✅ Created new contact: %s\n", phoneNumber)
	}

	fmt.Printf("Contact ID: %v\n", contactID)
	fmt.Println()

	// Find the Clear Perceptions campaign
	campaign, _ := mongoClient.NewQuery("campaigns").
		Eq("name", "Clear Perceptions AI Assistant Campaign").
		FindOne(ctx)

	if campaign == nil {
		// Try to find any campaign
		campaigns, _ := mongoClient.NewQuery("campaigns").
			Select("_id", "id", "name").
			Find(ctx)

		if len(campaigns) == 0 {
			log.Fatalf("No campaigns found. Please run 'make create-campaign' first.")
		}

		// Use the first campaign
		campaign = campaigns[0]
		fmt.Printf("⚠️  Using campaign: %v\n", campaign["name"])
	} else {
		fmt.Printf("✅ Found campaign: %v\n", campaign["name"])
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

	fmt.Printf("Campaign ID: %v\n", campaignID)
	fmt.Println()

	// Check if contact is already in campaign
	existingCampaignContact, _ := mongoClient.NewQuery("campaign_contacts").
		Eq("campaign_id", campaignID).
		Eq("contact_id", contactID).
		FindOne(ctx)

	if existingCampaignContact != nil {
		fmt.Printf("⚠️  Contact is already in this campaign\n")
		fmt.Printf("Status: %v\n", existingCampaignContact["status"])
	} else {
		// Add contact to campaign
		campaignContactData := map[string]interface{}{
			"campaign_id": campaignID,
			"contact_id":  contactID,
			"status":      "pending",
			"created_at":  time.Now().Format(time.RFC3339),
		}

		_, err = mongoClient.NewQuery("campaign_contacts").Insert(ctx, campaignContactData)
		if err != nil {
			log.Fatalf("Failed to add contact to campaign: %v", err)
		}

		fmt.Printf("✅ Contact added to campaign successfully!\n")
	}

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("✅ Complete!")
	fmt.Println("========================================")
	fmt.Println()
	fmt.Printf("Contact: %s (%s)\n", contactName, phoneNumber)
	fmt.Printf("Campaign: %v\n", campaign["name"])
	fmt.Println()
	fmt.Println("The contact is ready to receive calls from the campaign.")
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

