package fsm

import (
	"context"
	"sync"
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
