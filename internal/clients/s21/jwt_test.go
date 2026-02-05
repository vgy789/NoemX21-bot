package s21

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAccessTokenClaims(t *testing.T) {
	exp := time.Unix(1730000000, 0).UTC()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(exp),
		IssuedAt:  jwt.NewNumericDate(exp.Add(-1 * time.Hour)),
	})
	raw, err := token.SignedString([]byte("test-secret"))
	require.NoError(t, err)

	claims, err := ParseAccessTokenClaims(raw)
	require.NoError(t, err)
	require.NotNil(t, claims.ExpiresAt)
	assert.True(t, claims.ExpiresAt.Time.Equal(exp))
}

func TestAccessTokenExpiry(t *testing.T) {
	exp := time.Unix(1730000000, 0).UTC()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(exp),
	})
	raw, err := token.SignedString([]byte("test-secret"))
	require.NoError(t, err)

	got, ok := AccessTokenExpiry(raw)
	assert.True(t, ok)
	assert.True(t, got.Equal(exp))
}

func TestAccessTokenExpiry_InvalidToken(t *testing.T) {
	_, ok := AccessTokenExpiry("not-a-jwt")
	assert.False(t, ok)
	_, ok = AccessTokenExpiry("a.b.c")
	assert.False(t, ok)
}
