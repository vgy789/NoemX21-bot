//go:build identity

package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
)

const (
	identitySessionTTL = 10 * time.Minute
	identityCodeTTL    = 2 * time.Minute
	maxIdentityClients = 3
	maxRedirectURIs    = 5
)

var (
	ErrIdentityInvalidRequest       = errors.New("invalid_request")
	ErrIdentityInvalidClient        = errors.New("invalid_client")
	ErrIdentityInvalidRedirectURI   = errors.New("invalid_redirect_uri")
	ErrIdentityInvalidState         = errors.New("invalid_state")
	ErrIdentityInvalidCodeChallenge = errors.New("invalid_code_challenge")
	ErrIdentitySessionNotFound      = errors.New("session_not_found")
	ErrIdentitySessionExpired       = errors.New("session_expired")
	ErrIdentitySessionNotPending    = errors.New("session_not_pending")
	ErrIdentityAccessDenied         = errors.New("access_denied")
	ErrIdentityInvalidCode          = errors.New("invalid_code")
	ErrIdentityCodeConsumed         = errors.New("code_consumed")
	ErrIdentityBotUsernameMissing   = errors.New("bot_username_missing")
	ErrIdentityClientLimit          = errors.New("client_limit")
	ErrIdentityRedirectLimit        = errors.New("redirect_limit")
)

type IdentityClientService struct {
	queries db.Querier
}

type IdentityClientCreateResult struct {
	Client db.IdentityClient
	URI    db.IdentityClientRedirectUri
	RawURL string
}

func NewIdentityClientService(queries db.Querier) *IdentityClientService {
	return &IdentityClientService{queries: queries}
}

func (s *IdentityClientService) CreateClient(ctx context.Context, ownerUserAccountID int64, name string, redirectURI string) (IdentityClientCreateResult, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return IdentityClientCreateResult{}, ErrIdentityInvalidRequest
	}
	cleanRedirectURI, err := normalizeRedirectURI(redirectURI)
	if err != nil {
		return IdentityClientCreateResult{}, err
	}
	count, err := s.queries.CountActiveIdentityClientsByOwner(ctx, ownerUserAccountID)
	if err != nil {
		return IdentityClientCreateResult{}, fmt.Errorf("count identity clients: %w", err)
	}
	if count >= maxIdentityClients {
		return IdentityClientCreateResult{}, ErrIdentityClientLimit
	}
	clientID, err := randomToken("noemx_cli_", 12)
	if err != nil {
		return IdentityClientCreateResult{}, err
	}
	client, err := s.queries.CreateIdentityClient(ctx, db.CreateIdentityClientParams{
		OwnerUserAccountID: ownerUserAccountID,
		ClientID:           clientID,
		Name:               name,
	})
	if err != nil {
		return IdentityClientCreateResult{}, fmt.Errorf("create identity client: %w", err)
	}
	uri, err := s.queries.AddIdentityClientRedirectURI(ctx, db.AddIdentityClientRedirectURIParams{
		ClientDbID:  client.ID,
		RedirectUri: cleanRedirectURI,
	})
	if err != nil {
		return IdentityClientCreateResult{}, fmt.Errorf("add redirect uri: %w", err)
	}
	return IdentityClientCreateResult{Client: client, URI: uri, RawURL: cleanRedirectURI}, nil
}

func (s *IdentityClientService) AddRedirectURI(ctx context.Context, ownerUserAccountID int64, clientID string, redirectURI string) (db.IdentityClientRedirectUri, error) {
	client, err := s.queries.GetActiveIdentityClientByClientID(ctx, strings.TrimSpace(clientID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.IdentityClientRedirectUri{}, ErrIdentityInvalidClient
		}
		return db.IdentityClientRedirectUri{}, err
	}
	if client.OwnerUserAccountID != ownerUserAccountID {
		return db.IdentityClientRedirectUri{}, ErrIdentityInvalidClient
	}
	count, err := s.queries.CountIdentityClientRedirectURIs(ctx, client.ID)
	if err != nil {
		return db.IdentityClientRedirectUri{}, err
	}
	if count >= maxRedirectURIs {
		return db.IdentityClientRedirectUri{}, ErrIdentityRedirectLimit
	}
	cleanRedirectURI, err := normalizeRedirectURI(redirectURI)
	if err != nil {
		return db.IdentityClientRedirectUri{}, err
	}
	return s.queries.AddIdentityClientRedirectURI(ctx, db.AddIdentityClientRedirectURIParams{
		ClientDbID:  client.ID,
		RedirectUri: cleanRedirectURI,
	})
}

func (s *IdentityClientService) ListClients(ctx context.Context, ownerUserAccountID int64) ([]db.IdentityClient, error) {
	return s.queries.ListIdentityClientsByOwner(ctx, ownerUserAccountID)
}

type IdentityAuthService struct {
	queries db.Querier
	pool    *pgxpool.Pool
	cfg     *config.Config
}

