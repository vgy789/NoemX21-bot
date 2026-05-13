package db

import "context"

// Exec exposes raw SQL execution for targeted schema recovery paths.
func (q *Queries) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := q.db.Exec(ctx, sql, args...)
	return err
}
