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

// generateAndSendOTP generates a 6-digit code and sends it via RocketChat (internal use)
func (s *OTPService) generateAndSendOTP(ctx context.Context, s21Login string, ui fsm.UserInfo) error {
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
	escape := func(s string) string {
		r := strings.NewReplacer(
			"*", "\\*",
			"_", "\\_",
			"`", "\\`",
			"~", "\\~",
		)
		return r.Replace(s)
	}

	fullName := strings.TrimSpace(fmt.Sprintf("%s %s", ui.FirstName, ui.LastName))
	if fullName == "" {
		fullName = "No Name"
	}

	fullNameEscaped := escape(fullName)
	usernameEscaped := escape(ui.Username)
	platformEscaped := escape(ui.Platform)

	message := fmt.Sprintf(
		"🔐 *NOEMX21-BOT* | КОД ПОДТВЕРЖДЕНИЯ: *%s*\n\n"+
			"---\n"+
			"Действует: 5 минут\n"+
			"Код запросил пользователь *%s* id: *%d* username: *%s* platform: *%s*\n"+
			"Не передавай код третьим лицам.\n\n",
		code,
		fullNameEscaped,
		ui.ID,
		usernameEscaped,
		platformEscaped,
	)
	_, err = s.rcClient.SendDirectMessage(ctx, rcUserID, message)
	if err != nil {
		// Log error but don't fail immediately
		s.log.Error("failed to send OTP via RocketChat", "error", err, "rc_user_id", rcUserID)
		return fmt.Errorf("failed to send message")
	}

	s.log.Info("OTP generated and sent", "student_id", s21Login, "expires_at", expiresAt)

	return nil
}

// verifyOTP verifies the provided code (internal use)
func (s *OTPService) verifyOTP(ctx context.Context, telegramUserID int64, code string) (bool, error) {
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

	// Delete the used code and reset rate limiter state
	s.cleanupAfterSuccess(ctx, telegramUserID, code)

	return true, nil
}

// CleanupExpiredCodes removes expired verification codes
func (s *OTPService) CleanupExpiredCodes(ctx context.Context) error {
	return s.db.DeleteExpiredAuthVerificationCodes(ctx)
}

// cleanupAfterSuccess removes used code and clears rate-limit counters.
func (s *OTPService) cleanupAfterSuccess(ctx context.Context, telegramUserID int64, code string) {
	// Best-effort: delete code
	s21Login, _ := ctx.Value(fsm.ContextKeyS21Login).(string)
	if err := s.db.DeleteAuthVerificationCode(ctx, db.DeleteAuthVerificationCodeParams{
		S21Login: pgtype.Text{Valid: true, String: s21Login},
		Code:     code,
	}); err != nil {
		s.log.Warn("failed to delete verification code after success", "error", err)
	}

	// Reset rate limiter for this user to allow fresh attempts next time
	rl := GetRateLimiter()
	rl.Reset(telegramUserID)
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
