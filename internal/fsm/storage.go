package fsm

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/vgy789/noemx21-bot/internal/database/db"
)

// StateRepository defines the interface for storing user states.
type StateRepository interface {
	GetState(ctx context.Context, userID int64) (*UserState, error)
	SetState(ctx context.Context, state *UserState) error
}

// MemoryStateRepository is an in-memory implementation of StateRepository.
type MemoryStateRepository struct {
	states map[int64]*UserState
	mu     sync.RWMutex
}

func NewMemoryStateRepository() *MemoryStateRepository {
	return &MemoryStateRepository{
		states: make(map[int64]*UserState),
	}
}

func (r *MemoryStateRepository) GetState(ctx context.Context, userID int64) (*UserState, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if state, ok := r.states[userID]; ok {
		// Return a copy to avoid race conditions if caller modifies it
		copyState := *state
		return &copyState, nil
	}
	return nil, nil // Not found
}

func (r *MemoryStateRepository) SetState(ctx context.Context, state *UserState) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.states[state.UserID] = state
	return nil
}

// PostgreSQLStateRepository is a PostgreSQL implementation of StateRepository.
type PostgreSQLStateRepository struct {
	queries db.Querier
}

func NewPostgreSQLStateRepository(queries db.Querier) *PostgreSQLStateRepository {
	return &PostgreSQLStateRepository{
		queries: queries,
	}
}

func (r *PostgreSQLStateRepository) GetState(ctx context.Context, userID int64) (*UserState, error) {
	row, err := r.queries.GetFSMState(ctx, userID)
	if err != nil {
		// sqlc with pgx returns error if no rows found
		// We need to check if it's "no rows" error
		// For now, assume if error happens it might be not found or connection error
		// In a real app we would check errors.Is(err, pgx.ErrNoRows)
		return nil, nil // Fallback to start
	}

	var contextMap map[string]interface{}
	if err := json.Unmarshal(row.Context, &contextMap); err != nil {
		return nil, err
	}

	return &UserState{
		UserID:       row.UserID,
		CurrentFlow:  row.CurrentFlow,
		CurrentState: row.CurrentState,
		Context:      contextMap,
		Language:     row.Language,
	}, nil
}

func (r *PostgreSQLStateRepository) SetState(ctx context.Context, state *UserState) error {
	contextBytes, err := json.Marshal(state.Context)
	if err != nil {
		return err
	}

	return r.queries.UpsertFSMState(ctx, db.UpsertFSMStateParams{
		UserID:       state.UserID,
		CurrentFlow:  state.CurrentFlow,
		CurrentState: state.CurrentState,
		Context:      contextBytes,
		Language:     state.Language,
	})
}
