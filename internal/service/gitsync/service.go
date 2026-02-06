package gitsync

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/robfig/cron/v3"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"gopkg.in/yaml.v3"
)

type GitSyncService struct {
	cfg     config.GitSync
	queries db.Querier
	log     *slog.Logger
	cron    *cron.Cron
}

func NewGitSyncService(cfg config.GitSync, queries db.Querier, log *slog.Logger) *GitSyncService {
	return &GitSyncService{
		cfg:     cfg,
		queries: queries,
		log:     log.With("service", "gitsync"),
		cron:    cron.New(),
	}
}

func (s *GitSyncService) Start() error {
	if s.cfg.RepoURL == "" {
		s.log.Warn("git repo url not configured, gitsync disabled")
		return nil
	}

	_, err := s.cron.AddFunc("@every "+s.cfg.Interval, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := s.Sync(ctx); err != nil {
			s.log.Error("sync failed", "error", err)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to schedule sync: %w", err)
	}

	s.cron.Start()
	s.log.Info("gitsync started", "interval", s.cfg.Interval, "repo", s.cfg.RepoURL)

	// Run initial sync
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := s.Sync(ctx); err != nil {
			s.log.Error("initial sync failed", "error", err)
		}
	}()

	return nil
}

func (s *GitSyncService) Stop() {
	s.cron.Stop()
}

func (s *GitSyncService) Sync(ctx context.Context) error {
	s.log.Info("starting git sync")

	// 1. Update repo
	if err := s.updateRepo(); err != nil {
		return fmt.Errorf("failed to update repo: %w", err)
	}

	// 2. Scan directories
	entries, err := os.ReadDir(s.cfg.LocalPath)
	if err != nil {
		return fmt.Errorf("failed to read local path: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "_") {
			continue
		}

		// Use directory name directly to match short_name in DB
		campusName := entry.Name()
		campus, err := s.queries.GetCampusByShortName(ctx, campusName)
		if err != nil {
			// Fallback: try stripping "21 " if it exists, or adding it?
			// the DB currently has "21 Novosibirsk".
			// If dir is "21 Novosibirsk", direct match works.
			s.log.Warn("skipping directory: campus not found in DB", "dir", entry.Name())
			continue
		}

		if err := s.syncCampus(ctx, campus, filepath.Join(s.cfg.LocalPath, entry.Name())); err != nil {
			s.log.Error("failed to sync campus", "campus", campusName, "error", err)
		}
	}

	s.log.Info("git sync completed")
	return nil
}

func (s *GitSyncService) updateRepo() error {
	auth := s.getAuth()

	if _, err := os.Stat(filepath.Join(s.cfg.LocalPath, ".git")); os.IsNotExist(err) {
		s.log.Info("cloning repository", "url", s.cfg.RepoURL, "path", s.cfg.LocalPath)
		_, err := git.PlainClone(s.cfg.LocalPath, false, &git.CloneOptions{
			URL:           s.cfg.RepoURL,
			ReferenceName: plumbing.NewBranchReferenceName(s.cfg.Branch),
			SingleBranch:  true,
			Auth:          auth,
			Progress:      os.Stdout,
		})
		if err != nil {
			return err
		}
	} else {
		s.log.Debug("pulling repository", "path", s.cfg.LocalPath)
		r, err := git.PlainOpen(s.cfg.LocalPath)
		if err != nil {
			return err
		}
		w, err := r.Worktree()
		if err != nil {
			return err
		}
		err = w.Pull(&git.PullOptions{
			RemoteName:    "origin",
			ReferenceName: plumbing.NewBranchReferenceName(s.cfg.Branch),
			Auth:          auth,
			SingleBranch:  true,
		})
		if err != nil && err != git.NoErrAlreadyUpToDate {
			return err
		}
	}
	return nil
}

func (s *GitSyncService) getAuth() *http.BasicAuth {
	if s.cfg.AuthToken == "" {
		return nil
	}
	// For GitLab/GitHub tokens, username can be anything, but often "oauth2" or "private-token"
	return &http.BasicAuth{
		Username: "private-token",
		Password: string(s.cfg.AuthToken),
	}
}

func (s *GitSyncService) syncCampus(ctx context.Context, campus db.Campuse, path string) error {
	clubsPath := filepath.Join(path, "clubs.yaml")
	if _, err := os.Stat(clubsPath); os.IsNotExist(err) {
		return nil
	}

	data, err := os.ReadFile(clubsPath)
	if err != nil {
		return err
	}

	var clubsYAML ClubsFileYAML
	if err := yaml.Unmarshal(data, &clubsYAML); err != nil {
		return fmt.Errorf("failed to parse clubs.yaml: %w", err)
	}

	// Mark all clubs in this campus as inactive first
	if err := s.queries.DeactivateClubsByCampus(ctx, campus.ID); err != nil {
		return err
	}

	for _, c := range clubsYAML.Clubs {
		// Upsert Category
		catName := c.Category
		if catName == "" {
			catName = "Other"
		}
		category, err := s.queries.UpsertClubCategory(ctx, catName)
		if err != nil {
			s.log.Error("failed to upsert category", "name", catName, "error", err)
			continue
		}

		// Upsert Club
		_, err = s.queries.UpsertClub(ctx, db.UpsertClubParams{
			ID:           int16(c.ID),
			CampusID:     campus.ID,
			LeaderLogin:  toText(c.LeaderLogin),
			Name:         c.Name,
			Description:  toText(c.Description),
			CategoryID:   category.ID,
			ExternalLink: toText(c.ExternalLink),
			IsLocal:      toBool(c.IsLocal),
			IsActive:     toBool(c.IsActive),
		})
		if err != nil {
			s.log.Error("failed to upsert club", "club_id", c.ID, "name", c.Name, "error", err)
		}
	}

	s.log.Info("campus sync done", "campus", campus.ShortName, "clubs", len(clubsYAML.Clubs))
	return nil
}
