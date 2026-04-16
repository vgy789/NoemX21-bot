package rocketchat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/vgy789/noemx21-bot/internal/pkg/netretry"
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
		baseURL:    normalizeBaseURL(baseURL),
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
		baseURL:    normalizeBaseURL(baseURL),
		authToken:  strings.TrimSpace(authToken),
		userID:     strings.TrimSpace(userID),
		httpClient: hc,
	}
}

func normalizeBaseURL(rawURL string) string {
	return strings.TrimRight(strings.TrimSpace(rawURL), "/")
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

	var body []byte
	err = netretry.Do(ctx, func() error {
		req, reqErr := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
		if reqErr != nil {
			return netretry.Permanent(fmt.Errorf("failed to create request: %w", reqErr))
		}

		req.Header.Set("X-Auth-Token", c.authToken)
		req.Header.Set("X-User-Id", c.userID)
		req.Header.Set("Content-Type", "application/json")

		resp, doErr := c.httpClient.Do(req)
		if doErr != nil {
			return doErr
		}
		defer func() { _ = resp.Body.Close() }()

		body, doErr = io.ReadAll(resp.Body)
		if doErr != nil {
			return doErr
		}

		if resp.StatusCode != http.StatusOK {
			httpErr := &netretry.HTTPStatusError{
				Method:     http.MethodPost,
				URL:        url,
				StatusCode: resp.StatusCode,
				Body:       string(body),
			}
			if netretry.IsRetryableStatusCode(resp.StatusCode) {
				return httpErr
			}
			return netretry.Permanent(httpErr)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
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

// MyProfileResponse from Rocket.Chat me endpoint.
type MyProfileResponse struct {
	ID     string `json:"_id"`
	Emails []struct {
		Address  string `json:"address"`
		Verified bool   `json:"verified"`
	} `json:"emails"`
	Username string `json:"username"`
	Success  bool   `json:"success"`
}

// GetUserInfo gets information about a user
func (c *Client) GetUserInfo(ctx context.Context, username string) (*UserInfoResponse, error) {
	// Use url.QueryEscape for safety against injection
	url := fmt.Sprintf("%s/users.info?username=%s", c.baseURL, url.QueryEscape(username))

	var body []byte
	err := netretry.Do(ctx, func() error {
		req, reqErr := http.NewRequestWithContext(ctx, "GET", url, nil)
		if reqErr != nil {
			return netretry.Permanent(fmt.Errorf("failed to create request: %w", reqErr))
		}
		req.Header.Set("X-Auth-Token", c.authToken)
		req.Header.Set("X-User-Id", c.userID)

		resp, doErr := c.httpClient.Do(req)
		if doErr != nil {
			return doErr
		}
		defer func() { _ = resp.Body.Close() }()

		body, doErr = io.ReadAll(resp.Body)
		if doErr != nil {
			return doErr
		}

		if resp.StatusCode != http.StatusOK {
			httpErr := &netretry.HTTPStatusError{
				Method:     http.MethodGet,
				URL:        url,
				StatusCode: resp.StatusCode,
				Body:       string(body),
			}
			if netretry.IsRetryableStatusCode(resp.StatusCode) {
				return httpErr
			}
			return netretry.Permanent(httpErr)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
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

// GetMyProfileWithToken fetches the current user's profile using their own user ID + auth token.
func (c *Client) GetMyProfileWithToken(ctx context.Context, userID, authToken string) (*MyProfileResponse, error) {
	requestURL := fmt.Sprintf("%s/me", c.baseURL)

	userID = strings.TrimSpace(userID)
	authToken = strings.TrimSpace(authToken)
	if userID == "" || authToken == "" {
		return nil, fmt.Errorf("user id and auth token are required")
	}

	var body []byte
	err := netretry.Do(ctx, func() error {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if reqErr != nil {
			return netretry.Permanent(fmt.Errorf("failed to create request: %w", reqErr))
		}

		req.Header.Set("X-Auth-Token", authToken)
		req.Header.Set("X-User-Id", userID)

		resp, doErr := c.httpClient.Do(req)
		if doErr != nil {
			return doErr
		}
		defer func() { _ = resp.Body.Close() }()

		body, doErr = io.ReadAll(resp.Body)
		if doErr != nil {
			return doErr
		}

		if resp.StatusCode != http.StatusOK {
			httpErr := &netretry.HTTPStatusError{
				Method:     http.MethodGet,
				URL:        requestURL,
				StatusCode: resp.StatusCode,
				Body:       string(body),
			}
			if netretry.IsRetryableStatusCode(resp.StatusCode) {
				return httpErr
			}
			return netretry.Permanent(httpErr)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	var profile MyProfileResponse
	if err := json.Unmarshal(body, &profile); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if !profile.Success {
		return nil, fmt.Errorf("API error: success=false, body: %s", string(body))
	}

	return &profile, nil
}
