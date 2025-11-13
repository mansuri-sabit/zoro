package mongo

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"

	"github.com/troikatech/calling-agent/pkg/logger"
)

// Client wraps the MongoDB client and database
type Client struct {
	client   *mongo.Client
	database *mongo.Database
	dbName   string
}

// NewClient creates a new MongoDB client and connects to the database
func NewClient(mongoURI, dbName string) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOptions := options.Client().
		ApplyURI(mongoURI).
		SetMaxPoolSize(100).
		SetMinPoolSize(10).
		SetMaxConnIdleTime(30 * time.Second).
		SetServerSelectionTimeout(5 * time.Second)

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// Ping to verify connection
	if err := client.Ping(ctx, nil); err != nil {
		client.Disconnect(ctx)
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	database := client.Database(dbName)

	logger.Log.Info("MongoDB connection established",
		zap.String("uri", maskURI(mongoURI)),
		zap.String("database", dbName),
	)

	return &Client{
		client:   client,
		database: database,
		dbName:   dbName,
	}, nil
}

// Database returns the MongoDB database instance
func (c *Client) Database() *mongo.Database {
	return c.database
}

// Collection returns a MongoDB collection by name
func (c *Client) Collection(name string) *mongo.Collection {
	return c.database.Collection(name)
}

// Ping checks the connection to MongoDB
func (c *Client) Ping(ctx context.Context) error {
	return c.client.Ping(ctx, nil)
}

// Disconnect closes the MongoDB connection
func (c *Client) Disconnect(ctx context.Context) error {
	return c.client.Disconnect(ctx)
}

// Client returns the underlying MongoDB client (for advanced operations)
func (c *Client) Client() *mongo.Client {
	return c.client
}

// DBName returns the database name
func (c *Client) DBName() string {
	return c.dbName
}

// maskURI masks sensitive parts of MongoDB URI for logging
func maskURI(uri string) string {
	// Simple masking: show only scheme and host, mask credentials
	// mongodb://user:pass@host:port/db -> mongodb://***:***@host:port/db
	if len(uri) < 10 {
		return "***"
	}
	// Find @ symbol to mask credentials
	for i := 0; i < len(uri); i++ {
		if uri[i] == '@' {
			// Find : after mongodb://
			schemeEnd := 0
			for j := 0; j < i; j++ {
				if uri[j] == ':' && j+2 < len(uri) && uri[j+1] == '/' && uri[j+2] == '/' {
					schemeEnd = j + 3
					break
				}
			}
			if schemeEnd > 0 {
				return uri[:schemeEnd] + "***:***" + uri[i:]
			}
		}
	}
	return uri
}
