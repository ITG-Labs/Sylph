package store

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func TestConcurrentRegistrationsDoNotDeadlockOnNodeBumps(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `INSERT INTO nodes (cert_cn, name) SELECT 'dl' || i, 'dl' || i FROM generate_series(1, 8) i`); err != nil {
		t.Fatal(err)
	}
	var nodes []int64
	rows, err := s.pool.Query(ctx, `SELECT id FROM nodes ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		nodes = append(nodes, id)
	}
	rows.Close()
	warmPool(t, s)
	var failed atomic.Int64
	var firstErr sync.Once
	report := func(err error) {
		failed.Add(1)
		firstErr.Do(func() { t.Errorf("first failure: %v", err) })
	}
	var wg sync.WaitGroup
	for w := range 24 {
		wg.Go(func() {
			for i := range 25 {
				tg := int64(9_000_000 + w*1000 + i)
				if _, _, err := s.RegisterTelegramUser(ctx, tg, model.NewToken(), fmt.Sprintf("00000000-0000-4000-8000-%012d", tg)); err != nil {
					report(err)
				}
			}
		})
	}
	for _, id := range nodes {
		wg.Go(func() {
			for range 50 {
				if err := s.TouchNodeSync(ctx, id, true, true); err != nil {
					report(err)
				}
			}
		})
	}
	for range 4 {
		wg.Go(func() {
			for range 60 {
				subset := slices.Clone(nodes)
				rand.Shuffle(len(subset), func(i, j int) { subset[i], subset[j] = subset[j], subset[i] })
				if err := s.BumpNodes(ctx, subset[:1+rand.IntN(len(subset))]); err != nil {
					report(err)
				}
			}
		})
	}
	wg.Wait()
	if n := failed.Load(); n > 0 {
		t.Fatalf("%d operations failed under concurrent node bumps", n)
	}
	var users int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if users != 24*25 {
		t.Fatalf("users = %d", users)
	}
}
