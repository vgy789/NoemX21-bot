package service

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

// testLogger implements Logger interface for tests
type testLogger struct {
	*slog.Logger
}

func newTestLogger() *testLogger {
	return &testLogger{
		Logger: slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
	}
}

func TestMockOTPProvider_GenerateAndSendOTP(t *testing.T) {
	log := newTestLogger()
	provider := NewMockOTPProvider(log)
	ctx := context.Background()

	// Mock provider should not fail on GenerateAndSendOTP
	err := provider.GenerateAndSendOTP(ctx, "testuser", fsm.UserInfo{ID: 1, Username: "test", Platform: "Telegram"})
	assert.NoError(t, err)
}

func TestMockOTPProvider_VerifyOTP(t *testing.T) {
	log := newTestLogger()
	provider := NewMockOTPProvider(log)
	ctx := context.Background()

	t.Run("valid 6-digit code", func(t *testing.T) {
		valid, err := provider.VerifyOTP(ctx, 123, "123456")
		require.NoError(t, err)
		assert.True(t, valid)
	})

	t.Run("another valid 6-digit code", func(t *testing.T) {
		valid, err := provider.VerifyOTP(ctx, 123, "000000")
		require.NoError(t, err)
		assert.True(t, valid)
	})

	t.Run("code too short", func(t *testing.T) {
		valid, err := provider.VerifyOTP(ctx, 123, "12345")
		require.NoError(t, err)
		assert.False(t, valid)
	})

	t.Run("code too long", func(t *testing.T) {
		valid, err := provider.VerifyOTP(ctx, 123, "1234567")
		require.NoError(t, err)
		assert.False(t, valid)
	})

	t.Run("empty code", func(t *testing.T) {
		valid, err := provider.VerifyOTP(ctx, 123, "")
		require.NoError(t, err)
		assert.False(t, valid)
	})

	t.Run("code with letters", func(t *testing.T) {
		// Mock provider accepts any 6-character code (format check is len == 6)
		valid, err := provider.VerifyOTP(ctx, 123, "abcdef")
		require.NoError(t, err)
		assert.True(t, valid)
	})
}

func TestRealOTPProvider_WrapsOTPService(t *testing.T) {
	// This test just verifies that RealOTPProvider properly wraps OTPService
	// Actual OTP functionality is tested in otp_test.go
	ctrl := newTestLogger()
	otpService := &OTPService{log: ctrl.Logger}
	provider := NewRealOTPProvider(otpService)

	require.NotNil(t, provider)
	assert.NotNil(t, provider.OTPService)
}

func TestMockOTPProvider_AcceptsAnySixDigitCode(t *testing.T) {
	log := newTestLogger()
	provider := NewMockOTPProvider(log)
	ctx := context.Background()

	// Test various 6-digit codes that should all be accepted
	codes := []string{"000000", "111111", "999999", "123456", "654321"}
	for _, code := range codes {
		t.Run("code_"+code, func(t *testing.T) {
			valid, err := provider.VerifyOTP(ctx, 123, code)
			require.NoError(t, err)
			assert.True(t, valid)
		})
	}
}
