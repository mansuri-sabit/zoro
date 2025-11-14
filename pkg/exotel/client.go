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

	if req.AppletID != "" {
		// Voicebot Applet call: Use correct mapping for Exotel API
		// CRITICAL: For Exotel ConnectCall API with Voicebot Applets:
		// - From = Virtual Exophone (the number that will make the call)
		// - To = Target number (customer we're calling)
		// - CallerId = Virtual Exophone (what shows on caller ID - same as From)
		// Note: For Voicebot Applets, Exotel uses the AppletID to route to our init endpoint
		// The Applet is configured in Exotel Dashboard with our voicebot/init endpoint URL

		// Use CallerID as From (Virtual Exophone makes the call)
		fromNumber := req.CallerID
		if fromNumber == "" {
			// Fallback: if CallerID not provided, use From (should be Exophone)
			fromNumber = req.From
		}

		data.Set("From", fromNumber)       // Virtual Exophone (makes the call)
		data.Set("To", req.To)             // Target number (customer we're calling)
		data.Set("CallerId", req.CallerID) // Virtual Exophone (caller ID)
		data.Set("CallType", req.CallType)

		// CRITICAL: For Voicebot Applets, also pass target number in UserData
		// This ensures Exotel preserves it even if To parameter is ignored
		if req.To != "" {
			data.Set("UserData", fmt.Sprintf(`{"target_number":"%s"}`, req.To))
		}
		// Note: Url is not set here - Exotel uses Applet configuration
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
