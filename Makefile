.PHONY: help run build test lint clean docker-build docker-run

help:
	@echo "Troika Calling Agent Platform - Makefile Commands"
	@echo ""
	@echo "  make run          - Run unified server locally"
	@echo "  make build        - Build server binary"
	@echo "  make test         - Run all tests"
	@echo "  make lint         - Run linters"
	@echo "  make clean        - Clean build artifacts"
	@echo "  make docker-build - Build Docker image"
	@echo "  make docker-run   - Run Docker container"
	@echo ""

run:
	@echo "Starting unified server..."
	@echo "Note: Ensure .env file is configured"
	@go run ./cmd/server

build:
	@echo "Building server..."
	@go build -o bin/server ./cmd/server
	@echo "Build complete: bin/server"

build-utils:
	@echo "Building utility commands..."
	@go build -o bin/check-db ./cmd/check-db
	@go build -o bin/create-user ./cmd/create-user
	@echo "Build complete: bin/check-db, bin/create-user"

test:
	@echo "Running tests..."
	@go test -v -cover ./...

lint:
	@echo "Running linters..."
	@go vet ./...
	@golangci-lint run ./... || echo "golangci-lint not installed, skipping"

clean:
	@echo "Cleaning build artifacts..."
	@rm -rf bin/
	@rm -f server.exe check-db.exe create-user.exe
	@echo "Clean complete"

docker-build:
	@echo "Building Docker image..."
	@docker build -t troika-server:latest .
	@echo "Docker image built: troika-server:latest"

docker-run:
	@echo "Running Docker container..."
	@docker run -p 8080:8080 --env-file .env troika-server:latest

.PHONY: check-db create-user upload-persona set-campaign-persona create-campaign add-contact update-campaign-flow

check-db:
	@echo "Running database check..."
	@go run ./cmd/check-db

create-user:
	@echo "Creating user..."
	@go run ./cmd/create-user

upload-persona:
	@echo "Uploading persona and knowledge base to MongoDB..."
	@go run ./cmd/upload-persona

set-campaign-persona:
	@echo "Setting persona_id in all campaigns..."
	@go run ./cmd/set-campaign-persona

create-campaign:
	@echo "Creating Clear Perceptions campaign..."
	@go run ./cmd/create-campaign

add-contact:
	@echo "Adding contact to campaign..."
	@go run ./cmd/add-contact

update-campaign-flow:
	@echo "Updating campaign flow_id..."
	@go run ./cmd/update-campaign-flow

