package s21

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

type ParticipantV1DTO struct {
	Login        string      `json:"login"`
	ParallelName string      `json:"parallelName"`
	Status       interface{} `json:"status"`
}

type Client struct {
	baseURL    string
	authURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return NewClientWithHTTPClient(baseURL, &http.Client{Timeout: 10 * time.Second})
}

func NewClientWithHTTPClient(baseURL string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{baseURL: baseURL, authURL: defaultAuthURL, httpClient: hc}
}

func NewClientForTest(apiBaseURL, authBaseURL string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	authURL := defaultAuthURL
	if authBaseURL != "" {
		authURL = authBaseURL + "/auth/realms/EduPowerKeycloak/protocol/openid-connect/token"
	}
	return &Client{baseURL: apiBaseURL, authURL: authURL, httpClient: hc}
}

func (c *Client) GetParticipant(ctx context.Context, token string, login string) (*ParticipantV1DTO, error) {
	apiUrl := fmt.Sprintf("%s/participants/%s", c.baseURL, url.PathEscape(login))
	// For debugging, we can log the URL (make sure it's not sensitive in prod)
	// fmt.Printf("Calling URL: %s\n", url)

	req, err := http.NewRequestWithContext(ctx, "GET", apiUrl, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "noemx21-bot/0.0.1")

	resp, err := c.httpClient.Do(req)
	// log for debug (optional, but helps with the user's report)
	// fmt.Printf("API request: %s, token present: %v\n", apiUrl, token != "")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check HTTP status code first
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: status %d, url: %s, body: %s", resp.StatusCode, apiUrl, string(bodyBytes))
	}

	var participant ParticipantV1DTO
	if err := json.Unmarshal(bodyBytes, &participant); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w, body: %s", err, string(bodyBytes))
	}

	// Check if the body contains an error status (some APIs return 200 OK with error body)
	if status, ok := participant.Status.(float64); ok { // JSON numbers are float64 by default
		if status != 200 && status != 0 { // 0 might mean field missing
			return nil, fmt.Errorf("API body error: status %d, body: %s", int(status), string(bodyBytes))
		}
	}

	return &participant, nil
}

type AuthResponse struct {
	AccessToken      string `json:"access_token"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	NotBeforePolicy  int    `json:"not-before-policy"`
	SessionState     string `json:"session_state"`
	Scope            string `json:"scope"`
}

var jwtParser = jwt.NewParser(jwt.WithoutClaimsValidation())

func ParseAccessTokenClaims(accessToken string) (*jwt.RegisteredClaims, error) {
	var claims jwt.RegisteredClaims
	_, _, err := jwtParser.ParseUnverified(accessToken, &claims)
	if err != nil {
		return nil, fmt.Errorf("parse access token: %w", err)
	}
	return &claims, nil
}

func AccessTokenExpiry(accessToken string) (time.Time, bool) {
	claims, err := ParseAccessTokenClaims(accessToken)
	if err != nil || claims.ExpiresAt == nil {
		return time.Time{}, false
	}
	return claims.ExpiresAt.Time, true
}

const defaultAuthURL = "https://auth.21-school.ru/auth/realms/EduPowerKeycloak/protocol/openid-connect/token"

func (c *Client) Auth(ctx context.Context, username, password string) (*AuthResponse, error) {
	authURL := c.authURL
	if authURL == "" {
		authURL = defaultAuthURL
	}

	params := url.Values{}
	params.Set("client_id", "s21-open-api")
	params.Set("username", username)
	params.Set("password", password)
	params.Set("grant_type", "password")

	req, err := http.NewRequestWithContext(ctx, "POST", authURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create auth request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "noemx21-bot/0.0.1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("auth failed: status %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	var authResp AuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return nil, fmt.Errorf("failed to decode auth response: %w", err)
	}

	return &authResp, nil
}
