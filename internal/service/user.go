package service

//go:generate mockgen -source=user.go -destination=mock/user_mock.go -package=mock

import (
	"context"
	"fmt"

	"github.com/vgy789/noemx21-bot/internal/database/db"
)

// UserProfile represents the domain model for a user profile.
type UserProfile struct {
	Login         string
	CampusName    string
	CoalitionName string
	Level         int32
	Exp           int32
	PRP           int32
	CRP           int32
	Coins         int32
}

// UserService defines business logic for users.
type UserService interface {
	GetProfileByTelegramID(ctx context.Context, tgID int64) (*UserProfile, error)
	GetProfileByExternalID(ctx context.Context, platform db.EnumPlatform, externalID string) (*UserProfile, error)
}

type userService struct {
	repo db.Querier
}

// NewUserService creates a new instance of UserService.
func NewUserService(repo db.Querier) UserService {
	return &userService{repo: repo}
}

func (s *userService) GetProfileByTelegramID(ctx context.Context, tgID int64) (*UserProfile, error) {
	return s.GetProfileByExternalID(ctx, db.EnumPlatformTelegram, fmt.Sprintf("%d", tgID))
}

func (s *userService) GetProfileByExternalID(ctx context.Context, platform db.EnumPlatform, externalID string) (*UserProfile, error) {
	// 1. Find account
	ua, err := s.repo.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
		Platform:   platform,
		ExternalID: externalID,
	})
	if err != nil {
		return nil, fmt.Errorf("user account not found: %w", err)
	}

	// 2. Fetch profile (registered_users + participant_stats_cache)
	profile, err := s.repo.GetMyProfile(ctx, ua.S21Login)
	if err != nil {
		return nil, fmt.Errorf("user profile not found: %w", err)
	}

	// 3. Map to domain model
	return &UserProfile{
		Login:         profile.S21Login,
		CampusName:    profile.CampusName.String,
		CoalitionName: profile.CoalitionName.String,
		Level:         profile.Level.Int32,
		Exp:           profile.ExpValue.Int32,
		PRP:           profile.Prp.Int32,
		CRP:           profile.Crp.Int32,
		Coins:         profile.Coins.Int32,
	}, nil
}
