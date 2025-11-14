package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	wsURL := "wss://zoro-yvye.onrender.com/voicebot/ws?sample-rate=16000&call_sid=test123&from=%2B917948516111&to=%2B919324606985"
	
	if len(os.Args) > 1 {
		wsURL = os.Args[1]
	}

	fmt.Println("========================================")
	fmt.Println("Testing WebSocket Endpoint")
	fmt.Println("========================================")
	fmt.Printf("URL: %s\n", wsURL)
	fmt.Println()

	// Parse URL
	u, err := url.Parse(wsURL)
	if err != nil {
		log.Fatalf("Failed to parse URL: %v", err)
	}

	// Create WebSocket dialer
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
	}

	// Connect to WebSocket
	fmt.Println("Connecting to WebSocket...")
	conn, resp, err := dialer.Dial(u.String(), nil)
	if err != nil {
		fmt.Printf("❌ Connection failed!\n")
		fmt.Printf("Error: %v\n", err)
		if resp != nil {
			fmt.Printf("Status Code: %d\n", resp.StatusCode)
			fmt.Printf("Status: %s\n", resp.Status)
		}
		log.Fatalf("WebSocket connection failed")
	}
	defer conn.Close()

	fmt.Println("✅ WebSocket connection established!")
	fmt.Printf("Status Code: %d\n", resp.StatusCode)
	fmt.Println()

	// Send a test message (start event)
	fmt.Println("Sending test 'start' event...")
	startEvent := map[string]interface{}{
		"event":      "start",
		"stream_sid": "test_stream_123",
		"custom_parameters": map[string]interface{}{
			"persona_name":  "Test Persona",
			"persona_age":   "25",
			"tone":          "friendly",
			"gender":        "female",
			"city":          "Mumbai",
			"language":      "Hindi",
			"documents":     "Test document content",
			"customer_name": "Test Customer",
			"voice_id":      "shimmer",
		},
	}

	err = conn.WriteJSON(startEvent)
	if err != nil {
		log.Fatalf("Failed to send start event: %v", err)
	}
	fmt.Println("✅ Start event sent!")

	// Wait for response
	fmt.Println("\nWaiting for response (5 seconds)...")
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var response map[string]interface{}
	err = conn.ReadJSON(&response)
	if err != nil {
		if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
			fmt.Printf("⚠️  WebSocket closed: %v\n", err)
		} else {
			fmt.Printf("⚠️  No response received (this is OK if server is processing)\n")
		}
	} else {
		fmt.Println("✅ Received response:")
		fmt.Printf("%+v\n", response)
	}

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("✅ Test Complete!")
	fmt.Println("========================================")
}

