package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/database/db"
)

type ApiKeyService struct {
	queries db.Querier
}

func NewApiKeyService(queries db.Querier) *ApiKeyService {
	return &ApiKeyService{queries: queries}
}

func nullableInt8(value int64) pgtype.Int8 {
	return pgtype.Int8{Int64: value, Valid: true}
}

// GenerateApiKey generates a new API key for the user, revoking old ones.
// Returns the raw key (to show to user) and error.
func (s *ApiKeyService) GenerateApiKey(ctx context.Context, userAccountID int64) (string, error) {
	ua, err := s.queries.GetUserAccountByID(ctx, userAccountID)
	if err != nil {
		return "", fmt.Errorf("failed to load user account: %w", err)
	}

	var telegramUserID pgtype.Int8
	if ua.Platform == db.EnumPlatformTelegram {
		if parsedID, err := strconv.ParseInt(ua.ExternalID, 10, 64); err == nil {
			telegramUserID = nullableInt8(parsedID)
		}
	}

	principal, err := s.queries.EnsurePersonalApiPrincipal(ctx, db.EnsurePersonalApiPrincipalParams{
		DisplayName:    "Personal key for " + ua.S21Login,
		TelegramUserID: telegramUserID,
		UserAccountID:  nullableInt8(userAccountID),
		Scopes:         []string{"self.read"},
	})
	if err != nil {
		return "", fmt.Errorf("failed to ensure personal api principal: %w", err)
	}

	// 1. Revoke old keys
	if err := s.queries.RevokeOldApiKeys(ctx, nullableInt8(userAccountID)); err != nil {
		return "", fmt.Errorf("failed to revoke old keys: %w", err)
	}

	// 2. Generate random bytes
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	randomPart := hex.EncodeToString(bytes)
	rawKey := "noemx_sk_" + randomPart

	// 3. Hash the key
	hash := sha256.Sum256([]byte(rawKey))
	keyHash := hex.EncodeToString(hash[:])

	// 4. Store in DB
	// Prefix is the first 8 chars of random part for identification (optional, but good for "ends with...")
	prefix := "noemx_sk_" + randomPart[:4]

	_, err = s.queries.CreateApiKey(ctx, db.CreateApiKeyParams{
		ApiPrincipalID: principal.ID,
		KeyHash:        keyHash,
		Prefix:         prefix,
		// ExpiresAt is null (indefinite)
	})
	if err != nil {
		return "", fmt.Errorf("failed to create api key: %w", err)
	}

	return rawKey, nil
}

// ValidateApiKey checks if the raw key is valid.
// Returns the user account ID and valid status.
func (s *ApiKeyService) ValidateApiKey(ctx context.Context, rawKey string) (*db.ApiKey, bool, error) {
	if !strings.HasPrefix(rawKey, "noemx_sk_") {
		return nil, false, nil
	}

	hash := sha256.Sum256([]byte(rawKey))
	keyHash := hex.EncodeToString(hash[:])

	apiKey, err := s.queries.GetApiKeyByHash(ctx, keyHash)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return nil, false, nil
		}
		return nil, false, err
	}

	return &apiKey, true, nil
}

// GetActiveApiKey returns the prefix of the active API key for the user.
// Returns empty string if no active key.
func (s *ApiKeyService) GetActiveApiKey(ctx context.Context, userAccountID int64) (string, error) {
	key, err := s.queries.GetActiveApiKey(ctx, nullableInt8(userAccountID))
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return "", nil
		}
		return "", err
	}
	return key.Prefix + "...", nil
}
