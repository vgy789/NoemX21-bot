package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	telegramPollingLockKey        int64         = 21021021
	telegramPollingLockRetryDelay time.Duration = 2 * time.Second
	telegramPollingLockReleaseTTL time.Duration = 5 * time.Second
)

type pollingLease interface {
	Release() error
}

type pollingLocker interface {
	Acquire(ctx context.Context) (pollingLease, error)
}

type advisoryLockClient interface {
	Acquire(ctx context.Context) (advisoryLockSession, error)
}

type advisoryLockSession interface {
	TryLock(ctx context.Context, key int64) (bool, error)
	Unlock(ctx context.Context, key int64) (bool, error)
	Release()
	Destroy(ctx context.Context) error
}

type noopPollingLocker struct{}

func (noopPollingLocker) Acquire(context.Context) (pollingLease, error) {
	return noopPollingLease{}, nil
}

type noopPollingLease struct{}

func (noopPollingLease) Release() error {
	return nil
}

type postgresPollingLocker struct {
	client         advisoryLockClient
	log            *slog.Logger
	retryDelay     time.Duration
	releaseTimeout time.Duration
}

func newPollingLocker(pool *pgxpool.Pool, log *slog.Logger) pollingLocker {
	if pool == nil {
		return noopPollingLocker{}
	}
	if log == nil {
		log = slog.Default()
	}
	return &postgresPollingLocker{
		client:         pgxAdvisoryLockClient{pool: pool},
		log:            log,
		retryDelay:     telegramPollingLockRetryDelay,
		releaseTimeout: telegramPollingLockReleaseTTL,
	}
}

func (l *postgresPollingLocker) Acquire(ctx context.Context) (pollingLease, error) {
	for {
		session, err := l.client.Acquire(ctx)
		if err != nil {
			return nil, fmt.Errorf("acquire postgres advisory lock session: %w", err)
		}

		locked, err := session.TryLock(ctx, telegramPollingLockKey)
		if err != nil {
			destroyErr := session.Destroy(context.Background())
			if destroyErr != nil {
				return nil, errors.Join(
					fmt.Errorf("acquire telegram polling lock: %w", err),
					fmt.Errorf("destroy postgres advisory lock session: %w", destroyErr),
				)
			}
			return nil, fmt.Errorf("acquire telegram polling lock: %w", err)
		}

		if locked {
			l.log.Info("telegram polling lock acquired")
			return &postgresPollingLease{
				session:        session,
				log:            l.log,
				releaseTimeout: l.releaseTimeout,
			}, nil
		}

		session.Release()
		l.log.Warn("telegram polling lock is busy; waiting for the previous instance to stop", "retry_in", l.retryDelay)

		if err := waitForPollingLockRetry(ctx, l.retryDelay); err != nil {
			return nil, err
		}
	}
}

func waitForPollingLockRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type postgresPollingLease struct {
	session        advisoryLockSession
	log            *slog.Logger
	releaseTimeout time.Duration
}

func (l *postgresPollingLease) Release() error {
	if l.session == nil {
		return nil
	}

	session := l.session
	l.session = nil

	ctx, cancel := context.WithTimeout(context.Background(), l.releaseTimeout)
	defer cancel()

	unlocked, err := session.Unlock(ctx, telegramPollingLockKey)
	if err != nil {
		destroyErr := session.Destroy(ctx)
		if destroyErr != nil {
			return errors.Join(
				fmt.Errorf("release telegram polling lock: %w", err),
				fmt.Errorf("destroy postgres advisory lock session: %w", destroyErr),
			)
		}
		return fmt.Errorf("release telegram polling lock: %w", err)
	}

	if !unlocked {
		destroyErr := session.Destroy(ctx)
		if destroyErr != nil {
			return errors.Join(
				errors.New("telegram polling lock was not held by the current session"),
				fmt.Errorf("destroy postgres advisory lock session: %w", destroyErr),
			)
		}
		return errors.New("telegram polling lock was not held by the current session")
	}

	session.Release()
	l.log.Info("telegram polling lock released")
	return nil
}

type pgxAdvisoryLockClient struct {
	pool *pgxpool.Pool
}

func (c pgxAdvisoryLockClient) Acquire(ctx context.Context) (advisoryLockSession, error) {
	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	return &pgxAdvisoryLockSession{conn: conn}, nil
}

type pgxAdvisoryLockSession struct {
	conn *pgxpool.Conn
}

func (s *pgxAdvisoryLockSession) TryLock(ctx context.Context, key int64) (bool, error) {
	var locked bool
	if err := s.conn.QueryRow(ctx, "select pg_try_advisory_lock($1)", key).Scan(&locked); err != nil {
		return false, err
	}
	return locked, nil
}

func (s *pgxAdvisoryLockSession) Unlock(ctx context.Context, key int64) (bool, error) {
	var unlocked bool
	if err := s.conn.QueryRow(ctx, "select pg_advisory_unlock($1)", key).Scan(&unlocked); err != nil {
		return false, err
	}
	return unlocked, nil
}

func (s *pgxAdvisoryLockSession) Release() {
	s.conn.Release()
}

func (s *pgxAdvisoryLockSession) Destroy(ctx context.Context) error {
	conn := s.conn.Hijack()
	return conn.Close(ctx)
}

var _ advisoryLockClient = (*pgxAdvisoryLockClient)(nil)
var _ advisoryLockSession = (*pgxAdvisoryLockSession)(nil)
var _ pollingLocker = (*postgresPollingLocker)(nil)
var _ pollingLease = (*postgresPollingLease)(nil)
var _ pollingLease = noopPollingLease{}
var _ pollingLocker = noopPollingLocker{}
