package s21

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/vgy789/noemx21-bot/internal/pkg/netretry"
)

type CampusV1DTO struct {
	ID        string `json:"id"`
	ShortName string `json:"shortName"`
	FullName  string `json:"fullName"`
	Timezone  string `json:"timezone"`
}

type CampusesResponse struct {
	Campuses []CampusV1DTO `json:"campuses"`
}

type CoalitionV1DTO struct {
	CoalitionID int64  `json:"coalitionId"`
	Name        string `json:"name"`
}

type CoalitionsResponse struct {
	Coalitions []CoalitionV1DTO `json:"coalitions"`
}

func (c *Client) GetCampuses(ctx context.Context, token string) ([]CampusV1DTO, error) {
	apiUrl := fmt.Sprintf("%s/campuses", c.baseURL)
	var bodyBytes []byte
	err := netretry.Do(ctx, func() error {
		req, reqErr := http.NewRequestWithContext(ctx, "GET", apiUrl, nil)
		if reqErr != nil {
			return netretry.Permanent(reqErr)
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("User-Agent", "noemx21-bot/0.0.1")

		resp, doErr := c.httpClient.Do(req)
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

	var wrapper CampusesResponse
	if err := json.Unmarshal(bodyBytes, &wrapper); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return wrapper.Campuses, nil
}

func (c *Client) GetCampusCoalitions(ctx context.Context, token, campusID string) ([]CoalitionV1DTO, error) {
	const pageSize = 1000

	all := make([]CoalitionV1DTO, 0, pageSize)
	for offset := 0; ; offset += pageSize {
		query := url.Values{}
		query.Set("limit", fmt.Sprintf("%d", pageSize))
		query.Set("offset", fmt.Sprintf("%d", offset))

		apiURL := fmt.Sprintf("%s/campuses/%s/coalitions?%s", c.baseURL, url.PathEscape(campusID), query.Encode())
		resp, err := getJSON[CoalitionsResponse](ctx, c.httpClient, apiURL, token)
		if err != nil {
			return nil, err
		}
		if resp == nil || len(resp.Coalitions) == 0 {
			break
		}

		all = append(all, resp.Coalitions...)
		if len(resp.Coalitions) < pageSize {
			break
		}
	}

	return all, nil
}
