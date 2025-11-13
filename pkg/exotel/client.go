package exotel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	subdomain  string
	accountSID string
	apiKey     string
	apiToken   string
	httpClient *http.Client
}

func NewClient(subdomain, accountSID, apiKey, apiToken string) *Client {
	return &Client{
		subdomain:  subdomain,
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
	data.Set("From", req.From)
	data.Set("To", req.To)
	data.Set("CallerId", req.CallerID)
	data.Set("CallType", req.CallType)
	if req.CallbackURL != "" {
		data.Set("StatusCallback", req.CallbackURL)
	}

	httpReq, err := http.NewRequest("POST", endpoint, bytes.NewBufferString(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.SetBasicAuth(c.apiKey, c.apiToken)
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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
