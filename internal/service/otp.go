package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"html/template"
	"math/big"
	"net"
	"net/smtp"
	"os"
	"path/filepath"
	"strconv"
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

// generateAndSendOTP generates a 6-digit code and sends it via selected channel (internal use).
func (s *OTPService) generateAndSendOTP(ctx context.Context, s21Login string, ui fsm.UserInfo) error {
	deliveryMethod := otpDeliveryRocketchat
	if method, ok := ctx.Value(fsm.ContextKeyOTPDeliveryMethod).(string); ok {
		method = strings.ToLower(strings.TrimSpace(method))
		if method == otpDeliveryEmail || method == otpDeliveryRocketchat {
			deliveryMethod = method
		}
	}

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

	switch deliveryMethod {
	case otpDeliveryEmail:
		if err := s.sendOTPViaEmail(ctx, s21Login, code, expiresAt, ui); err != nil {
			return fmt.Errorf("failed to send email: %w", err)
		}
	default:
		if err := s.sendOTPViaRocketChat(ctx, s21Login, code, ui); err != nil {
			return err
		}
	}

	s.log.Info("OTP generated and sent", "student_id", s21Login, "expires_at", expiresAt, "delivery", deliveryMethod)

	return nil
}

const (
	otpDeliveryRocketchat = "rocketchat"
	otpDeliveryEmail      = "email"
)

func (s *OTPService) sendOTPViaRocketChat(ctx context.Context, s21Login, code string, ui fsm.UserInfo) error {
	if s.rcClient == nil {
		return fmt.Errorf("rocketchat client is not configured")
	}

	regUser, err := s.db.GetRegisteredUserByS21Login(ctx, s21Login)
	if err != nil {
		return fmt.Errorf("failed to get registered user info: %w", err)
	}
	if regUser.RocketchatID == "" {
		return fmt.Errorf("registered user has no rocketchat_id")
	}

	escape := func(raw string) string {
		r := strings.NewReplacer(
			"*", "\\*",
			"_", "\\_",
			"`", "\\`",
			"~", "\\~",
		)
		return r.Replace(raw)
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
	_, err = s.rcClient.SendDirectMessage(ctx, regUser.RocketchatID, message)
	if err != nil {
		// Log error but don't fail immediately
		s.log.Error("failed to send OTP via RocketChat", "error", err, "rc_user_id", regUser.RocketchatID)
		return fmt.Errorf("failed to send message")
	}

	return nil
}

type otpEmailTemplateData struct {
	Code            string
	S21Login        string
	TargetEmail     string
	ExpiresInMin    int
	TelegramUserID  int64
	TelegramUser    string
	TelegramName    string
	TelegramChannel string
	RequestedAt     string
}

func (s *OTPService) sendOTPViaEmail(ctx context.Context, s21Login, code string, expiresAt time.Time, ui fsm.UserInfo) error {
	if !s.cfg.EmailOTP.Enabled {
		return fmt.Errorf("email otp is disabled")
	}

	normalizedLogin := strings.ToLower(strings.TrimSpace(s21Login))
	host := strings.TrimSpace(s.cfg.EmailOTP.SMTPHost)
	username := strings.TrimSpace(s.cfg.EmailOTP.SMTPUsername)
	password := s.cfg.EmailOTP.SMTPPassword.Expose()
	from := strings.TrimSpace(s.cfg.EmailOTP.From)
	if from == "" {
		from = username
	}
	if host == "" || username == "" || password == "" || from == "" {
		return fmt.Errorf("email otp smtp settings are incomplete")
	}

	if err := s.ensureRegisteredUserForEmail(ctx, normalizedLogin); err != nil {
		return err
	}

	to := fmt.Sprintf("%s@student.21-school.ru", normalizedLogin)
	if testTo := strings.TrimSpace(s.cfg.EmailOTP.TestTo); testTo != "" {
		s.log.Warn("OTP_EMAIL_TEST_TO is set, overriding recipient", "original_target", to, "test_target", testTo)
		to = testTo
	}

	timeout := s.cfg.EmailOTP.SMTPTimeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}

	subject := strings.TrimSpace(s.cfg.EmailOTP.Subject)
	if subject == "" {
		subject = "NOEMX21-BOT | Verification code"
	}

	body, err := s.renderOTPEmailBody(code, normalizedLogin, to, ui)
	if err != nil {
		return err
	}

	s.log.Info("sending otp via email", "login", normalizedLogin, "target", to, "smtp_host", host, "smtp_port", s.cfg.EmailOTP.SMTPPort, "smtp_timeout", timeout)
	msg := buildHTMLMessage(from, to, subject, body)
	if err := s.sendSMTPMail(host, s.cfg.EmailOTP.SMTPPort, username, password, from, to, msg, timeout); err != nil {
		return fmt.Errorf("smtp send failed: %w", err)
	}

	s.log.Info("otp sent via email", "login", s21Login, "target", to, "expires_at", expiresAt)
	return nil
}

