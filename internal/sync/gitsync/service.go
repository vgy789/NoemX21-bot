package gitsync

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/robfig/cron/v3"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	cryptossh "golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

type Service struct {
	cfg     config.GitSync
	queries db.Querier
	log     *slog.Logger
	cron    *cron.Cron
}

func New(cfg config.GitSync, queries db.Querier, log *slog.Logger) *Service {
	return &Service{
		cfg:     cfg,
		queries: queries,
		log:     log.With("worker", "gitsync"),
		cron:    cron.New(),
	}
}

func (s *Service) Start() error {
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

func (s *Service) Stop() {
	s.cron.Stop()
}

func (s *Service) Sync(ctx context.Context) error {
	s.log.Info("starting git sync")

	// 1. Update repo
	updateErr := s.updateRepo()
	if updateErr != nil {
		return fmt.Errorf("failed to update repo: %w", updateErr)
	}

	// 2. Scan directories
	entries, err := os.ReadDir(s.cfg.LocalPath)
	if err != nil {
		return fmt.Errorf("failed to read local path: %w", err)
	}

	synced := 0
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "_") {
			continue
		}

		// Use directory name directly to match short_name in DB
		campusName := entry.Name()
		campus, err := s.queries.GetCampusByShortName(ctx, campusName)
		if err != nil {
			// Fallback: try adding "21 " if not present
			if !strings.HasPrefix(campusName, "21 ") {
				campus, err = s.queries.GetCampusByShortName(ctx, "21 "+campusName)
			}
			if err != nil {
				s.log.Warn("skipping directory: campus not found in DB", "dir", entry.Name())
				continue
			}
		}

		if err := s.syncCampus(ctx, campus, filepath.Join(s.cfg.LocalPath, entry.Name())); err != nil {
			s.log.Error("failed to sync campus", "campus", campusName, "error", err)
		} else {
			synced++
		}
	}

	s.log.Info("git sync completed")
	return nil
}

const (
	sshRetryAttempts = 3
	sshRetryDelay    = 3 * time.Second
)

func (s *Service) updateRepo() error {
	auth, err := s.getAuth()
	if err != nil {
		return err
	}

	var lastErr error
	for attempt := 1; attempt <= sshRetryAttempts; attempt++ {
		lastErr = s.doUpdateRepo(auth)
		if lastErr == nil {
			return nil
		}
		if !isTransientSSHError(lastErr) || attempt == sshRetryAttempts {
			return lastErr
		}
		s.log.Warn("retrying after transient SSH/network error", "attempt", attempt, "error", lastErr)
		time.Sleep(sshRetryDelay)
	}
	return lastErr
}

func isTransientSSHError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "handshake failed") ||
		strings.Contains(s, "EOF") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "i/o timeout")
}

