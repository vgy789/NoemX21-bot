package s21

import (
"context"
"encoding/json"
"fmt"
"net/http"
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

	req, err := http.NewRequestWithContext(ctx, "GET", apiUrl, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "noemx21-bot/0.0.1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: status %d", resp.StatusCode)
	}

	var wrapper CampusesResponse
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return wrapper.Campuses, nil
}
