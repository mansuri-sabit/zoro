package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/troikatech/calling-agent/pkg/exotel"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: go run cmd/check-exotel-call/main.go <call_sid>")
	}

	callSID := os.Args[1]

	// Load environment variables
	subdomain := os.Getenv("EXOTEL_SUBDOMAIN")
	accountSID := os.Getenv("EXOTEL_ACCOUNT_SID")
	apiKey := os.Getenv("EXOTEL_API_KEY")
	apiToken := os.Getenv("EXOTEL_API_TOKEN")

	if subdomain == "" || accountSID == "" || apiKey == "" || apiToken == "" {
		log.Fatalf("Missing Exotel environment variables")
	}

	fmt.Println("========================================")
	fmt.Printf("Checking Exotel Call Status: %s\n", callSID)
	fmt.Println("========================================")
	fmt.Println()

	// Create Exotel client
	client := exotel.NewClient(subdomain, accountSID, apiKey, apiToken)

	// Get call status
	status, err := client.GetCallStatus(callSID)
	if err != nil {
		log.Fatalf("Failed to get call status: %v", err)
	}

	fmt.Println("✅ Exotel Call Status:")
	fmt.Println("----------------------------------------")
	fmt.Printf("Call SID: %s\n", status.Call.Sid)
	fmt.Printf("Status: %s\n", status.Call.Status)
	fmt.Printf("Direction: %s\n", status.Call.Direction)
	fmt.Printf("From: %s\n", status.Call.From)
	fmt.Printf("To: %s\n", status.Call.To)
	fmt.Printf("Start Time: %s\n", status.Call.StartTime)
	fmt.Printf("End Time: %s\n", status.Call.EndTime)
	fmt.Printf("Duration: %s\n", status.Call.Duration)

	// Pretty print full response
	fmt.Println()
	fmt.Println("Full Response:")
	fmt.Println("----------------------------------------")
	prettyJSON, _ := json.MarshalIndent(status, "", "  ")
	fmt.Println(string(prettyJSON))

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("✅ Complete!")
	fmt.Println("========================================")
}
