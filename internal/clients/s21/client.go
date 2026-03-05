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
	"github.com/vgy789/noemx21-bot/internal/pkg/netretry"
)

//go:fix inline
func StringPtr(s string) *string {
	return new(s)
}

type ParticipantCampusV1DTO struct {
	ID        string `json:"id"`
	ShortName string `json:"shortName"`
}

type ParticipantProjectV1DTO struct {
	ID              int64      `json:"id"`
	Title           string     `json:"title"`
	Type            string     `json:"type"`
	Status          string     `json:"status"`
	FinalPercentage *int32     `json:"finalPercentage"`
	CompletionDate  *time.Time `json:"completionDateTime"`
}

type ParticipantProjectsV1DTO struct {
	Projects []ParticipantProjectV1DTO `json:"projects"`
}

type ParticipantV1DTO struct {
	Login          string                 `json:"login"`
	ClassName      *string                `json:"className"`
	ParallelName   *string                `json:"parallelName"`
	ExpValue       int64                  `json:"expValue"`
	Level          int32                  `json:"level"`
	ExpToNextLevel int64                  `json:"expToNextLevel"`
	Campus         ParticipantCampusV1DTO `json:"campus"`
	Status         string                 `json:"status"`
}

type ParticipantPointsV1DTO struct {
	PeerReviewPoints int32 `json:"peerReviewPoints"`
	CodeReviewPoints int32 `json:"codeReviewPoints"`
	Coins            int32 `json:"coins"`
}

type ParticipantSkillV1DTO struct {
	Name        string     `json:"name"`
	Points      int32      `json:"points"`
	Level       *int32     `json:"level"`
	LastUpdated *time.Time `json:"lastUpdated"`
}

type ParticipantSkillsV1DTO struct {
	Skills           []ParticipantSkillV1DTO `json:"skills"`
	TotalSkillPoints int64                   `json:"totalSkillPoints"`
}

type ParticipantCoalitionV1DTO struct {
	CoalitionID    int64      `json:"coalitionId"`
	CoalitionName  string     `json:"name"`
	JoinedDateTime *time.Time `json:"joinedDateTime"`
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

type ParticipantFeedbackV1DTO struct {
	Integrity    float64 `json:"averageVerifierInterest"`
	Friendliness float64 `json:"averageVerifierFriendliness"`
	Punctuality  float64 `json:"averageVerifierPunctuality"`
	Thoroughness float64 `json:"averageVerifierThoroughness"`
}

func (c *Client) GetParticipant(ctx context.Context, token string, login string) (*ParticipantV1DTO, error) {
	apiUrl := fmt.Sprintf("%s/participants/%s", c.baseURL, url.PathEscape(login))
	return getJSON[ParticipantV1DTO](ctx, c.httpClient, apiUrl, token)
}

func (c *Client) GetParticipantPoints(ctx context.Context, token string, login string) (*ParticipantPointsV1DTO, error) {
	apiUrl := fmt.Sprintf("%s/participants/%s/points", c.baseURL, url.PathEscape(login))
	return getJSON[ParticipantPointsV1DTO](ctx, c.httpClient, apiUrl, token)
}

func (c *Client) GetParticipantSkills(ctx context.Context, token string, login string) (*ParticipantSkillsV1DTO, error) {
	apiUrl := fmt.Sprintf("%s/participants/%s/skills", c.baseURL, url.PathEscape(login))
	return getJSON[ParticipantSkillsV1DTO](ctx, c.httpClient, apiUrl, token)
}

func (c *Client) GetParticipantCoalition(ctx context.Context, token string, login string) (*ParticipantCoalitionV1DTO, error) {
	apiUrl := fmt.Sprintf("%s/participants/%s/coalition", c.baseURL, url.PathEscape(login))
	return getJSON[ParticipantCoalitionV1DTO](ctx, c.httpClient, apiUrl, token)
}

func (c *Client) GetParticipantFeedback(ctx context.Context, token string, login string) (*ParticipantFeedbackV1DTO, error) {
	apiUrl := fmt.Sprintf("%s/participants/%s/feedback", c.baseURL, url.PathEscape(login))
	return getJSON[ParticipantFeedbackV1DTO](ctx, c.httpClient, apiUrl, token)
}

func (c *Client) GetParticipantProjects(ctx context.Context, token, login string, limit, offset int, status string) (*ParticipantProjectsV1DTO, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", fmt.Sprintf("%d", limit))
	}
	if offset > 0 {
		query.Set("offset", fmt.Sprintf("%d", offset))
	}
	if status != "" {
		query.Set("status", status)
	}

	apiURL := fmt.Sprintf("%s/participants/%s/projects", c.baseURL, url.PathEscape(login))
	if encoded := query.Encode(); encoded != "" {
		apiURL += "?" + encoded
	}

	return getJSON[ParticipantProjectsV1DTO](ctx, c.httpClient, apiURL, token)
}

func getJSON[T any](ctx context.Context, httpClient *http.Client, apiUrl string, token string) (*T, error) {
	var bodyBytes []byte
	err := netretry.Do(ctx, func() error {
		req, reqErr := http.NewRequestWithContext(ctx, "GET", apiUrl, nil)
		if reqErr != nil {
			return netretry.Permanent(reqErr)
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("User-Agent", "noemx21-bot/0.0.1")

		resp, doErr := httpClient.Do(req)
		if doErr != nil {
			return doErr
		}
		defer func() { _ = resp.Body.Close() }()

		bodyBytes, doErr = io.ReadAll(resp.Body)
		if doErr != nil {
			return doErr
		}

		if resp.StatusCode != http.StatusOK {
			httpErr := &netretry.HTTPStatusError{
				Method:     http.MethodGet,
				URL:        apiUrl,
				StatusCode: resp.StatusCode,
				Body:       string(bodyBytes),
			}
			if netretry.IsRetryableStatusCode(resp.StatusCode) {
				return httpErr
			}
			return netretry.Permanent(httpErr)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	var data T
	if err := json.Unmarshal(bodyBytes, &data); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w, body: %s", err, string(bodyBytes))
	}

	return &data, nil
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

	var bodyBytes []byte
	err := netretry.Do(ctx, func() error {
		reqAttempt, reqErr := http.NewRequestWithContext(ctx, "POST", authURL, strings.NewReader(params.Encode()))
		if reqErr != nil {
			return netretry.Permanent(fmt.Errorf("failed to create auth request: %w", reqErr))
		}
		reqAttempt.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		reqAttempt.Header.Set("User-Agent", "noemx21-bot/0.0.1")

		resp, doErr := c.httpClient.Do(reqAttempt)
		if doErr != nil {
			return doErr
		}
		defer func() { _ = resp.Body.Close() }()

		bodyBytes, doErr = io.ReadAll(resp.Body)
		if doErr != nil {
			return doErr
		}

		if resp.StatusCode != http.StatusOK {
			httpErr := &netretry.HTTPStatusError{
				Method:     http.MethodPost,
				URL:        authURL,
				StatusCode: resp.StatusCode,
				Body:       string(bodyBytes),
			}
			if netretry.IsRetryableStatusCode(resp.StatusCode) {
				return httpErr
			}
			return netretry.Permanent(fmt.Errorf("auth failed: status %d, body: %s", resp.StatusCode, string(bodyBytes)))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("auth request failed: %w", err)
	}

	var authResp AuthResponse
	if err := json.Unmarshal(bodyBytes, &authResp); err != nil {
		return nil, fmt.Errorf("failed to decode auth response: %w", err)
	}

	return &authResp, nil
}
