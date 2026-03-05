package s21

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

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
