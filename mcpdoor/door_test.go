package mcpdoor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/JaviCss/eco/port"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type boomErr struct{}

func (boomErr) Error() string { return "boom" }

type spy struct {
	mu        sync.Mutex
	calls     []string
	fallback  *port.Fake
	readErr   error
	searchErr error
	appendErr error
	probeErr  error
}

func newSpy() *spy {
	return &spy{fallback: port.NewFake()}
}

func (s *spy) record(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, name)
}

func (s *spy) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.calls))
	copy(out, s.calls)
	return out
}

func (s *spy) Read(ctx context.Context, scope port.Scope, axis port.Axis, limit int) ([]port.Entry, error) {
	s.record("Read")
	if s.readErr != nil {
		return nil, s.readErr
	}
	return s.fallback.Read(ctx, scope, axis, limit)
}

func (s *spy) Search(ctx context.Context, scope port.Scope, axis port.Axis, query string, limit int) ([]port.Entry, error) {
	s.record("Search")
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	return s.fallback.Search(ctx, scope, axis, query, limit)
}

func (s *spy) Append(ctx context.Context, scope port.Scope, axis port.Axis, entry port.Entry) (port.Entry, error) {
	s.record("Append")
	if s.appendErr != nil {
		return port.Entry{}, s.appendErr
	}
	return s.fallback.Append(ctx, scope, axis, entry)
}

func (s *spy) Probe(ctx context.Context) error {
	s.record("Probe")
	if s.probeErr != nil {
		return s.probeErr
	}
	return s.fallback.Probe(ctx)
}

func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil {
		t.Fatal("nil tool result")
	}
	if len(res.Content) != 1 {
		t.Fatalf("content blocks = %d, want exactly 1", len(res.Content))
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] type = %T, want *mcp.TextContent", res.Content[0])
	}
	return tc.Text
}

func seedZ3(t *testing.T, fake *port.Fake, count, size int) []port.Entry {
	t.Helper()
	ctx := context.Background()
	body := strings.Repeat("z", size)
	want := make([]port.Entry, 0, count)
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("seed-%03d", i)
		entry, err := fake.Append(ctx, port.ScopeProject, port.AxisZ3, port.Entry{ID: id, Body: body})
		if err != nil {
			t.Fatalf("seed append %d: %v", i, err)
		}
		want = append(want, entry)
	}
	return want
}

func TestPayloadJSONCarriesEntryNames(t *testing.T) {
	fake := port.NewFake()
	seedZ3(t, fake, 1, 8)
	d := &door{client: fake}
	res := d.read(context.Background(), "Z3", 1)
	var decoded map[string]any
	if err := json.Unmarshal([]byte(textOf(t, res.result)), &decoded); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	entries, ok := decoded["entries"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("entries = %#v", decoded["entries"])
	}
	entry, ok := entries[0].(map[string]any)
	if !ok {
		t.Fatalf("entry is not an object: %#v", entries[0])
	}
	for _, key := range []string{"ID", "Axis", "Scope", "At", "Body", "Attrs"} {
		if _, ok := entry[key]; !ok {
			t.Fatalf("entry is missing key %q: %#v", key, entry)
		}
	}
}

