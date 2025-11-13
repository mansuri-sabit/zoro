package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/troikatech/calling-agent/pkg/env"
	"github.com/troikatech/calling-agent/pkg/mongo"
)

func main() {
	fmt.Println("========================================")
	fmt.Println("Database Connection Diagnostic Tool")
	fmt.Println("========================================")
	fmt.Println()

	// Load config
	cfg, err := env.Load(".env")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	fmt.Printf("MongoDB URI: %s\n", maskURL(cfg.MongoURI))
	fmt.Printf("Database Name: %s\n", cfg.DBName)
	fmt.Println()

	// Create MongoDB client
	client, err := mongo.NewClient(cfg.MongoURI, cfg.DBName)
	if err != nil {
		log.Fatalf("Failed to create MongoDB client: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client.Disconnect(ctx)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test 1: Try to query campaigns collection
	fmt.Println("Test 1: Querying campaigns collection...")
	_, err = client.NewQuery("campaigns").
		Select("id").
		Limit(1).
		FindOne(ctx)

	if err != nil {
		fmt.Printf("❌ ERROR: %v\n", err)
		fmt.Println()
		fmt.Println("This error indicates:")
		fmt.Println("  1. Collection doesn't exist in database, OR")
		fmt.Println("  2. MongoDB connection failed")
		fmt.Println()
		fmt.Println("SOLUTION:")
		fmt.Println("  Ensure MongoDB is running and accessible")
		fmt.Println("  Check MONGO_URI and DB_NAME in .env file")
		os.Exit(1)
	}

	fmt.Printf("✅ SUCCESS: Campaigns collection is accessible\n")
	fmt.Println()

	// Test 2: Check other required collections
	requiredCollections := []string{
		"contacts",
		"campaign_contacts",
		"calls",
		"personas",
		"suppression",
		"users",
	}

	fmt.Println("Test 2: Checking other required collections...")
	allGood := true
	for _, collection := range requiredCollections {
		_, err := client.NewQuery(collection).
			Select("id").
			Limit(1).
			FindOne(ctx)

		if err != nil {
			fmt.Printf("❌ %s: %v\n", collection, err)
			allGood = false
		} else {
			fmt.Printf("✅ %s: OK\n", collection)
		}
	}

	fmt.Println()
	if allGood {
		fmt.Println("========================================")
		fmt.Println("✅ All checks passed!")
		fmt.Println("========================================")
		fmt.Println()
		fmt.Println("Your database is properly configured.")
		fmt.Println("If you're still seeing errors, restart your services.")
	} else {
		fmt.Println("========================================")
		fmt.Println("⚠️  Some collections are missing")
		fmt.Println("========================================")
		fmt.Println()
		fmt.Println("Collections will be created automatically when first used.")
		fmt.Println("If collections should exist, check MongoDB connection and database name.")
		os.Exit(1)
	}
}

func maskURL(url string) string {
	if len(url) < 20 {
		return url
	}
	return url[:20] + "..."
}

func maskKey(key string) string {
	if len(key) < 10 {
		return "***"
	}
	return key[:10]
}
