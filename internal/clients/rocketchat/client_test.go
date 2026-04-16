package rocketchat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetUserInfo_BaseURLWithTrailingSlash(t *testing.T) {
	client := NewClientWithHTTPClient("http://example.local/api/v1/", " token ", " user ", &http.Client{
		Transport: mockRoundTripper{
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/v1/users.info", r.URL.Path)
				assert.Equal(t, "user", r.Header.Get("X-User-Id"))
				assert.Equal(t, "token", r.Header.Get("X-Auth-Token"))

				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(UserInfoResponse{
					Success: true,
				})
			}),
		},
	})

	got, err := client.GetUserInfo(context.Background(), "john")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, got.Success)
}

type mockRoundTripper struct {
	handler http.Handler
}

func (m mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rr := httptest.NewRecorder()
	m.handler.ServeHTTP(rr, req)
	return rr.Result(), nil
}
