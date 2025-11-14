package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	// API endpoint
	baseURL := "https://zoro-yvye.onrender.com"
	if url := os.Getenv("API_URL"); url != "" {
		baseURL = url
	}

	// Get phone number from command line or use default
	targetNumber := "+919324606985"
	if len(os.Args) > 1 {
		targetNumber = os.Args[1]
		// Remove spaces and ensure +91 format
		targetNumber = strings.ReplaceAll(targetNumber, " ", "")
		if !strings.HasPrefix(targetNumber, "+") {
			if strings.HasPrefix(targetNumber, "91") {
				targetNumber = "+" + targetNumber
			} else if strings.HasPrefix(targetNumber, "0") {
				targetNumber = "+91" + targetNumber[1:]
			} else {
				targetNumber = "+91" + targetNumber
			}
		}
	}

	fmt.Println("========================================")
	fmt.Printf("Making Call to %s\n", targetNumber)
	fmt.Println("========================================")
	fmt.Println()

	// Step 1: Login to get auth token
	fmt.Println("Step 1: Logging in...")
	loginData := map[string]interface{}{
		"email":    "alizsabit@gmail.com", // Default admin user
		"password": "11111111",             // Default password
	}

	loginJSON, err := json.Marshal(loginData)
	if err != nil {
		log.Fatalf("Failed to marshal login request: %v", err)
	}

	loginURL := baseURL + "/auth/login"
	loginReq, err := http.NewRequest("POST", loginURL, bytes.NewBuffer(loginJSON))
	if err != nil {
		log.Fatalf("Failed to create login request: %v", err)
	}

	loginReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	loginResp, err := client.Do(loginReq)
	if err != nil {
		log.Fatalf("Failed to login: %v", err)
	}
	defer loginResp.Body.Close()

	loginBody, err := io.ReadAll(loginResp.Body)
	if err != nil {
		log.Fatalf("Failed to read login response: %v", err)
	}

	var loginResult map[string]interface{}
	if err := json.Unmarshal(loginBody, &loginResult); err != nil {
		log.Fatalf("Failed to parse login response: %v\nResponse: %s", err, string(loginBody))
	}

	if loginResp.StatusCode != http.StatusOK {
		fmt.Printf("❌ Login failed (Status: %d)\n", loginResp.StatusCode)
		fmt.Printf("Response: %s\n", string(loginBody))
		log.Fatalf("Please check your credentials or create a user first")
	}

	accessToken, ok := loginResult["access_token"].(string)
	if !ok {
		log.Fatalf("No access token in login response: %s", string(loginBody))
	}

	fmt.Println("✅ Login successful!")
	fmt.Println()

	// Step 2: Make the call
	fmt.Println("Step 2: Initiating call...")
	callData := map[string]interface{}{
		"from":    "+917948516111", // Exotel Exophone
		"to":      targetNumber,    // Target number
		"flow_id": "1116870",       // Exotel Applet ID
	}

	jsonData, err := json.Marshal(callData)
	if err != nil {
		log.Fatalf("Failed to marshal request: %v", err)
	}

	// Create HTTP request
	url := baseURL + "/api/calls"
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	// Make request
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Failed to read response: %v", err)
	}

	fmt.Printf("URL: %s\n", url)
	fmt.Printf("Status Code: %d\n", resp.StatusCode)
	fmt.Println()

	if resp.StatusCode == http.StatusOK {
		var result map[string]interface{}
		if err := json.Unmarshal(body, &result); err == nil {
			fmt.Println("✅ Call initiated successfully!")
			if callSid, ok := result["call_sid"].(string); ok {
				fmt.Printf("Call SID: %s\n", callSid)
			}
			if status, ok := result["status"].(string); ok {
				fmt.Printf("Status: %s\n", status)
			}
			if message, ok := result["message"].(string); ok {
				fmt.Printf("Message: %s\n", message)
			}
		} else {
			fmt.Println("Response:", string(body))
		}
	} else {
		fmt.Printf("❌ Call initiation failed (Status: %d)\n", resp.StatusCode)
		fmt.Println("Response:", string(body))
		var errorResp map[string]interface{}
		if err := json.Unmarshal(body, &errorResp); err == nil {
			if errorMsg, ok := errorResp["error"].(string); ok {
				fmt.Printf("Error: %s\n", errorMsg)
			} else if detail, ok := errorResp["detail"].(string); ok {
				fmt.Printf("Detail: %s\n", detail)
			}
		}
	}

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("✅ Complete!")
	fmt.Println("========================================")
}

