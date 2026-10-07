package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/JaviCss/eco/port"
)

var errCut = errors.New("eco: simulated cut")

type promotionDouble struct {
	name    string
	source  port.Port
	target  port.Port
	setDown func(bool)
	downed  func() port.Port
	cut     func(after int) *cuttingPort
}

func promotionDoubleNamed(t *testing.T, name string) promotionDouble {
	t.Helper()
	fakeSource := port.NewFake()
	storeSource := newStoreAt(t, t.TempDir())
	fakeTarget := newStoreAt(t, t.TempDir())
	storeTarget := newStoreAt(t, t.TempDir())
	fakeOutage := &outageStore{inner: fakeTarget}
	storeOutage := &outageStore{inner: storeTarget}
	doubles := []promotionDouble{
		{
			name:    "fake",
			source:  fakeSource,
			target:  fakeTarget,
			setDown: fakeOutage.SetUnavailable,
			downed:  func() port.Port { return fakeOutage },
			cut: func(after int) *cuttingPort {
				return &cuttingPort{Port: fakeTarget, batchFn: fakeTarget.appendPromotedBatch, after: after, err: errCut}
			},
		},
		{
			name:    "store",
			source:  storeSource,
			target:  storeTarget,
			setDown: storeOutage.SetUnavailable,
			downed:  func() port.Port { return storeOutage },
			cut: func(after int) *cuttingPort {
				return &cuttingPort{Port: storeTarget, batchFn: storeTarget.appendPromotedBatch, after: after, err: errCut}
			},
		},
	}
	for _, d := range doubles {
		if d.name == name {
			return d
		}
	}
	t.Fatalf("there is no promotion double named %q", name)
	return promotionDouble{}
}

type cuttingPort struct {
	port.Port
	batchFn  func(ctx context.Context, scope port.Scope, axis port.Axis, entries []port.Entry) ([]port.Entry, error)
	after    int
	seen     int
	appended []string
	err      error
}

func (c *cuttingPort) appendPromotedBatch(ctx context.Context, scope port.Scope, axis port.Axis, entries []port.Entry) ([]port.Entry, error) {
	if c.seen >= c.after {
		return nil, c.err
	}
	c.seen++
	stored, err := c.batchFn(ctx, scope, axis, entries)
	if err != nil {
		return nil, err
	}
	for _, entry := range stored {
		c.appended = append(c.appended, entry.ID)
	}
	return stored, nil
}

func (c *cuttingPort) preflightPromotion(context.Context, port.Scope, port.Axis, []PromotedTarget) error {
	return nil
}

func (c *cuttingPort) promotionProfile() Profile {
	if inner, ok := c.Port.(promoter); ok {
		return inner.promotionProfile()
	}
	return ProfileRuntime
}

func seedZ3(t *testing.T, source port.Port, ids ...string) {
	t.Helper()
	for i, id := range ids {
		if _, err := source.Append(context.Background(), port.ScopeProject, port.AxisZ3, port.Entry{
			ID:   id,
			At:   atOffset(i),
			Body: "body of " + id,
		}); err != nil {
			t.Fatalf("seed Z3 %s: %v", id, err)
		}
	}
}

func readZ2(t *testing.T, target port.Port) []port.Entry {
	t.Helper()
	entries, err := target.Read(context.Background(), port.ScopeUser, port.AxisZ2, MaxLimit)
	if err != nil {
		t.Fatalf("read Z2: %v", err)
	}
	return entries
}