func (s *Service) doUpdateRepo(auth ssh.AuthMethod) error {
	gitExists := true
	if _, err := os.Stat(filepath.Join(s.cfg.LocalPath, ".git")); os.IsNotExist(err) {
		gitExists = false
	}

	if !gitExists {
		return s.cloneRepo(auth)
	}
	s.log.Debug("pulling repository (force)", "path", s.cfg.LocalPath)
	r, err := git.PlainOpen(s.cfg.LocalPath)
	if err != nil {
		return err
	}
	ref, err := r.Head()
	if err != nil {
		// Empty clone: HEAD/refs/heads/main may not exist; fetch the other branch and checkout.
		if isRefNotFound(err) {
			if repairErr := s.repairWhenHeadFails(r, auth); repairErr != nil {
				return err
			}
			return nil
		}
		return err
	}
	branch := ref.Name()
	if !branch.IsBranch() {
		branch = plumbing.NewBranchReferenceName(s.cfg.Branch)
	}
	branchShort := branch.Short()
	remote, err := r.Remote("origin")
	if err != nil {
		return err
	}
	// Force fetch: always update remote ref from origin
	refSpec := gitconfig.RefSpec("+refs/heads/" + branchShort + ":refs/remotes/origin/" + branchShort)
	fetchErr := remote.Fetch(&git.FetchOptions{
		Auth:     auth,
		RefSpecs: []gitconfig.RefSpec{refSpec},
		Force:    true,
	})
	if fetchErr != nil && fetchErr != git.NoErrAlreadyUpToDate {
		if isRefNotFound(fetchErr) {
			w, wErr := r.Worktree()
			if wErr != nil {
				return fetchErr
			}
			if repairErr := s.repairBranchThenPull(r, w, auth); repairErr != nil {
				return fetchErr
			}
			return nil
		}
		return fetchErr
	}
	remoteRef, err := r.Reference(plumbing.ReferenceName("refs/remotes/origin/"+branchShort), true)
	if err != nil {
		return err
	}
	w, err := r.Worktree()
	if err != nil {
		return err
	}
	// Hard reset to remote: working tree always matches origin
	if err := r.Storer.SetReference(plumbing.NewHashReference(branch, remoteRef.Hash())); err != nil {
		return err
	}
	s.log.Info("resetting worktree to remote", "branch", branchShort, "commit", remoteRef.Hash().String())
	return w.Reset(&git.ResetOptions{Mode: git.HardReset, Commit: remoteRef.Hash()})
}

// repairWhenHeadFails fixes repo when Head() fails (e.g. empty clone with no valid HEAD). Fetches master, creates local branch, checkouts.
func (s *Service) repairWhenHeadFails(r *git.Repository, auth ssh.AuthMethod) error {
	remote, err := r.Remote("origin")
	if err != nil {
		return err
	}
	for _, branch := range []string{"master", "main"} {
		refSpec := gitconfig.RefSpec("+refs/heads/" + branch + ":refs/remotes/origin/" + branch)
		if err := remote.Fetch(&git.FetchOptions{
			Auth:     auth,
			RefSpecs: []gitconfig.RefSpec{refSpec},
			Force:    true,
		}); err != nil {
			continue
		}
		remoteRef, err := r.Reference(plumbing.ReferenceName("refs/remotes/origin/"+branch), true)
		if err != nil {
			continue
		}
		_ = r.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName(branch), remoteRef.Hash()))
		w, err := r.Worktree()
		if err != nil {
			return err
		}
		if err := w.Checkout(&git.CheckoutOptions{Branch: plumbing.NewBranchReferenceName(branch)}); err != nil {
			continue
		}
		_ = w.Reset(&git.ResetOptions{Mode: git.HardReset, Commit: remoteRef.Hash()})
		return nil
	}
	return fmt.Errorf("could not repair: neither master nor main found on remote")
}

// repairBranchThenPull fetches the other default branch (main/master), checks it out, and pulls.
// Used when the current branch does not exist on remote (e.g. local main empty, remote has master).
func (s *Service) repairBranchThenPull(r *git.Repository, w *git.Worktree, auth ssh.AuthMethod) error {
	head, err := r.Head()
	if err != nil {
		return err
	}
	currentShort := head.Name().Short()
	otherBranch := "master"
	if currentShort == "master" {
		otherBranch = "main"
	}
	s.log.Warn("current branch not on remote, switching to", "branch", otherBranch)

	remote, err := r.Remote("origin")
	if err != nil {
		return err
	}
	refSpec := gitconfig.RefSpec("+refs/heads/" + otherBranch + ":refs/remotes/origin/" + otherBranch)
	if err := remote.Fetch(&git.FetchOptions{
		Auth:     auth,
		RefSpecs: []gitconfig.RefSpec{refSpec},
		Force:    true,
	}); err != nil && !strings.Contains(err.Error(), "already up-to-date") {
		return err
	}
	remoteRef, err := r.Reference(plumbing.ReferenceName("refs/remotes/origin/"+otherBranch), true)
	if err != nil {
		return err
	}
	_ = r.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName(otherBranch), remoteRef.Hash()))
	if err := w.Checkout(&git.CheckoutOptions{Branch: plumbing.NewBranchReferenceName(otherBranch)}); err != nil {
		return err
	}
	return w.Reset(&git.ResetOptions{Mode: git.HardReset, Commit: remoteRef.Hash()})
}

