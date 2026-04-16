package service

import (
	"context"

	"github.com/vgy789/noemx21-bot/internal/fsm"
)

// OTPProvider defines the interface for OTP verification
type OTPProvider interface {
	// GenerateAndSendOTP generates and sends OTP code to the user
	GenerateAndSendOTP(ctx context.Context, s21Login string, ui fsm.UserInfo) error
	// VerifyOTP verifies the provided code for the user
	VerifyOTP(ctx context.Context, telegramUserID int64, code string) (bool, error)
}

// RealOTPProvider is the production OTP provider (Rocket.Chat/Email via OTPService).
type RealOTPProvider struct {
	*OTPService
}

// NewRealOTPProvider creates a new real OTP provider
func NewRealOTPProvider(otpService *OTPService) *RealOTPProvider {
	return &RealOTPProvider{OTPService: otpService}
}

// GenerateAndSendOTP generates and sends OTP via the channel selected in context.
func (p *RealOTPProvider) GenerateAndSendOTP(ctx context.Context, s21Login string, ui fsm.UserInfo) error {
	return p.generateAndSendOTP(ctx, s21Login, ui)
}

// VerifyOTP verifies OTP code against database
func (p *RealOTPProvider) VerifyOTP(ctx context.Context, telegramUserID int64, code string) (bool, error) {
	return p.verifyOTP(ctx, telegramUserID, code)
}

// MockOTPProvider is the test OTP provider that accepts any code
type MockOTPProvider struct {
	log Logger
}

// Logger is a minimal logging interface
type Logger interface {
	Info(msg string, args ...any)
	Debug(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// NewMockOTPProvider creates a new mock OTP provider for testing
func NewMockOTPProvider(log Logger) *MockOTPProvider {
	return &MockOTPProvider{log: log}
}

// GenerateAndSendOTP simulates OTP generation (no actual sending)
func (p *MockOTPProvider) GenerateAndSendOTP(ctx context.Context, s21Login string, ui fsm.UserInfo) error {
	p.log.Debug("mock OTP: skipping actual code generation and sending", "login", s21Login)
	// In mock mode, we don't actually generate or send anything
	// Any 6-digit code will be accepted during verification
	return nil
}

// VerifyOTP accepts any 6-digit code in mock mode
func (p *MockOTPProvider) VerifyOTP(ctx context.Context, telegramUserID int64, code string) (bool, error) {
	// Accept any 6-digit code
	if len(code) != 6 {
		p.log.Debug("mock OTP: invalid code format", "user_id", telegramUserID, "code", code)
		return false, nil // Match real provider contract: invalid code returns (false, nil)
	}
	p.log.Debug("mock OTP: accepting code", "user_id", telegramUserID, "code", code)
	return true, nil
}
