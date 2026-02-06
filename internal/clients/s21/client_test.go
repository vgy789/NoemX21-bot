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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "openid-connect/token")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(authResp)
	}))
	defer server.Close()

	client := NewClientForTest(server.URL, server.URL, server.Client())
	got, err := client.Auth(context.Background(), "user", "pass")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "test-token", got.AccessToken)
	assert.Equal(t, 300, got.ExpiresIn)
}

func TestClient_Auth_failStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("invalid credentials"))
	}))
	defer server.Close()

	client := NewClientForTest(server.URL, server.URL, server.Client())
	got, err := client.Auth(context.Background(), "user", "wrong")
	assert.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "401")
}

func TestClient_GetParticipant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Contains(t, r.URL.Path, "participants/")
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ParticipantV1DTO{
			Login:        "testuser",
			ParallelName: "Core program",
			Status:       "ACTIVE",
		})
	}))
	defer server.Close()

	client := NewClientForTest(server.URL, "", server.Client())
	got, err := client.GetParticipant(context.Background(), "tok", "testuser")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "testuser", got.Login)
	assert.Equal(t, "Core program", got.ParallelName)
	assert.Equal(t, "ACTIVE", got.Status)
}

func TestClient_GetParticipant_apiError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"NOT_FOUND"}`))
	}))
	defer server.Close()

	client := NewClientForTest(server.URL, "", server.Client())
	got, err := client.GetParticipant(context.Background(), "tok", "unknown")
	assert.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "404")
}
func TestClient_GetParticipant_404InBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// The quirk: 200 OK but body says 404
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"status": 404,
			"code": "NOT_FOUND",
			"message": "Not found user by login maslenok"
		}`))
	}))
	defer server.Close()

	client := NewClientForTest(server.URL, "", server.Client())
	got, err := client.GetParticipant(context.Background(), "tok", "maslenok")
	assert.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "API body error: status 404")
}

func TestClient_GetParticipant_StatusAsInt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"login": "user",
			"status": 200,
			"parallelName": "Core program"
		}`))
	}))
	defer server.Close()

	client := NewClientForTest(server.URL, "", server.Client())
	got, err := client.GetParticipant(context.Background(), "tok", "user")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, float64(200), got.Status)
}
