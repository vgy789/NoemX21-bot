package gitsync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"gopkg.in/yaml.v3"
)

const fallbackProjectsCatalogPath = "data_repo/bot_content/various/projects.yaml"

func (s *Service) syncProjectsCatalog(ctx context.Context) error {
	projectsPath, ok := s.resolveProjectsCatalogPath()
	if !ok {
		s.log.Debug("projects catalog not found, skipping", "configured_path", s.cfg.ProjectsPath)
		return nil
	}

	data, err := os.ReadFile(projectsPath)
	if err != nil {
		return fmt.Errorf("read projects catalog: %w", err)
	}

	var projectsYAML ProjectsFileYAML
	if err := yaml.Unmarshal(data, &projectsYAML); err != nil {
		return fmt.Errorf("parse projects catalog yaml: %w", err)
	}

	batchID := time.Now().UnixNano()
	syncedProjects := 0

	for _, p := range projectsYAML.Projects {
		if p.ID <= 0 {
			continue
		}

		title := strings.TrimSpace(p.Title)
		if title == "" {
			title = fmt.Sprintf("Project %d", p.ID)
		}

		courseID := int8FromPtr(p.CourseID)
		courseTitle := strings.TrimSpace(stringFromPtr(p.CourseTitle))
		if courseID.Valid {
			if courseTitle == "" {
				courseTitle = fmt.Sprintf("Course %d", courseID.Int64)
			}

			if err := s.queries.UpsertCourseCatalog(ctx, db.UpsertCourseCatalogParams{
				ID:          courseID.Int64,
				Title:       courseTitle,
				Code:        pgtype.Text{},
				SyncBatchID: batchID,
			}); err != nil {
				s.log.Warn("failed to upsert course catalog row", "course_id", courseID.Int64, "error", err)
				continue
			}
		}

		if err := s.queries.UpsertProjectCatalog(ctx, db.UpsertProjectCatalogParams{
			ID:          p.ID,
			CourseID:    courseID,
			Title:       title,
			Code:        textFromPtr(p.Code),
			SyncBatchID: batchID,
		}); err != nil {
			s.log.Warn("failed to upsert project catalog row", "project_id", p.ID, "error", err)
			continue
		}

		for _, rawNode := range p.Nodes {
			nodeID, nodeErr := s.upsertNodePath(ctx, rawNode, batchID)
			if nodeErr != nil {
				s.log.Warn("failed to upsert node path", "project_id", p.ID, "node", rawNode, "error", nodeErr)
				continue
			}

			if err := s.queries.UpsertProjectNodeCatalog(ctx, db.UpsertProjectNodeCatalogParams{
				ProjectID:   p.ID,
				NodeID:      nodeID,
				SyncBatchID: batchID,
			}); err != nil {
				s.log.Warn("failed to upsert project-node relation", "project_id", p.ID, "node_id", nodeID, "error", err)
			}
		}

		syncedProjects++
	}

	if err := s.queries.DeleteStaleProjectNodesCatalog(ctx, batchID); err != nil {
		return fmt.Errorf("delete stale project_nodes: %w", err)
	}
	if err := s.queries.DeleteStaleProjectsCatalog(ctx, batchID); err != nil {
		return fmt.Errorf("delete stale projects: %w", err)
	}
	if err := s.queries.DeleteStaleCoursesCatalog(ctx, batchID); err != nil {
		return fmt.Errorf("delete stale courses: %w", err)
	}
	if err := s.queries.DeleteStaleNodesCatalog(ctx, batchID); err != nil {
		return fmt.Errorf("delete stale nodes: %w", err)
	}
	if err := s.queries.DeleteStaleProjectSearchCatalog(ctx, batchID); err != nil {
		return fmt.Errorf("delete stale project_search rows: %w", err)
	}

	s.log.Info("projects catalog synced", "path", projectsPath, "projects", syncedProjects)
	return nil
}

func (s *Service) resolveProjectsCatalogPath() (string, bool) {
	candidates := make([]string, 0, 4)
	configured := strings.TrimSpace(s.cfg.ProjectsPath)
	if configured != "" {
		if filepath.IsAbs(configured) {
			candidates = append(candidates, configured)
		} else {
			base := strings.TrimSpace(s.cfg.LocalPath)
			if base != "" {
				candidates = append(candidates, filepath.Join(base, configured))
			}
			candidates = append(candidates, configured)
		}
	}
	candidates = append(candidates, fallbackProjectsCatalogPath)

	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate, true
		}
	}

	return "", false
}

func (s *Service) upsertNodePath(ctx context.Context, raw string, batchID int64) (int64, error) {
	parts := splitCatalogNodePath(raw)
	if len(parts) == 0 {
		return 0, fmt.Errorf("empty node path")
	}

	parentID := pgtype.Int8{}
	leafID := int64(0)

	for _, name := range parts {
		id, err := s.queries.UpsertNodeCatalog(ctx, db.UpsertNodeCatalogParams{
			Name:        name,
			ParentID:    parentID,
			SyncBatchID: batchID,
		})
		if err != nil {
			return 0, err
		}
		leafID = id
		parentID = pgtype.Int8{Int64: id, Valid: true}
	}

	return leafID, nil
}

func splitCatalogNodePath(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	splitters := []string{" / ", " > ", " -> ", "::", " - "}
	for _, splitter := range splitters {
		if strings.Contains(raw, splitter) {
			return splitAndClean(raw, splitter)
		}
	}

	if shouldSplitCompactDash(raw) {
		return splitAndClean(raw, "-")
	}

	return []string{raw}
}

func splitAndClean(raw string, splitter string) []string {
	parts := strings.Split(raw, splitter)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Join(strings.Fields(strings.TrimSpace(part)), " ")
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func shouldSplitCompactDash(raw string) bool {
	if strings.Contains(raw, " ") || strings.Count(raw, "-") != 1 {
		return false
	}

	parts := strings.SplitN(raw, "-", 2)
	if len(parts) != 2 {
		return false
	}

	suffix := strings.TrimSpace(parts[1])
	if suffix == "" || len(suffix) > 8 {
		return false
	}

	for _, r := range suffix {
		if (r < '0' || r > '9') && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			return false
		}
	}

	return true
}

func textFromPtr(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return toText(strings.TrimSpace(*value))
}

func stringFromPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func int8FromPtr(value *int64) pgtype.Int8 {
	if value == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *value, Valid: true}
}