// cloneRepo clones the repository. Tries GIT_BRANCH first; if the ref is not found or clone is empty (e.g. repo uses master), tries the other common branch.
func (s *Service) cloneRepo(auth ssh.AuthMethod) error {
	branch := s.cfg.Branch
	for attempt := range 2 {
		s.log.Info("cloning repository", "url", s.cfg.RepoURL, "path", s.cfg.LocalPath, "branch", branch)
		_, err := git.PlainClone(s.cfg.LocalPath, false, &git.CloneOptions{
			URL:           s.cfg.RepoURL,
			ReferenceName: plumbing.NewBranchReferenceName(branch),
			SingleBranch:  true,
			Auth:          auth,
			Progress:      os.Stdout,
		})
		if err != nil {
			// If branch not found, try the other common default (main <-> master)
			if isRefNotFound(err) && attempt == 0 {
				branch = alternateBranch(branch)
				if branch == "" {
					return err
				}
				s.log.Warn("branch not found, retrying with", "branch", branch)
				continue
			}
			return err
		}
		// Some servers succeed cloning non-existent branch with empty worktree; retry with other branch.
		if attempt == 0 && cloneIsEmpty(s.cfg.LocalPath) {
			other := alternateBranch(branch)
			if other == "" {
				return nil
			}
			_ = os.RemoveAll(s.cfg.LocalPath)
			branch = other
			s.log.Warn("clone was empty, retrying with", "branch", branch)
			continue
		}
		return nil
	}
	return fmt.Errorf("clone failed (tried branch %s)", s.cfg.Branch)
}

func alternateBranch(branch string) string {
	switch branch {
	case "main":
		return "master"
	case "master":
		return "main"
	default:
		return ""
	}
}

func cloneIsEmpty(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return true
	}
	for _, e := range entries {
		if e.Name() != ".git" {
			return false
		}
	}
	return true
}

func isRefNotFound(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "reference not found") ||
		strings.Contains(s, "couldn't find remote ref") ||
		strings.Contains(s, "Remote branch")
}

func (s *Service) getAuth() (ssh.AuthMethod, error) {
	if s.cfg.SSHKeyBase64 == "" {
		return nil, nil
	}
	keyPEM, err := base64.StdEncoding.DecodeString(string(s.cfg.SSHKeyBase64))
	if err != nil {
		return nil, fmt.Errorf("decode SSH_KEY_BASE64: %w", err)
	}
	auth, err := ssh.NewPublicKeys("git", keyPEM, "")
	if err != nil {
		return nil, fmt.Errorf("parse SSH key: %w", err)
	}
	// Host key verification: use SSH_KNOWN_HOSTS if set (paths separated by ":"), otherwise accept any host (e.g. in Docker/CI).
	if paths := os.Getenv("SSH_KNOWN_HOSTS"); paths != "" {
		files := strings.Split(paths, ":")
		callback, err := ssh.NewKnownHostsCallback(files...)
		if err != nil {
			return nil, fmt.Errorf("known_hosts: %w", err)
		}
		auth.HostKeyCallbackHelper.HostKeyCallback = callback
	} else {
		auth.HostKeyCallbackHelper.HostKeyCallback = cryptossh.InsecureIgnoreHostKey()
		s.log.Warn("SSH_KNOWN_HOSTS not set, accepting any host key")
	}
	return auth, nil
}