func TestReadTruncatesWholeEntriesOnly(t *testing.T) {
	fake := port.NewFake()
	seeded := seedZ3(t, fake, 500, 1024)
	d := &door{client: fake}
	res := d.read(context.Background(), "Z3", maxLimit)
	text := textOf(t, res.result)
	t.Logf("truncated result bytes = %d", len(text))
	if len(text) > MaxToolResultBytes {
		t.Fatalf("result bytes = %d, want <= %d", len(text), MaxToolResultBytes)
	}
	var decoded entriesPayload
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if !decoded.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if decoded.Omitted <= 0 {
		t.Fatalf("omitted = %d, want > 0", decoded.Omitted)
	}
	if len(decoded.Entries)+decoded.Omitted != maxLimit {
		t.Fatalf("kept %d + omitted %d != %d", len(decoded.Entries), decoded.Omitted, maxLimit)
	}
	for i, entry := range decoded.Entries {
		if len(entry.Body) != 1024 {
			t.Fatalf("entries[%d].Body length = %d, want 1024 (entry was split)", i, len(entry.Body))
		}
	}
	if len(decoded.Entries) > 0 {
		first := decoded.Entries[0].ID
		found := false
		for _, entry := range seeded {
			if entry.ID == first {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("entries[0].ID = %q is not a seeded id", first)
		}
	}
}

func TestSingleOversizedEntryIsOmittedWhole(t *testing.T) {
	fake := port.NewFake()
	body := strings.Repeat("q", MaxToolResultBytes+2048)
	if _, err := fake.Append(context.Background(), port.ScopeProject, port.AxisZ3, port.Entry{ID: "huge", Body: body}); err != nil {
		t.Fatalf("append: %v", err)
	}
	d := &door{client: fake}
	text := textOf(t, d.read(context.Background(), "Z3", maxLimit).result)
	if len(text) > MaxToolResultBytes {
		t.Fatalf("result bytes = %d, want <= %d", len(text), MaxToolResultBytes)
	}
	var decoded entriesPayload
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if len(decoded.Entries) != 0 {
		t.Fatalf("entries = %d, want 0 (oversized entry must be omitted whole)", len(decoded.Entries))
	}
	if !decoded.Truncated || decoded.Omitted != 1 {
		t.Fatalf("truncated = %v, omitted = %d, want true and 1", decoded.Truncated, decoded.Omitted)
	}
}

func TestDoorRejectsZ2AppendBeforePort(t *testing.T) {
	s := newSpy()
	d := &door{client: s}
	res := d.append(context.Background(), "Z2", "abc", "body", nil)
	if !res.result.IsError {
		t.Fatal("IsError = false, want true")
	}
	if got := textOf(t, res.result); got != "eco: forbidden" {
		t.Fatalf("text = %q, want %q", got, "eco: forbidden")
	}
	if calls := s.recorded(); len(calls) != 0 {
		t.Fatalf("port calls = %v, want none", calls)
	}
}

func TestDoorRejectsUnknownAxesBeforePort(t *testing.T) {
	s := newSpy()
	d := &door{client: s}
	cases := []struct {
		call string
		run  func() toolResponse
	}{
		{"read R", func() toolResponse { return d.read(context.Background(), "R", 10) }},
		{"read V", func() toolResponse { return d.read(context.Background(), "V", 10) }},
		{"read Manifest", func() toolResponse { return d.read(context.Background(), "Manifest", 10) }},
		{"read lowercase z3", func() toolResponse { return d.read(context.Background(), "z3", 10) }},
		{"search R", func() toolResponse { return d.search(context.Background(), "R", "q", 10) }},
		{"append V", func() toolResponse { return d.append(context.Background(), "V", "id", "b", nil) }},
		{"append Manifest", func() toolResponse { return d.append(context.Background(), "Manifest", "id", "b", nil) }},
	}
	for _, tc := range cases {
		res := tc.run()
		if !res.result.IsError {
			t.Fatalf("%s: IsError = false, want true", tc.call)
		}
		if got := textOf(t, res.result); got != "eco: forbidden" {
			t.Fatalf("%s: text = %q, want %q", tc.call, got, "eco: forbidden")
		}
	}
	if calls := s.recorded(); len(calls) != 0 {
		t.Fatalf("port calls = %v, want none", calls)
	}
}

func TestErrorTextIsAlwaysASentinelName(t *testing.T) {
	sentinels := []struct {
		err  error
		text string
	}{
		{port.ErrNotFound, "eco: not found"},
		{port.ErrForbidden, "eco: forbidden"},
		{port.ErrInvalidEntry, "eco: invalid entry"},
		{port.ErrUnavailable, "eco: unavailable"},
		{port.ErrAxisNotInScope, "eco: axis not in scope"},
		{port.ErrInvalidScope, "eco: invalid scope"},
	}
	for _, sentinel := range sentinels {
		s := newSpy()
		s.readErr = fmt.Errorf("eco: read: C:/secret/base.db: %w", sentinel.err)
		d := &door{client: s}
		res := d.read(context.Background(), "Z3", 5)
		if !res.result.IsError {
			t.Fatalf("%v: IsError = false, want true", sentinel.err)
		}
		if got := textOf(t, res.result); got != sentinel.text {
			t.Fatalf("%v: text = %q, want %q", sentinel.err, got, sentinel.text)
		}
	}
}

func TestUnmappedErrorNeverLeaksRawText(t *testing.T) {
	s := newSpy()
	s.readErr = fmt.Errorf("open C:/secret/base.db: %w", boomErr{})
	s.searchErr = fmt.Errorf("open C:/secret/base.db: %w", boomErr{})
	s.appendErr = fmt.Errorf("write C:/secret/base.db: %w", boomErr{})
	d := &door{client: s}
	results := []toolResponse{
		d.read(context.Background(), "Z3", 5),
		d.search(context.Background(), "Z3", "q", 5),
		d.append(context.Background(), "Z3", "id", "body", nil),
	}
	for i, res := range results {
		if !res.result.IsError {
			t.Fatalf("result %d: IsError = false, want true", i)
		}
		text := textOf(t, res.result)
		if text != "eco: unavailable" {
			t.Fatalf("result %d: text = %q, want %q", i, text, "eco: unavailable")
		}
	}
}

func TestProbeCodesAreTyped(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"healthy", nil, "ok"},
		{"forbidden", port.ErrForbidden, "forbidden"},
		{"unavailable", port.ErrUnavailable, "unavailable"},
		{"unmapped", boomErr{}, "unavailable"},
	}
	for _, tc := range cases {
		s := newSpy()
		s.probeErr = tc.err
		d := &door{client: s}
		res := d.probe(context.Background())
		if res.result.IsError {
			t.Fatalf("%s: IsError = true, want false", tc.name)
		}
		var decoded probePayload
		if err := json.Unmarshal([]byte(textOf(t, res.result)), &decoded); err != nil {
			t.Fatalf("%s: payload is not JSON: %v", tc.name, err)
		}
		if decoded.Status != tc.code {
			t.Fatalf("%s: status = %q, want %q", tc.name, decoded.Status, tc.code)
		}
	}
}
