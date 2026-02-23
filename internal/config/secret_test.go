package config

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSecret(t *testing.T) {
	s := Secret("password123")

	// Test String method
	assert.Equal(t, "[REDACTED]", s.String())

	// Test LogValue method
	logValue := s.LogValue()
	assert.Equal(t, "[REDACTED]", logValue.String())

	// Test Expose method
	assert.Equal(t, "password123", s.Expose())
}

func TestSecret_LogValue(t *testing.T) {
	s := Secret("my-secret-token")
	val := s.LogValue()
	assert.Equal(t, slog.KindString, val.Kind())
	assert.Equal(t, "[REDACTED]", val.String())
}

func TestRedactString(t *testing.T) {
	msg := "failed to call https://api.telegram.org/bot123456:ABCDEF/getMe"
	redacted := RedactString(msg, Secret("123456:ABCDEF"))
	assert.Equal(t, "failed to call https://api.telegram.org/bot[REDACTED]/getMe", redacted)

	// Empty secret should be ignored.
	assert.Equal(t, msg, RedactString(msg, Secret("")))
}
