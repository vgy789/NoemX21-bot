package rocketchat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client for Rocket.Chat API
type Client struct {
	baseURL    string
	httpClient *http.Client
	authToken  string
	userID     string
}

// NewClient creates a new Rocket.Chat client
func NewClient(baseURL, authToken, userID string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		authToken:  strings.TrimSpace(authToken),
		userID:     strings.TrimSpace(userID),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// NewClientWithHTTPClient creates a new Rocket.Chat client with custom HTTP client
func NewClientWithHTTPClient(baseURL, authToken, userID string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		baseURL:    baseURL,
		authToken:  authToken,
		userID:     userID,
		httpClient: hc,
	}
}

// MessageRequest for sending messages
type MessageRequest struct {
	Channel string `json:"channel"`
	Text    string `json:"text"`
}

// MessageResponse from Rocket.Chat
type MessageResponse struct {
	ID        string          `json:"_id"`
	RoomID    string          `json:"rid"`
	Message   string          `json:"msg"`
	Timestamp json.RawMessage `json:"ts"` // Can be string or number
}

// SendDirectMessage sends a direct message to a user
func (c *Client) SendDirectMessage(ctx context.Context, userID, text string) (*MessageResponse, error) {
	url := fmt.Sprintf("%s/chat.postMessage", c.baseURL)

	reqBody := MessageRequest{
		Channel: userID,
		Text:    text,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Auth-Token", c.authToken)
	req.Header.Set("X-User-Id", c.userID)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var msgResp MessageResponse
	if err := json.Unmarshal(body, &msgResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &msgResp, nil
}

// UserInfoResponse from Rocket.Chat user.info
type UserInfoResponse struct {
	User struct {
		ID     string `json:"_id"`
		Emails []struct {
			Address  string `json:"address"`
			Verified bool   `json:"verified"`
		} `json:"emails"`
		Username string `json:"username"`
	} `json:"user"`
	Success bool `json:"success"`
}

// GetUserInfo gets information about a user
func (c *Client) GetUserInfo(ctx context.Context, username string) (*UserInfoResponse, error) {
	fmt.Printf("DEBUG baseURL: |%s|\n", c.baseURL)
	url := fmt.Sprintf("%s/users.info?username=%s", c.baseURL, username)
	fmt.Printf("DEBUG final URL: |%s|\n", url)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Auth-Token", c.authToken)
	req.Header.Set("X-User-Id", c.userID)

	// DEBUG LOGGING
	fmt.Printf("DEBUG REQUEST: %s %s|\n", req.Method, req.URL.String())
	for k, v := range req.Header {
		fmt.Printf("HEADER %s: %s|\n", k, strings.Join(v, ","))
	}
	// END DEBUG LOGGING

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var userInfo UserInfoResponse
	if err := json.Unmarshal(body, &userInfo); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if !userInfo.Success {
		return nil, fmt.Errorf("API error: success=false, body: %s", string(body))
	}

	return &userInfo, nil
}