type IdentityAuthorizeRequest struct {
	ClientID            string
	RedirectURI         string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
}

type IdentityAuthorizeResult struct {
	SessionID   string
	TelegramURL string
	ExpiresAt   time.Time
}

type IdentityExchangeRequest struct {
	ClientID     string `json:"client_id"`
	Code         string `json:"code"`
	RedirectURI  string `json:"redirect_uri"`
	CodeVerifier string `json:"code_verifier"`
}

type IdentityExchangeResult struct {
	User       IdentityExchangeUser `json:"user"`
	VerifiedAt time.Time            `json:"verified_at"`
}

type IdentityExchangeUser struct {
	ID       string `json:"id"`
	Login    string `json:"login"`
	Verified bool   `json:"verified"`
}

type IdentityApprovalResult struct {
	ReturnURL string
}

func NewIdentityAuthService(queries db.Querier, pool *pgxpool.Pool, cfg *config.Config) *IdentityAuthService {
	return &IdentityAuthService{queries: queries, pool: pool, cfg: cfg}
}

func (s *IdentityAuthService) CreateAuthorizeSession(ctx context.Context, req IdentityAuthorizeRequest) (IdentityAuthorizeResult, error) {
	if strings.TrimSpace(s.cfg.Telegram.Username) == "" {
		return IdentityAuthorizeResult{}, ErrIdentityBotUsernameMissing
	}
	state := strings.TrimSpace(req.State)
	if state == "" || len(state) > 256 {
		return IdentityAuthorizeResult{}, ErrIdentityInvalidState
	}
	redirectURI, err := normalizeRedirectURI(req.RedirectURI)
	if err != nil {
		return IdentityAuthorizeResult{}, err
	}
	method := strings.TrimSpace(req.CodeChallengeMethod)
	if method == "" {
		method = "S256"
	}
	if method != "S256" || strings.TrimSpace(req.CodeChallenge) == "" || len(req.CodeChallenge) > 128 {
		return IdentityAuthorizeResult{}, ErrIdentityInvalidCodeChallenge
	}
	client, err := s.queries.GetIdentityClientWithRedirectURI(ctx, db.GetIdentityClientWithRedirectURIParams{
		ClientID:    strings.TrimSpace(req.ClientID),
		RedirectUri: redirectURI,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IdentityAuthorizeResult{}, ErrIdentityInvalidClient
		}
		return IdentityAuthorizeResult{}, fmt.Errorf("get identity client: %w", err)
	}
	sessionID, err := randomToken("ias_", 16)
	if err != nil {
		return IdentityAuthorizeResult{}, err
	}
	expiresAt := time.Now().Add(identitySessionTTL)
	_, err = s.queries.CreateIdentityAuthSession(ctx, db.CreateIdentityAuthSessionParams{
		ID:                  sessionID,
		ClientDbID:          client.ID,
		RedirectUri:         redirectURI,
		State:               state,
		CodeChallenge:       strings.TrimSpace(req.CodeChallenge),
		CodeChallengeMethod: method,
		Status:              "pending",
		ExpiresAt:           pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
	if err != nil {
		return IdentityAuthorizeResult{}, fmt.Errorf("create identity auth session: %w", err)
	}
	botUsername := strings.TrimPrefix(strings.TrimSpace(s.cfg.Telegram.Username), "@")
	return IdentityAuthorizeResult{
		SessionID:   sessionID,
		TelegramURL: fmt.Sprintf("https://t.me/%s?start=identity_%s", botUsername, sessionID),
		ExpiresAt:   expiresAt,
	}, nil
}

func (s *IdentityAuthService) GetSession(ctx context.Context, sessionID string) (db.GetIdentityAuthSessionRow, error) {
	row, err := s.queries.GetIdentityAuthSession(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.GetIdentityAuthSessionRow{}, ErrIdentitySessionNotFound
		}
		return db.GetIdentityAuthSessionRow{}, err
	}
	if row.ExpiresAt.Valid && row.ExpiresAt.Time.Before(time.Now()) {
		return row, ErrIdentitySessionExpired
	}
	return row, nil
}

func (s *IdentityAuthService) ApproveFromTelegram(ctx context.Context, sessionID string, telegramUserID int64) (IdentityApprovalResult, error) {
	session, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return IdentityApprovalResult{}, err
	}
	if session.Status != "pending" {
		return IdentityApprovalResult{}, ErrIdentitySessionNotPending
	}
	account, err := s.queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: fmt.Sprintf("%d", telegramUserID),
	})
	if err != nil {
		return IdentityApprovalResult{}, fmt.Errorf("telegram account not found: %w", err)
	}
	rows, err := s.queries.ApproveIdentityAuthSession(ctx, db.ApproveIdentityAuthSessionParams{
		ID:            sessionID,
		UserAccountID: pgtype.Int8{Int64: account.ID, Valid: true},
	})
	if err != nil {
		return IdentityApprovalResult{}, err
	}
	if rows != 1 {
		return IdentityApprovalResult{}, ErrIdentitySessionNotPending
	}
	code, err := randomToken("iac_", 24)
	if err != nil {
		return IdentityApprovalResult{}, err
	}
	_, err = s.queries.CreateIdentityAuthCode(ctx, db.CreateIdentityAuthCodeParams{
		SessionID: sessionID,
		CodeHash:  sha256Hex(code),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(identityCodeTTL), Valid: true},
	})
	if err != nil {
		return IdentityApprovalResult{}, err
	}
	_, _ = s.queries.MarkIdentityAuthSessionCodeIssued(ctx, sessionID)
	_ = s.queries.InsertIdentityAudit(ctx, db.InsertIdentityAuditParams{
		ClientDbID:    pgtype.Int8{Int64: session.ClientDbID, Valid: true},
		UserAccountID: pgtype.Int8{Int64: account.ID, Valid: true},
		EventType:     "approved",
	})
	return IdentityApprovalResult{ReturnURL: withQuery(session.RedirectUri, map[string]string{
		"code":  code,
		"state": session.State,
	})}, nil
}

