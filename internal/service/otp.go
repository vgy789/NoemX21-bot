package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
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
	db       db.Querier
	rcClient *rocketchat.Client
	cfg      *config.Config
	log      *slog.Logger
}

// NewOTPService creates a new OTP service
func NewOTPService(db db.Querier, rcClient *rocketchat.Client, cfg *config.Config, log *slog.Logger) *OTPService {
	return &OTPService{
		db:       db,
		rcClient: rcClient,
		cfg:      cfg,
		log:      log,
	}
}

// GenerateAndSendOTP generates a 6-digit code and sends it via RocketChat
func (s *OTPService) GenerateAndSendOTP(ctx context.Context, s21Login string, ui fsm.UserInfo) error {
	// 0. Rate limiting check (cooldown 60 seconds)
	lastOTP, err := s.db.GetLastAuthVerificationCode(ctx, pgtype.Text{Valid: true, String: s21Login})
	if err == nil {
		if time.Since(lastOTP.CreatedAt.Time) < 60*time.Second {
			remaining := 60 - int(time.Since(lastOTP.CreatedAt.Time).Seconds())
			return fmt.Errorf("RATE_LIMIT:%d", remaining)
		}
	} else if !strings.Contains(err.Error(), "no rows") {
		s.log.Error("failed to check last OTP", "error", err)
	}

	// 1. Generate 6-digit code
	code, err := s.generateCode()
	if err != nil {
		return fmt.Errorf("failed to generate code: %w", err)
	}

	// Calculate expiration time (5 minutes from now)
	expiresAt := time.Now().Add(5 * time.Minute)

	// 2. Get RocketChat user ID from database (it was verified in previous step)
	regUser, err := s.db.GetRegisteredUserByS21Login(ctx, s21Login)
	if err != nil {
		return fmt.Errorf("failed to get registered user info: %w", err)
	}

	if regUser.RocketchatID == "" {
		return fmt.Errorf("registered user has no rocketchat_id")
	}

	// 3. Delete all previous verification codes for this student (invalidate old codes)
	err = s.db.DeleteAllAuthVerificationCodes(ctx, pgtype.Text{Valid: true, String: s21Login})
	if err != nil && !strings.Contains(err.Error(), "no rows") {
		s.log.Warn("failed to delete old verification codes", "error", err)
		// Continue anyway - not critical
	}

	// 4. Create new verification code in database
	_, err = s.db.CreateAuthVerificationCode(ctx, db.CreateAuthVerificationCodeParams{
		S21Login:  pgtype.Text{Valid: true, String: s21Login},
		Code:      code,
		ExpiresAt: pgtype.Timestamptz{Valid: true, Time: expiresAt},
	})
	if err != nil {
		return fmt.Errorf("failed to create verification code: %w", err)
	}

	// 4. Send OTP via RocketChat using stored ID
	rcUserID := regUser.RocketchatID
	fullName := strings.TrimSpace(fmt.Sprintf("%s %s", ui.FirstName, ui.LastName))
	if fullName == "" {
		fullName = "No Name"
	}

	message := fmt.Sprintf(
		"🔐 *NOEMX21-BOT* | КОД ПОДТВЕРЖДЕНИЯ: *%s*\n\n"+
			"---\n"+
			"Действует: 5 минут\n"+
			"Код запросил пользователь *%s* id: *%d* username: *%s* platform: *%s*\n"+
			"Не передавай код третьим лицам.\n\n",
		code,
		fullName,
		ui.ID,
		ui.Username,
		ui.Platform,
	)
	_, err = s.rcClient.SendDirectMessage(ctx, rcUserID, message)
	if err != nil {
		// Log error but don't fail immediately
		s.log.Error("failed to send OTP via RocketChat", "error", err, "rc_user_id", rcUserID)
		return fmt.Errorf("failed to send message")
	}

	s.log.Info("OTP generated and sent", "student_id", s21Login, "code", code, "expires_at", expiresAt)

	return nil
}

// VerifyOTP verifies the provided code
func (s *OTPService) VerifyOTP(ctx context.Context, telegramUserID int64, code string) (bool, error) {
	// Get the S21 login from context
	s21Login, ok := ctx.Value(fsm.ContextKeyS21Login).(string)
	if !ok {
		return false, fmt.Errorf("S21 login not found in context")
	}

	// Get valid verification code
	var err error
	_, err = s.db.GetValidAuthVerificationCode(ctx, db.GetValidAuthVerificationCodeParams{
		S21Login: pgtype.Text{Valid: true, String: s21Login},
		Code:     code,
	})
	if err != nil {
		if strings.Contains(err.Error(), "no rows") || strings.Contains(err.Error(), "not found") {
			return false, nil
		}
		return false, fmt.Errorf("database error during verification: %w", err)
	}

	// Delete the used code
	if err := s.db.DeleteAuthVerificationCode(ctx, db.DeleteAuthVerificationCodeParams{
		S21Login: pgtype.Text{Valid: true, String: s21Login},
		Code:     code,
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