func (s *Service) syncCampus(ctx context.Context, campus db.Campuse, path string) error {
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

	// Update campus leader info
	_, err = s.queries.UpsertCampus(ctx, db.UpsertCampusParams{
		ID:             campus.ID,
		ShortName:      campus.ShortName,
		FullName:       campus.FullName,
		Timezone:       campus.Timezone,
		IsActive:       campus.IsActive,
		LeaderName:     toText(clubsYAML.Leader.Name),
		LeaderFormLink: toText(clubsYAML.Leader.FormLink),
	})
	if err != nil {
		s.log.Error("failed to update campus leader info", "campus", campus.ShortName, "error", err)
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

	s.log.Info("campus sync done (clubs)", "campus", campus.ShortName, "clubs", len(clubsYAML.Clubs))

	// Sync Rooms
	if err := s.syncRooms(ctx, campus, path); err != nil {
		s.log.Error("failed to sync rooms", "campus", campus.ShortName, "error", err)
	}

	// Sync Books
	if err := s.syncBooks(ctx, campus, path); err != nil {
		s.log.Error("failed to sync books", "campus", campus.ShortName, "error", err)
	}

	return nil
}

func (s *Service) syncRooms(ctx context.Context, campus db.Campuse, path string) error {
	roomsPath := filepath.Join(path, "rooms.csv")
	if _, err := os.Stat(roomsPath); os.IsNotExist(err) {
		return nil
	}

	rooms, err := ParseRoomsCSV(roomsPath)
	if err != nil {
		return fmt.Errorf("failed to parse rooms.csv: %w", err)
	}

	if err := s.queries.DeactivateRoomsByCampus(ctx, campus.ID); err != nil {
		return err
	}

	for _, r := range rooms {
		id, err := strconv.Atoi(r.ID)
		if err != nil {
			s.log.Warn("skipping room with invalid ID", "id", r.ID, "campus", campus.ShortName)
			continue
		}
		minDur, _ := strconv.Atoi(r.MinDuration)
		maxDur, _ := strconv.Atoi(r.MaxDuration)

		_, err = s.queries.UpsertRoom(ctx, db.UpsertRoomParams{
			ID:          int16(id),
			CampusID:    campus.ID,
			Name:        r.Name,
			MinDuration: int32(minDur),
			MaxDuration: int32(maxDur),
			IsActive:    pgtype.Bool{Bool: r.IsActive, Valid: true},
			Description: toText(r.Description),
		})
		if err != nil {
			s.log.Error("failed to upsert room", "room_id", id, "error", err)
		}
	}
	s.log.Info("rooms sync done", "campus", campus.ShortName, "count", len(rooms))
	return nil
}

func (s *Service) syncBooks(ctx context.Context, campus db.Campuse, path string) error {
	booksPath := filepath.Join(path, "books.csv")
	if _, err := os.Stat(booksPath); os.IsNotExist(err) {
		return nil
	}

	books, err := ParseBooksCSV(booksPath)
	if err != nil {
		return fmt.Errorf("failed to parse books.csv: %w", err)
	}

	if err := s.queries.DeactivateBooksByCampus(ctx, campus.ID); err != nil {
		return err
	}

	for _, b := range books {
		id, err := strconv.Atoi(b.ID)
		if err != nil {
			s.log.Warn("skipping book with invalid ID", "id", b.ID, "campus", campus.ShortName)
			continue
		}
		stock, _ := strconv.Atoi(b.TotalStock)

		_, err = s.queries.UpsertBook(ctx, db.UpsertBookParams{
			ID:          int16(id),
			CampusID:    campus.ID,
			Title:       b.Title,
			Author:      b.Author,
			Category:    b.Category,
			TotalStock:  int32(stock),
			Description: toText(b.Description),
			IsActive:    pgtype.Bool{Bool: true, Valid: true},
		})
		if err != nil {
			s.log.Error("failed to upsert book", "book_id", id, "error", err)
		}
	}
	s.log.Info("books sync done", "campus", campus.ShortName, "count", len(books))
	return nil
}
