package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

// OTPService handles OTP verification codes
type OTPService struct {
	db       *db.Queries
	rcClient *rocketchat.Client
	cfg      *config.Config
	log      *slog.Logger
}

// NewOTPService creates a new OTP service
func NewOTPService(db *db.Queries, rcClient *rocketchat.Client, cfg *config.Config, log *slog.Logger) *OTPService {
	return &OTPService{
		db:       db,
		rcClient: rcClient,
		cfg:      cfg,
		log:      log,
	}
}

// GenerateAndSendOTP generates a 6-digit code and sends it via RocketChat
func (s *OTPService) GenerateAndSendOTP(ctx context.Context, telegramUserID int64, s21Login string) error {
	// Generate 6-digit code
	code, err := s.generateCode()
	if err != nil {
		return fmt.Errorf("failed to generate code: %w", err)
	}

	// Calculate expiration time (5 minutes from now)
	expiresAt := time.Now().Add(5 * time.Minute)

	// Get RocketChat user ID for the student
	student, err := s.db.GetStudentByS21Login(ctx, s21Login)
	if err != nil {
		return fmt.Errorf("failed to get student info: %w", err)
	}

	// Create verification code in database
	_, err = s.db.CreateAuthVerificationCode(ctx, db.CreateAuthVerificationCodeParams{
		StudentID: pgtype.Text{Valid: true, String: s21Login},
		Code:      code,
		ExpiresAt: pgtype.Timestamptz{Valid: true, Time: expiresAt},
	})
	if err != nil {
		return fmt.Errorf("failed to create verification code: %w", err)
	}

	// Send OTP via RocketChat
	rcUserID := student.RocketchatID
	message := fmt.Sprintf("🔐 Твой код подтверждения: %s\n\nСрок действия: 5 минут", code)

	_, err = s.rcClient.SendDirectMessage(ctx, rcUserID, message)
	if err != nil {
		// Log error but don't fail - code is still in DB
		s.log.Error("failed to send OTP via RocketChat", "error", err, "rc_user_id", rcUserID)
	}

	s.log.Info("OTP generated and sent", "student_id", s21Login, "code", code, "expires_at", expiresAt)

	return nil
}

// VerifyOTP verifies the provided code
func (s *OTPService) VerifyOTP(ctx context.Context, telegramUserID int64, code string) (bool, error) {
	// Get the student from telegram user ID (need to map telegram user to student)
	// For now, we'll use the context to get the student ID
	studentID, ok := ctx.Value(fsm.ContextKeyStudentID).(string)
	if !ok {
		return false, fmt.Errorf("student ID not found in context")
	}

	// Get valid verification code
	var err error
	_, err = s.db.GetValidAuthVerificationCode(ctx, db.GetValidAuthVerificationCodeParams{
		StudentID: pgtype.Text{Valid: true, String: studentID},
		Code:      code,
	})
	if err != nil {
		return false, fmt.Errorf("invalid or expired code")
	}

	// Delete the used code
	if err := s.db.DeleteAuthVerificationCode(ctx, db.DeleteAuthVerificationCodeParams{
		StudentID: pgtype.Text{Valid: true, String: studentID},
		Code:      code,
	}); err != nil {
		s.log.Error("failed to delete used verification code", "error", err)
	}

	return true, nil
}

// CleanupExpiredCodes removes expired verification codes
func (s *OTPService) CleanupExpiredCodes(ctx context.Context) error {
	return s.db.DeleteExpiredAuthVerificationCodes(ctx)
}

// generateCode generates a random 6-digit code
func (s *OTPService) generateCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}

	code := fmt.Sprintf("%06d", n.Int64())
	return code, nil
}

// SendOTPToRocketChat sends OTP directly to RocketChat (for testing)
func (s *OTPService) SendOTPToRocketChat(ctx context.Context, rcUserID, code string) error {
	message := fmt.Sprintf("🔐 Твой код подтверждения: %s\n\nСрок действия: 5 минут", code)
	_, err := s.rcClient.SendDirectMessage(ctx, rcUserID, message)
	return err
}