func TestPromoteMatrix(t *testing.T) {
	for _, name := range []string{"fake", "store"} {
		t.Run(name, func(t *testing.T) {
			t.Run("missing_id_fails_the_whole_batch", func(t *testing.T) {
				d := promotionDoubleNamed(t, name)
				seedZ3(t, d.source, "a", "b", "c")
				_, err := Promote(context.Background(), d.source, d.target, []string{"a", "ghost", "c"})
				if !errors.Is(err, port.ErrNotFound) {
					t.Fatalf("promoting a batch with a missing id: got %v, want ErrNotFound", err)
				}
				if got := readZ2(t, d.target); len(got) != 0 {
					t.Fatalf("a failed batch wrote %d entries to Z2, want 0: %v", len(got), got)
				}
			})

			t.Run("down_z2_writes_nothing", func(t *testing.T) {
				d := promotionDoubleNamed(t, name)
				seedZ3(t, d.source, "a", "b")
				d.setDown(true)
				_, err := Promote(context.Background(), d.source, d.downed(), []string{"a", "b"})
				d.setDown(false)
				if !errors.Is(err, port.ErrUnavailable) {
					t.Fatalf("promoting into an unavailable Z2: got %v, want ErrUnavailable", err)
				}
				if got := readZ2(t, d.target); len(got) != 0 {
					t.Fatalf("an unavailable Z2 received %d entries, want 0: %v", len(got), got)
				}
			})

			t.Run("a_cut_and_a_retry_converge", func(t *testing.T) {
				d := promotionDoubleNamed(t, name)
				seedZ3(t, d.source, "a", "b", "c")
				cut := d.cut(0)
				_, err := Promote(context.Background(), d.source, cut, []string{"a", "b", "c"})
				if err == nil {
					t.Fatal("the interrupted batch reported success")
				}
				if len(cut.appended) != 0 {
					t.Fatalf("the cut wrote %d entries (err %v), want 0: the batch is one transaction", len(cut.appended), err)
				}
				if got := readZ2(t, d.target); len(got) != 0 {
					t.Fatalf("the cut left %d entries in Z2, want 0: %v", len(got), got)
				}
				written, err := Promote(context.Background(), d.source, d.target, []string{"a", "b", "c"})
				if err != nil {
					t.Fatalf("the retry after a cut: %v", err)
				}
				if len(written) != 3 {
					t.Fatalf("the retry wrote %d entries, want 3", len(written))
				}
				seen := map[string]int{}
				for _, entry := range written {
					seen[entry.ID]++
					if entry.Attrs[attrPromotedFrom] == "" {
						t.Fatalf("%s has no promoted_from: %v", entry.ID, entry.Attrs)
					}
					if !strings.HasPrefix(entry.ID, "promoted:") {
						t.Fatalf("promoted id %q does not carry the promoted: prefix", entry.ID)
					}
				}
				for _, id := range []string{"promoted:a", "promoted:b", "promoted:c"} {
					if seen[id] != 1 {
						t.Fatalf("%s appears %d times, want exactly 1", id, seen[id])
					}
				}
				if got := readZ2(t, d.target); len(got) != 3 {
					t.Fatalf("Z2 holds %d entries after the retry, want exactly 3: %v", len(got), got)
				}
			})

			t.Run("an_occupied_id_with_another_body_is_refused", func(t *testing.T) {
				d := promotionDoubleNamed(t, name)
				seedZ3(t, d.source, "a")
				if _, err := d.target.Append(context.Background(), port.ScopeUser, port.AxisZ2, port.Entry{
					ID: "promoted:a", Body: "something else entirely",
				}); err != nil {
					t.Fatalf("seed the occupied id: %v", err)
				}
				_, err := Promote(context.Background(), d.source, d.target, []string{"a"})
				if !errors.Is(err, ErrPromotedConflict) {
					t.Fatalf("promoting onto an occupied id: got %v, want ErrPromotedConflict", err)
				}
				got := readZ2(t, d.target)
				if len(got) != 1 || got[0].Body != "something else entirely" {
					t.Fatalf("a rejected promotion changed Z2: %v", got)
				}
			})
		})
	}
}

func TestPromoteWithAZ3BiggerThanThePortPage(t *testing.T) {
	for _, name := range []string{"fake", "store"} {
		t.Run(name, func(t *testing.T) {
			d := promotionDoubleNamed(t, name)
			ids := make([]string, 0, 250)
			for i := 0; i < 250; i++ {
				ids = append(ids, fmt.Sprintf("z3-%03d", i))
			}
			seedZ3(t, d.source, ids...)
			batch := []string{"z3-240", "z3-000"}
			out, err := Promote(context.Background(), d.source, d.target, batch)
			if err != nil {
				t.Fatalf("Promote out of a Z3 of 250 through a %s source: got %v, want nil", name, err)
			}
			if len(out) != 2 {
				t.Fatalf("Promote promoted %d entries, want 2", len(out))
			}
			for i, want := range []string{PromotedID("z3-240"), PromotedID("z3-000")} {
				if out[i].ID != want {
					t.Fatalf("Promote order: entry %d is %q, want %q", i, out[i].ID, want)
				}
			}
			if got := readZ2(t, d.target); len(got) != 2 {
				t.Fatalf("Z2 holds %d entries, want exactly 2: %v", len(got), got)
			}
		})
	}
}