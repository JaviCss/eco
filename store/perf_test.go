package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/JaviCss/eco/port"
)

func TestStorePerformanceBaseline(t *testing.T) {
	if testing.Short() {
		t.Skip("the performance baseline is the expensive evidence run")
	}
	dir := t.TempDir()
	s := newStoreAt(t, dir)
	ctx := context.Background()

	const corpus = 10000
	seeded := time.Now()
	for i := 0; i < corpus; i++ {
		axis := port.AxisZ2
		if i%2 == 1 {
			axis = port.AxisZ3
		}
		scope := port.ScopeUser
		if axis == port.AxisZ3 {
			scope = port.ScopeProject
		}
		if _, err := s.Append(ctx, scope, axis, port.Entry{
			ID:   fmt.Sprintf("seed-%06d", i),
			Body: fmt.Sprintf("the runtime boundary of module %d needs a seam", i),
		}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	fmt.Printf("PERF seed_%d_append_total_ms=%d\n", corpus, time.Since(seeded).Milliseconds())

	appendStart := time.Now()
	const extra = 1000
	for i := 0; i < extra; i++ {
		if _, err := s.Append(ctx, port.ScopeUser, port.AxisZ2, port.Entry{
			ID:   fmt.Sprintf("extra-%06d", i),
			Body: fmt.Sprintf("an extra runtime note %d", i),
		}); err != nil {
			t.Fatalf("extra %d: %v", i, err)
		}
	}
	appendElapsed := time.Since(appendStart)
	fmt.Printf("PERF append_%d_total_ms=%d mean_us=%.1f\n",
		extra, appendElapsed.Milliseconds(), float64(appendElapsed.Microseconds())/float64(extra))

	searchStart := time.Now()
	const queries = 100
	hits := 0
	for i := 0; i < queries; i++ {
		found, err := s.Search(ctx, port.ScopeUser, port.AxisZ2, fmt.Sprintf("untime boundar%d", i%10), 10)
		if err != nil {
			found, err = s.Search(ctx, port.ScopeUser, port.AxisZ2, "untime boundar", 10)
			if err != nil {
				t.Fatalf("search %d: %v", i, err)
			}
		}
		hits += len(found)
	}
	searchElapsed := time.Since(searchStart)
	fmt.Printf("PERF search_%d_total_ms=%d mean_us=%.1f hits=%d\n",
		queries, searchElapsed.Milliseconds(), float64(searchElapsed.Microseconds())/float64(queries), hits)

	ids := make([]string, 0, 100)
	for i := 1; i < 200 && len(ids) < 100; i += 2 {
		ids = append(ids, fmt.Sprintf("seed-%06d", i))
	}
	promoteStart := time.Now()
	promoted, err := Promote(ctx, s, s, ids)
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	promoteElapsed := time.Since(promoteStart)
	fmt.Printf("PERF promote_100_from_z3_%d_total_ms=%d mean_us=%.1f written=%d\n",
		corpus, promoteElapsed.Milliseconds(), float64(promoteElapsed.Microseconds())/float64(len(ids)), len(promoted))
}