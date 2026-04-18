package telegram

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAdvisoryLockClient struct {
	sessions []advisoryLockSession
	acquired int
	err      error
}

func (c *fakeAdvisoryLockClient) Acquire(context.Context) (advisoryLockSession, error) {
	if c.err != nil {
		return nil, c.err
	}
	if c.acquired >= len(c.sessions) {
		return nil, errors.New("unexpected acquire")
	}
	session := c.sessions[c.acquired]
	c.acquired++
	return session, nil
}

type fakeAdvisoryLockSession struct {
	tryLockResult bool
	tryLockErr    error
	unlockResult  bool
	unlockErr     error
	released      bool
	destroyed     bool
}

func (s *fakeAdvisoryLockSession) TryLock(context.Context, int64) (bool, error) {
	return s.tryLockResult, s.tryLockErr
}

func (s *fakeAdvisoryLockSession) Unlock(context.Context, int64) (bool, error) {
	return s.unlockResult, s.unlockErr
}

func (s *fakeAdvisoryLockSession) Release() {
	s.released = true
}

func (s *fakeAdvisoryLockSession) Destroy(context.Context) error {
	s.destroyed = true
	return nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestPostgresPollingLockerAcquireRetriesBusySession(t *testing.T) {
	first := &fakeAdvisoryLockSession{tryLockResult: false}
	second := &fakeAdvisoryLockSession{tryLockResult: true, unlockResult: true}
	locker := &postgresPollingLocker{
		client:         &fakeAdvisoryLockClient{sessions: []advisoryLockSession{first, second}},
		log:            discardLogger(),
		retryDelay:     0,
		releaseTimeout: time.Millisecond,
	}

	lease, err := locker.Acquire(context.Background())
	require.NoError(t, err)
	require.NotNil(t, lease)
	assert.True(t, first.released)
	assert.False(t, first.destroyed)
	assert.False(t, second.released)

	require.NoError(t, lease.Release())
	assert.True(t, second.released)
	assert.False(t, second.destroyed)
}

func TestPostgresPollingLockerAcquireDestroysSessionOnTryLockError(t *testing.T) {
	session := &fakeAdvisoryLockSession{tryLockErr: errors.New("boom")}
	locker := &postgresPollingLocker{
		client:         &fakeAdvisoryLockClient{sessions: []advisoryLockSession{session}},
		log:            discardLogger(),
		retryDelay:     0,
		releaseTimeout: time.Millisecond,
	}

	lease, err := locker.Acquire(context.Background())
	require.Error(t, err)
	assert.Nil(t, lease)
	assert.True(t, session.destroyed)
	assert.False(t, session.released)
}

func TestPostgresPollingLeaseReleaseDestroysSessionOnUnlockError(t *testing.T) {
	session := &fakeAdvisoryLockSession{unlockErr: errors.New("boom")}
	lease := &postgresPollingLease{
		session:        session,
		log:            discardLogger(),
		releaseTimeout: time.Millisecond,
	}

	err := lease.Release()
	require.Error(t, err)
	assert.True(t, session.destroyed)
	assert.False(t, session.released)
}
