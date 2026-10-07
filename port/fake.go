package port

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Fake struct {
	mu          sync.RWMutex
	stores      map[Scope]map[Axis][]Entry
	unavailable bool
}

func NewFake() *Fake {
	return &Fake{stores: map[Scope]map[Axis][]Entry{
		ScopeUser:    {},
		ScopeProject: {},
	}}
}

func (f *Fake) SetUnavailable(unavailable bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unavailable = unavailable
}

func (f *Fake) down(op string) error {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.unavailable {
		return fmt.Errorf("eco: %s: %w", op, ErrUnavailable)
	}
	return nil
}

func cloneAttrs(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	out := make(map[string]string, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func cloneEntry(entry Entry) Entry {
	entry.Attrs = cloneAttrs(entry.Attrs)
	return entry
}

func cloneEntries(entries []Entry) []Entry {
	out := make([]Entry, len(entries))
	for i, entry := range entries {
		out[i] = cloneEntry(entry)
	}
	return out
}

func normalizeAt(at time.Time) time.Time {
	return time.Unix(0, at.UnixNano()).UTC()
}

func (f *Fake) Probe(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.down("Probe")
}

func (f *Fake) Read(ctx context.Context, scope Scope, axis Axis, limit int) ([]Entry, error) {
	if err := checkTarget(scope, axis); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := f.down("Read"); err != nil {
		return nil, err
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	return cloneEntries(sortedNewest(f.stores[scope][axis], limit)), nil
}

func (f *Fake) Append(ctx context.Context, scope Scope, axis Axis, entry Entry) (Entry, error) {
	if err := checkTarget(scope, axis); err != nil {
		return Entry{}, err
	}
	if entry.ID == "" {
		return Entry{}, fmt.Errorf("eco: Append: %w: empty id", ErrInvalidEntry)
	}
	if err := ctx.Err(); err != nil {
		return Entry{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.unavailable {
		return Entry{}, fmt.Errorf("eco: Append: %w", ErrUnavailable)
	}
	store := f.stores[scope][axis]
	for _, existing := range store {
		if existing.ID == entry.ID {
			return cloneEntry(existing), nil
		}
	}
	entry.Axis = axis
	entry.Scope = scope
	if entry.At.IsZero() {
		entry.At = time.Now()
	}
	entry.At = normalizeAt(entry.At)
	f.stores[scope][axis] = append(store, cloneEntry(entry))
	return cloneEntry(entry), nil
}

func (f *Fake) Search(ctx context.Context, scope Scope, axis Axis, query string, limit int) ([]Entry, error) {
	if err := checkTarget(scope, axis); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := f.down("Search"); err != nil {
		return nil, err
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	needle := strings.ToLower(query)
	hits := make([]Entry, 0)
	for _, entry := range sortedNewest(f.stores[scope][axis], 0) {
		if strings.Contains(strings.ToLower(entry.Body), needle) {
			hits = append(hits, cloneEntry(entry))
		}
	}
	if len(hits) == 0 {
		return nil, fmt.Errorf("eco: Search: %w: %q in %s/%s", ErrNotFound, query, scope, axis)
	}
	return limitTo(hits, limit), nil
}