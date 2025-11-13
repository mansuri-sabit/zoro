package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/troikatech/calling-agent/pkg/auth"
	"github.com/troikatech/calling-agent/pkg/env"
	"github.com/troikatech/calling-agent/pkg/logger"
	"github.com/troikatech/calling-agent/pkg/mongo"
)

func main() {
	// Load environment variables
	cfg, err := env.Load(".env")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
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

	// User details
	email := "alizsabit@gmail.com"
	password := "11111111"
	role := "admin" // You can change this to "operator", "viewer", etc.

	// Check if user already exists
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	existingUser, _ := mongoClient.NewQuery("users").
		Select("id", "email").
		Eq("email", email).
		FindOne(ctx)

	if existingUser != nil {
		fmt.Printf("❌ User with email %s already exists!\n", email)
		os.Exit(1)
	}

	// Hash password
	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		log.Fatalf("Failed to hash password: %v", err)
	}

	// Generate UUID
	userID := uuid.New().String()

	// Create user document
	userData := map[string]interface{}{
		"id":            userID,
		"email":         email,
		"password_hash": passwordHash,
		"role":          role,
		"is_active":     true,
		"created_at":    time.Now().Format(time.RFC3339),
	}

	// Insert user
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()

	_, err = mongoClient.NewQuery("users").Insert(ctx2, userData)
	if err != nil {
		log.Fatalf("Failed to create user: %v", err)
	}

	fmt.Printf("✅ User created successfully!\n")
	fmt.Printf("   Email: %s\n", email)
	fmt.Printf("   Password: %s\n", password)
	fmt.Printf("   Role: %s\n", role)
	fmt.Printf("   ID: %s\n", userID)
	fmt.Printf("\nYou can now login with:\n")
	fmt.Printf("   Email: %s\n", email)
	fmt.Printf("   Password: %s\n", password)
}

