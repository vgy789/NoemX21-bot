package service

import (
	"context"
	"fmt"

	"github.com/vgy789/noemx21-bot/internal/database/db"
)

// StudentProfile represents the domain model for a user profile.
type StudentProfile struct {
	Login         string
	CampusName    string
	CoalitionName string
	Level         int32
	Exp           int32
	PRP           int32
	CRP           int32
	Coins         int32
}

// StudentService defines business logic for students.
type StudentService interface {
	GetProfileByTelegramID(ctx context.Context, tgID int64) (*StudentProfile, error)
	GetProfileByExternalID(ctx context.Context, platform db.EnumPlatform, externalID string) (*StudentProfile, error)
}

type studentService struct {
	repo db.Querier
}

// NewStudentService creates a new instance of StudentService.
func NewStudentService(repo db.Querier) StudentService {
	return &studentService{repo: repo}
}

func (s *studentService) GetProfileByTelegramID(ctx context.Context, tgID int64) (*StudentProfile, error) {
	return s.GetProfileByExternalID(ctx, db.EnumPlatformTelegram, fmt.Sprintf("%d", tgID))
}

func (s *studentService) GetProfileByExternalID(ctx context.Context, platform db.EnumPlatform, externalID string) (*StudentProfile, error) {
	// 1. Find account
	ua, err := s.repo.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
		Platform:   platform,
		ExternalID: externalID,
	})
	if err != nil {
		return nil, fmt.Errorf("user account not found: %w", err)
	}

	// 2. Fetch profile with joins
	profile, err := s.repo.GetStudentProfile(ctx, ua.StudentID)
	if err != nil {
		return nil, fmt.Errorf("student profile not found: %w", err)
	}

	// 3. Map to domain model
	return &StudentProfile{
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
