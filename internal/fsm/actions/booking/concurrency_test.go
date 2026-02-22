//go:build integration

package booking

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/testutil/testdb"
)

func TestBooking_Concurrency_Stress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	tdb := testdb.NewPostgres(t)
	queries := tdb.DB.Queries
	campusID, userAID, _ := seedTestData(t, queries)

	ctx := context.Background()
	loc, _ := time.LoadLocation("Europe/Moscow")

	today := time.Now().In(loc)
	todayDate := pgtype.Date{Time: today, Valid: true}

	const workers = 10
	const iterations = 20
	var wg sync.WaitGroup

	// Attempt to book random slots concurrently
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID)))

			for j := 0; j < iterations; j++ {
				roomID := int16(r.Intn(2) + 1) // Room 1 or 2
				hour := r.Intn(10) + 10        // 10:00 to 19:00
				min := r.Intn(2) * 30          // 00 or 30

				startTime := pgtype.Time{Microseconds: int64(hour*60+min) * 60000000, Valid: true}

				// Attempt to create booking
				_, err := queries.CreateRoomBooking(ctx, db.CreateRoomBookingParams{
					CampusID:        campusID,
					RoomID:          roomID,
					UserID:          userAID,
					BookingDate:     todayDate,
					StartTime:       startTime,
					DurationMinutes: 30,
				})

				// We expect some to fail with ErrNoRows due to ON CONFLICT DO NOTHING
				// But we should NOT see deadlocks (code 40P01)
				if err != nil && err != pgx.ErrNoRows {
					// Check if it's a deadlock error
					t.Errorf("Worker %d iteration %d: unexpected error: %v", workerID, j, err)
				}

				// Small sleep to increase interleaving
				time.Sleep(time.Duration(r.Intn(10)) * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
}

func TestBooking_Deadlock_CircularWait(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	tdb := testdb.NewPostgres(t)
	pool := tdb.Pool
	ctx := context.Background()

	// Create seed data
	campusID, _, _ := seedTestData(t, tdb.DB.Queries)

	// We'll use two rooms to create a circular dependency using explicit row locks
	// room 1 and room 2 exists from seedTestData

	errCh := make(chan error, 2)
	barrier := make(chan struct{})
	barrier2 := make(chan struct{})

	// Transaction 1
	go func() {
		tx, err := pool.Begin(ctx)
		if err != nil {
			errCh <- err
			return
		}
		defer tx.Rollback(ctx)

		// 1. Lock Room 1
		_, err = tx.Exec(ctx, "SELECT id FROM rooms WHERE campus_id = $1 AND id = 1 FOR UPDATE", campusID)
		if err != nil {
			errCh <- err
			return
		}

		barrier <- struct{}{} // Signal T1 has Room 1
		<-barrier2            // Wait for T2 to have Room 2

		// 2. Try to lock Room 2
		_, err = tx.Exec(ctx, "SELECT id FROM rooms WHERE campus_id = $1 AND id = 2 FOR UPDATE", campusID)
		errCh <- err
	}()

	// Transaction 2
	go func() {
		tx, err := pool.Begin(ctx)
		if err != nil {
			errCh <- err
			return
		}
		defer tx.Rollback(ctx)

		<-barrier // Wait for T1 to have Room 1

		// 1. Lock Room 2
		_, err = tx.Exec(ctx, "SELECT id FROM rooms WHERE campus_id = $1 AND id = 2 FOR UPDATE", campusID)
		if err != nil {
			errCh <- err
			return
		}

		barrier2 <- struct{}{} // Signal T2 has Room 2

		// 2. Try to lock Room 1
		_, err = tx.Exec(ctx, "SELECT id FROM rooms WHERE campus_id = $1 AND id = 1 FOR UPDATE", campusID)
		errCh <- err
	}()

	var errs []error
	for i := 0; i < 2; i++ {
		errs = append(errs, <-errCh)
	}

	// At least one of them should have failed with a deadlock error
	foundDeadlock := false
	for _, err := range errs {
		if err != nil && fmt.Sprintf("%v", err) != "" {
			// In Postgres, deadlock error message contains "deadlock detected"
			if strings.Contains(err.Error(), "deadlock detected") {
				foundDeadlock = true
			}
		}
	}

	assert.True(t, foundDeadlock, "Expected at least one transaction to fail with deadlock")
}
