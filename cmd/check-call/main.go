package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: go run cmd/check-call/main.go <call_sid>")
	}

	callSID := os.Args[1]

	// API endpoint
	baseURL := "https://zoro-yvye.onrender.com"
	if url := os.Getenv("API_URL"); url != "" {
		baseURL = url
	}

	fmt.Println("========================================")
	fmt.Printf("Checking Call Status: %s\n", callSID)
	fmt.Println("========================================")
	fmt.Println()

	// Step 1: Login to get auth token
	fmt.Println("Step 1: Logging in...")
	loginData := map[string]interface{}{
		"email":    "alizsabit@gmail.com",
		"password": "11111111",
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
		log.Fatalf("Please check your credentials")
	}

	accessToken, ok := loginResult["access_token"].(string)
	if !ok {
		log.Fatalf("No access token in login response: %s", string(loginBody))
	}

	fmt.Println("✅ Login successful!")
	fmt.Println()

	// Step 2: Get call status
	fmt.Println("Step 2: Getting call status...")
	url := baseURL + "/api/calls/" + callSID
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)

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
		var callData map[string]interface{}
		if err := json.Unmarshal(body, &callData); err == nil {
			fmt.Println("✅ Call Details:")
			fmt.Println("----------------------------------------")
			for key, value := range callData {
				fmt.Printf("%s: %v\n", key, value)
			}
		} else {
			fmt.Println("Response:", string(body))
		}
	} else {
		fmt.Printf("❌ Failed to get call status (Status: %d)\n", resp.StatusCode)
		fmt.Println("Response:", string(body))
	}

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("✅ Complete!")
	fmt.Println("========================================")
}