func (s *IdentityAuthService) DenyFromTelegram(ctx context.Context, sessionID string) (IdentityApprovalResult, error) {
	session, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return IdentityApprovalResult{}, err
	}
	rows, err := s.queries.DenyIdentityAuthSession(ctx, sessionID)
	if err != nil {
		return IdentityApprovalResult{}, err
	}
	if rows != 1 {
		return IdentityApprovalResult{}, ErrIdentitySessionNotPending
	}
	_ = s.queries.InsertIdentityAudit(ctx, db.InsertIdentityAuditParams{
		ClientDbID: pgtype.Int8{Int64: session.ClientDbID, Valid: true},
		EventType:  "denied",
	})
	return IdentityApprovalResult{ReturnURL: withQuery(session.RedirectUri, map[string]string{
		"error": "access_denied",
		"state": session.State,
	})}, nil
}

func (s *IdentityAuthService) Exchange(ctx context.Context, req IdentityExchangeRequest) (IdentityExchangeResult, error) {
	redirectURI, err := normalizeRedirectURI(req.RedirectURI)
	if err != nil {
		return IdentityExchangeResult{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return IdentityExchangeResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	codeRow, err := q.GetIdentityAuthCodeForUpdate(ctx, sha256Hex(strings.TrimSpace(req.Code)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IdentityExchangeResult{}, ErrIdentityInvalidCode
		}
		return IdentityExchangeResult{}, err
	}
	if codeRow.ConsumedAt.Valid {
		return IdentityExchangeResult{}, ErrIdentityCodeConsumed
	}
	if !codeRow.ExpiresAt.Valid || codeRow.ExpiresAt.Time.Before(time.Now()) {
		return IdentityExchangeResult{}, ErrIdentityInvalidCode
	}
	client, err := q.GetActiveIdentityClientByClientID(ctx, strings.TrimSpace(req.ClientID))
	if err != nil {
		return IdentityExchangeResult{}, ErrIdentityInvalidClient
	}
	if client.ID != codeRow.SessionClientDbID {
		return IdentityExchangeResult{}, ErrIdentityInvalidClient
	}
	if redirectURI != codeRow.SessionRedirectUri {
		return IdentityExchangeResult{}, ErrIdentityInvalidRedirectURI
	}
	if !validatePKCES256(req.CodeVerifier, codeRow.SessionCodeChallenge) {
		return IdentityExchangeResult{}, ErrIdentityInvalidCodeChallenge
	}
	if !codeRow.SessionUserAccountID.Valid {
		return IdentityExchangeResult{}, ErrIdentityInvalidCode
	}
	rows, err := q.ConsumeIdentityAuthCode(ctx, codeRow.ID)
	if err != nil {
		return IdentityExchangeResult{}, err
	}
	if rows != 1 {
		return IdentityExchangeResult{}, ErrIdentityCodeConsumed
	}
	account, err := q.GetUserAccountByID(ctx, codeRow.SessionUserAccountID.Int64)
	if err != nil {
		return IdentityExchangeResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return IdentityExchangeResult{}, err
	}
	return IdentityExchangeResult{
		User: IdentityExchangeUser{
			ID:       fmt.Sprintf("usr_%d", account.ID),
			Login:    account.S21Login,
			Verified: true,
		},
		VerifiedAt: time.Now(),
	}, nil
}

func normalizeRedirectURI(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" || u.Fragment != "" {
		return "", ErrIdentityInvalidRedirectURI
	}
	if u.Scheme != "https" && (u.Scheme != "http" || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1")) {
		return "", ErrIdentityInvalidRedirectURI
	}
	return u.String(), nil
}

func validatePKCES256(verifier string, challenge string) bool {
	verifier = strings.TrimSpace(verifier)
	challenge = strings.TrimSpace(challenge)
	if verifier == "" || challenge == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]) == challenge
}

func withQuery(raw string, values map[string]string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	for k, v := range values {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
