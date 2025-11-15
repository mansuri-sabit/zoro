package exotel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	subdomain  string
	accountSID string
	apiKey     string
	apiToken   string
	httpClient *http.Client
}

// normalizeSubdomain removes .exotel.com if already present in subdomain
func normalizeSubdomain(subdomain string) string {
	if strings.Contains(subdomain, ".exotel.com") {
		return strings.ReplaceAll(subdomain, ".exotel.com", "")
	}
	return subdomain
}

func NewClient(subdomain, accountSID, apiKey, apiToken string) *Client {
	return &Client{
		subdomain:  normalizeSubdomain(subdomain),
		accountSID: accountSID,
		apiKey:     apiKey,
		apiToken:   apiToken,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

type ConnectCallRequest struct {
	From        string
	To          string
	CallerID    string
	CallType    string
	CallbackURL string
	AppletID    string // Applet ID for voicebot calls - will be converted to Url parameter
	AccountSID  string // Account SID for building voicebot URL
	// Additional parameters for Voicebot Applets
	CustomField string // Custom field to pass target number (alternative to To)
	UserData    string // User data JSON string (alternative to To)
	Url         string // Voicebot URL (alternative to AppletID) - format: http://my.exotel.com/{sid}/exoml/start_voice/{appId}
}

type ConnectCallResponse struct {
	Call struct {
		Sid       string `json:"Sid"`
		Status    string `json:"Status"`
		Direction string `json:"Direction"`
	} `json:"Call"`
}

func (c *Client) ConnectCall(req ConnectCallRequest) (*ConnectCallResponse, error) {
	endpoint := fmt.Sprintf("https://%s.exotel.com/v1/Accounts/%s/Calls/connect.json",
		c.subdomain, c.accountSID)

	data := url.Values{}

	// CRITICAL FIX: Correct parameter mapping for outbound Voicebot calls
	// For outbound Voicebot calls:
	// - From = customer's phone number (endUserNumber / target number)
	// - CallerId = our Virtual Exophone (e.g., 07948516111)
	// - To = target number (optional for Voicebot Applets, but we'll set it)
	// - Url = Voicebot Applet URL that returns WebSocket URL
	// - AppletID is used to route through the Voicebot Applet

	// CRITICAL: Use HCall pattern - build voicebot URL if AppletID is provided
	// HCall uses: http://my.exotel.com/{sid}/exoml/start_voice/{appId}
	// This is more reliable than using AppletID parameter
	var voicebotUrl string
	if req.Url != "" {
		// Use provided URL directly
		voicebotUrl = req.Url
	} else if req.AppletID != "" && req.AccountSID != "" {
		// Build URL from AppletID and AccountSID (HCall pattern)
		voicebotUrl = fmt.Sprintf("http://my.exotel.com/%s/exoml/start_voice/%s", req.AccountSID, req.AppletID)
	}

	if voicebotUrl != "" || req.AppletID != "" {
		// Voicebot Applet call: Use HCall pattern which is more reliable
		// HCall uses: From = customer number, CallerId = Exophone, Url = voicebot URL
		//
		// CRITICAL: For Exotel ConnectCall API with Voicebot Applets (HCall pattern):
		// - From = Customer number (who to call) - HCall pattern
		// - To = NOT USED in v1 API for Voicebot calls
		// - CallerId = Exophone (what shows on recipient's phone)
		// - Url = Voicebot applet URL (http://my.exotel.com/{sid}/exoml/start_voice/{appId})

		// Use CallerID as From if not provided (should be Exophone)
		fromNumber := req.CallerID
		if fromNumber == "" {
			fromNumber = req.From
		}

		// CRITICAL: Use To parameter as the customer number (HCall pattern)
		// In HCall, From parameter contains the customer number
		customerNumber := req.To
		if customerNumber == "" {
			// Fallback: if To not provided, try From (should contain customer number)
			customerNumber = req.From
		}

		// CRITICAL: Validate that customer number is not same as Exophone
		normalizedCustomer := strings.ReplaceAll(customerNumber, " ", "")
		normalizedCustomer = strings.ReplaceAll(normalizedCustomer, "-", "")
		normalizedCustomer = strings.ReplaceAll(normalizedCustomer, "+", "")
		normalizedFrom := strings.ReplaceAll(fromNumber, " ", "")
		normalizedFrom = strings.ReplaceAll(normalizedFrom, "-", "")
		normalizedFrom = strings.ReplaceAll(normalizedFrom, "+", "")
		
		// Remove country code prefix for comparison
		if strings.HasPrefix(normalizedCustomer, "91") && len(normalizedCustomer) == 12 {
			normalizedCustomer = normalizedCustomer[2:]
		}
		if strings.HasPrefix(normalizedFrom, "91") && len(normalizedFrom) == 12 {
			normalizedFrom = normalizedFrom[2:]
		}
		// Remove leading 0 for comparison
		if strings.HasPrefix(normalizedCustomer, "0") {
			normalizedCustomer = normalizedCustomer[1:]
		}
		if strings.HasPrefix(normalizedFrom, "0") {
			normalizedFrom = normalizedFrom[1:]
		}
		
		if normalizedCustomer == normalizedFrom {
			return nil, fmt.Errorf("CRITICAL: Customer number (%s) matches Exophone (%s) - this will cause self-call", customerNumber, fromNumber)
		}

		// Set parameters using HCall pattern
		if voicebotUrl != "" {
			// Use Url parameter (HCall pattern - more reliable)
			data.Set("From", customerNumber)      // Customer number to call (HCall pattern)
			data.Set("CallerId", fromNumber)      // Exophone (what shows on caller ID)
			data.Set("Url", voicebotUrl)          // Voicebot applet URL (HCall pattern)
			if req.CustomField != "" {
				data.Set("CustomField", req.CustomField)
			}
		} else {
			// Fallback: Use AppletID (current pattern)
			data.Set("From", fromNumber)       // Virtual Exophone (makes the call)
			data.Set("To", customerNumber)     // Target number (customer we're calling)
			data.Set("CallerId", req.CallerID) // Virtual Exophone (caller ID)
			data.Set("CallType", req.CallType)
		}

		// Log the actual parameters being sent (HCall pattern)
		if voicebotUrl != "" {
			fmt.Printf("[INFO] Exotel ConnectCall Request (HCall Pattern):\n")
			fmt.Printf("  - From (Customer): %s\n", customerNumber)
			fmt.Printf("  - CallerId (Exophone): %s\n", fromNumber)
			fmt.Printf("  - Url (Voicebot): %s\n", voicebotUrl)
			if req.CustomField != "" {
				fmt.Printf("  - CustomField: %s\n", req.CustomField)
			}
			fmt.Printf("  - AppletID: %s\n", req.AppletID)
			fmt.Printf("[INFO] Using HCall pattern - more reliable than AppletID parameter\n")
		} else {
			// Fallback logging for AppletID pattern
			fmt.Printf("[INFO] Exotel ConnectCall Request (AppletID Pattern):\n")
			fmt.Printf("  - From: %s\n", fromNumber)
			fmt.Printf("  - To: %s\n", customerNumber)
			fmt.Printf("  - CallerId: %s\n", req.CallerID)
			fmt.Printf("  - AppletID: %s\n", req.AppletID)
			fmt.Printf("[WARNING] Using AppletID parameter - less reliable than Url pattern\n")
		}
	} else {
		// Regular call (non-Voicebot)
		data.Set("From", req.From)
		data.Set("To", req.To)
		data.Set("CallerId", req.CallerID)
		data.Set("CallType", req.CallType)
	}

	if req.CallbackURL != "" {
		data.Set("StatusCallback", req.CallbackURL)
	}

	httpReq, err := http.NewRequest("POST", endpoint, bytes.NewBufferString(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.SetBasicAuth(c.apiKey, c.apiToken)
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Log the request for debugging (without sensitive data)
	// Extract From value from data for logging
	logFrom := data.Get("From")
	fmt.Printf("[Exotel ConnectCall] From=%s, To=%s, CallerId=%s, AppletID=%s\n",
		logFrom, data.Get("To"), data.Get("CallerId"), req.AppletID)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("exotel API error: %s (status %d)", string(body), resp.StatusCode)
	}

	var result ConnectCallResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

type CreateCampaignRequest struct {
	Name        string   `json:"name"`
	ContentType string   `json:"content_type"`
	ContentID   string   `json:"content_id"`
	Settings    Settings `json:"settings"`
	List        []string `json:"list"`
}

type Settings struct {
	ScheduleTime string `json:"schedule_time,omitempty"`
	CallType     string `json:"call_type"`
	CallerID     string `json:"caller_id"`
}

type CreateCampaignResponse struct {
	Campaign struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	} `json:"campaign"`
}

func (c *Client) CreateCampaign(req CreateCampaignRequest) (*CreateCampaignResponse, error) {
	endpoint := fmt.Sprintf("https://%s.exotel.com/v2/accounts/%s/campaigns",
		c.subdomain, c.accountSID)

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.SetBasicAuth(c.apiKey, c.apiToken)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("exotel API error: %s (status %d)", string(body), resp.StatusCode)
	}

	var result CreateCampaignResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

func (c *Client) PauseCampaign(campaignID string) error {
	endpoint := fmt.Sprintf("https://%s.exotel.com/v2/accounts/%s/campaigns/%s/pause",
		c.subdomain, c.accountSID, campaignID)

	httpReq, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.SetBasicAuth(c.apiKey, c.apiToken)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("exotel API error: %s (status %d)", string(body), resp.StatusCode)
	}

	return nil
}

func (c *Client) ResumeCampaign(campaignID string) error {
	endpoint := fmt.Sprintf("https://%s.exotel.com/v2/accounts/%s/campaigns/%s/resume",
		c.subdomain, c.accountSID, campaignID)

	httpReq, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.SetBasicAuth(c.apiKey, c.apiToken)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("exotel API error: %s (status %d)", string(body), resp.StatusCode)
	}

	return nil
}

func (c *Client) CancelCampaign(campaignID string) error {
	endpoint := fmt.Sprintf("https://%s.exotel.com/v2/accounts/%s/campaigns/%s/cancel",
		c.subdomain, c.accountSID, campaignID)

	httpReq, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.SetBasicAuth(c.apiKey, c.apiToken)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("exotel API error: %s (status %d)", string(body), resp.StatusCode)
	}

	return nil
}

// GetCallStatus gets the status of a call from Exotel API
func (c *Client) GetCallStatus(callSID string) (*CallStatusResponse, error) {
	endpoint := fmt.Sprintf("https://%s.exotel.com/v1/Accounts/%s/Calls/%s.json",
		c.subdomain, c.accountSID, callSID)

	httpReq, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.SetBasicAuth(c.apiKey, c.apiToken)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("exotel API error: %s (status %d)", string(body), resp.StatusCode)
	}

	var result CallStatusResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

type CallStatusResponse struct {
	Call struct {
		Sid       string `json:"Sid"`
		Status    string `json:"Status"`
		Direction string `json:"Direction"`
		From      string `json:"From"`
		To        string `json:"To"`
		StartTime string `json:"StartTime"`
		EndTime   string `json:"EndTime"`
		Duration  string `json:"Duration"`
	} `json:"Call"`
}
