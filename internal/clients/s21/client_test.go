package s21

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_Auth(t *testing.T) {
	authResp := AuthResponse{
		AccessToken: "test-token",
		ExpiresIn:   300,
		TokenType:   "Bearer",
	}
	client := newMockClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "openid-connect/token")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(authResp)
	}))

	got, err := client.Auth(context.Background(), "user", "pass")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "test-token", got.AccessToken)
	assert.Equal(t, 300, got.ExpiresIn)
}

func TestClient_Auth_failStatus(t *testing.T) {
	client := newMockClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("invalid credentials"))
	}))
	got, err := client.Auth(context.Background(), "user", "wrong")
	assert.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "401")
}

func TestClient_GetParticipant(t *testing.T) {
	client := newMockClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Contains(t, r.URL.Path, "participants/")
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ParticipantV1DTO{
			Login:        "testuser",
			ParallelName: new("Core program"),
			Status:       "ACTIVE",
		})
	}))
	got, err := client.GetParticipant(context.Background(), "tok", "testuser")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "testuser", got.Login)
	assert.Equal(t, "Core program", *got.ParallelName)
	assert.Equal(t, "ACTIVE", got.Status)
}

func TestClient_GetParticipant_apiError(t *testing.T) {
	client := newMockClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"NOT_FOUND"}`))
	}))
	got, err := client.GetParticipant(context.Background(), "tok", "unknown")
	assert.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "404")
}
func TestClient_GetParticipant_404InBody(t *testing.T) {
	client := newMockClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// The quirk: 200 OK but body says 404
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"status": 404,
			"code": "NOT_FOUND",
			"message": "Not found user by login maslenok"
		}`))
	}))
	got, err := client.GetParticipant(context.Background(), "tok", "maslenok")
	assert.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "failed to decode response")
}

func TestClient_GetParticipant_StatusAsString(t *testing.T) {
	client := newMockClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"login": "user",
			"status": "ACTIVE",
			"parallelName": "Core program"
		}`))
	}))
	got, err := client.GetParticipant(context.Background(), "tok", "user")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "ACTIVE", got.Status)
}

func TestClient_GetCampuses(t *testing.T) {
	client := newMockClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Contains(t, r.URL.Path, "campuses")
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(CampusesResponse{
			Campuses: []CampusV1DTO{
				{ID: "1", ShortName: "moscow", FullName: "Moscow", Timezone: "Europe/Moscow"},
				{ID: "2", ShortName: "nsk", FullName: "Novosibirsk", Timezone: "Asia/Novosibirsk"},
			},
		})
	}))
	got, err := client.GetCampuses(context.Background(), "tok")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "moscow", got[0].ShortName)
	assert.Equal(t, "Novosibirsk", got[1].FullName)
}

func TestClient_GetCampuses_apiError(t *testing.T) {
	client := newMockClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	got, err := client.GetCampuses(context.Background(), "tok")
	assert.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "500")
}

func TestClient_GetCampusCoalitions(t *testing.T) {
	campusID := "ff19a3a7-12f5-4332-9582-624519c3eaea"
	client := newMockClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Contains(t, r.URL.Path, "/campuses/"+campusID+"/coalitions")
		assert.Equal(t, "1000", r.URL.Query().Get("limit"))
		assert.Equal(t, "0", r.URL.Query().Get("offset"))
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(CoalitionsResponse{
			Coalitions: []CoalitionV1DTO{
				{CoalitionID: 1, Name: "Blue"},
				{CoalitionID: 2, Name: "Red"},
			},
		})
	}))
	got, err := client.GetCampusCoalitions(context.Background(), "tok", campusID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, int64(1), got[0].CoalitionID)
	assert.Equal(t, "Red", got[1].Name)
}

func TestClient_GetCampusCoalitions_apiError(t *testing.T) {
	client := newMockClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	got, err := client.GetCampusCoalitions(context.Background(), "tok", "ff19a3a7-12f5-4332-9582-624519c3eaea")
	assert.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "500")
}

func newMockClient(handler http.Handler) *Client {
	httpClient := &http.Client{
		Transport: mockRoundTripper{handler: handler},
	}
	return NewClientForTest("http://example.local", "http://example.local", httpClient)
}

type mockRoundTripper struct {
	handler http.Handler
}

func (m mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rr := httptest.NewRecorder()
	m.handler.ServeHTTP(rr, req)
	return rr.Result(), nil
}