func (s *OTPService) sendSMTPMail(host string, port int, username, password, from, to, msg string, timeout time.Duration) error {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	implicitTLS := port == 465
	start := time.Now()

	var (
		conn net.Conn
		err  error
	)

	dialer := &net.Dialer{Timeout: timeout}
	if implicitTLS {
		s.log.Debug("smtp stage", "stage", "dial_tls", "addr", addr)
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ //nolint:gosec // mail host is configured explicitly via env
			ServerName: host,
			MinVersion: tls.VersionTLS12,
		})
	} else {
		s.log.Debug("smtp stage", "stage", "dial_tcp", "addr", addr)
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(timeout))

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("new smtp client: %w", err)
	}
	defer client.Close()

	if !implicitTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			s.log.Debug("smtp stage", "stage", "starttls", "addr", addr)
			if err := client.StartTLS(&tls.Config{ //nolint:gosec // mail host is configured explicitly via env
				ServerName: host,
				MinVersion: tls.VersionTLS12,
			}); err != nil {
				return fmt.Errorf("starttls: %w", err)
			}
		} else {
			s.log.Warn("smtp STARTTLS extension not advertised", "addr", addr)
		}
	}

	if username != "" || password != "" {
		if ok, _ := client.Extension("AUTH"); !ok {
			return fmt.Errorf("auth extension not advertised by smtp server")
		}
		s.log.Debug("smtp stage", "stage", "auth", "addr", addr)
		if err := client.Auth(smtp.PlainAuth("", username, password, host)); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}

	s.log.Debug("smtp stage", "stage", "mail_from", "from", from)
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}

	s.log.Debug("smtp stage", "stage", "rcpt_to", "to", to)
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("rcpt to: %w", err)
	}

	s.log.Debug("smtp stage", "stage", "data_write")
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := writer.Write([]byte(msg)); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close message body: %w", err)
	}

	if err := client.Quit(); err != nil {
		return fmt.Errorf("quit: %w", err)
	}
	s.log.Debug("smtp stage", "stage", "done", "elapsed", time.Since(start))

	return nil
}

func (s *OTPService) ensureRegisteredUserForEmail(ctx context.Context, s21Login string) error {
	_, err := s.db.GetRegisteredUserByS21Login(ctx, s21Login)
	if err == nil {
		return nil
	}
	if !strings.Contains(err.Error(), "no rows") {
		return fmt.Errorf("failed to check registered user for email otp: %w", err)
	}

	syntheticRocketID := fmt.Sprintf("email:%s", s21Login)
	_, err = s.db.UpsertRegisteredUser(ctx, db.UpsertRegisteredUserParams{
		S21Login:           s21Login,
		RocketchatID:       syntheticRocketID,
		Timezone:           "UTC",
		AlternativeContact: pgtype.Text{Valid: false},
		HasCoffeeBan:       pgtype.Bool{Valid: false},
	})
	if err != nil {
		return fmt.Errorf("failed to create registered user for email otp: %w", err)
	}

	return nil
}

func (s *OTPService) renderOTPEmailBody(code, s21Login, targetEmail string, ui fsm.UserInfo) (string, error) {
	templatePath := strings.TrimSpace(s.cfg.EmailOTP.TemplatePath)
	if templatePath == "" {
		return "", fmt.Errorf("email template path is empty")
	}

	rawTemplate, err := os.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("read email template: %w", err)
	}

	tmpl, err := template.New(filepath.Base(templatePath)).Parse(string(rawTemplate))
	if err != nil {
		return "", fmt.Errorf("parse email template: %w", err)
	}

	fullName := strings.TrimSpace(fmt.Sprintf("%s %s", ui.FirstName, ui.LastName))
	if fullName == "" {
		fullName = "Unknown"
	}
	telegramUsername := strings.TrimSpace(ui.Username)
	if telegramUsername == "" {
		telegramUsername = "none"
	}
	telegramPlatform := strings.TrimSpace(ui.Platform)
	if telegramPlatform == "" {
		telegramPlatform = "Telegram"
	}

	data := otpEmailTemplateData{
		Code:            code,
		S21Login:        strings.ToLower(strings.TrimSpace(s21Login)),
		TargetEmail:     targetEmail,
		ExpiresInMin:    5,
		TelegramUserID:  ui.ID,
		TelegramUser:    telegramUsername,
		TelegramName:    fullName,
		TelegramChannel: telegramPlatform,
		RequestedAt:     time.Now().UTC().Format(time.RFC3339),
	}

	var body bytes.Buffer
	if err := tmpl.Execute(&body, data); err != nil {
		return "", fmt.Errorf("execute email template: %w", err)
	}

	return body.String(), nil
}

func buildHTMLMessage(from, to, subject, body string) string {
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("From: %s\r\n", from))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", to))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body)
	return msg.String()
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
