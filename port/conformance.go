package port

import (
	"context"
	"errors"
	"testing"
	"time"
)

func Conformance(t *testing.T, newImpl func() Port, newOutage func() Outage) {
	t.Helper()
	if newOutage == nil {
		t.Fatal("Conformance requires newOutage: without an outage switch the ErrUnavailable rules cannot run, and a suite that skips them reports a pass it did not earn")
	}
	t0 := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	at := func(offset time.Duration) time.Time { return t0.Add(offset) }

	t.Run("read_empty_returns_no_entries", func(t *testing.T) {
		entries, err := newImpl().Read(context.Background(), ScopeUser, AxisZ2, 10)
		if err != nil {
			t.Fatalf("Read on empty axis: got error %v, want nil", err)
		}
		if len(entries) != 0 {
			t.Fatalf("Read on empty axis: got %d entries, want 0", len(entries))
		}
	})

	t.Run("append_then_read_returns_the_entry", func(t *testing.T) {
		p := newImpl()
		stored, err := p.Append(context.Background(), ScopeUser, AxisZ2, Entry{
			ID:    "key-1",
			At:    at(0),
			Body:  "first entry",
			Attrs: map[string]string{"tool": "verifier"},
		})
		if err != nil {
			t.Fatalf("Append: got error %v, want nil", err)
		}
		if stored.Body != "first entry" {
			t.Fatalf("Append returned body %q, want %q", stored.Body, "first entry")
		}
		if stored.Axis != AxisZ2 || stored.Scope != ScopeUser {
			t.Fatalf("Append returned %s/%s, want %s/%s", stored.Scope, stored.Axis, ScopeUser, AxisZ2)
		}
		entries, err := p.Read(context.Background(), ScopeUser, AxisZ2, 10)
		if err != nil {
			t.Fatalf("Read after Append: got error %v, want nil", err)
		}
		if len(entries) != 1 {
			t.Fatalf("Read after Append: got %d entries, want 1", len(entries))
		}
		if entries[0].Attrs["tool"] != "verifier" {
			t.Fatalf("attrs round trip: got %v, want tool=verifier", entries[0].Attrs)
		}
	})

	t.Run("append_without_id_is_rejected", func(t *testing.T) {
		p := newImpl()
		_, err := p.Append(context.Background(), ScopeUser, AxisZ2, Entry{At: at(0), Body: "no key"})
		if !errors.Is(err, ErrInvalidEntry) {
			t.Fatalf("Append with an empty ID: got %v, want ErrInvalidEntry", err)
		}
		entries, readErr := p.Read(context.Background(), ScopeUser, AxisZ2, 10)
		if readErr != nil {
			t.Fatalf("Read after a rejected Append: got error %v, want nil", readErr)
		}
		if len(entries) != 0 {
			t.Fatalf("a rejected Append stored %d entries, want 0: %v", len(entries), entries)
		}
	})

	t.Run("append_same_key_twice_stores_one_entry", func(t *testing.T) {
		p := newImpl()
		for i := 0; i < 2; i++ {
			if _, err := p.Append(context.Background(), ScopeUser, AxisZ2, Entry{
				ID: "same-key", At: at(time.Duration(i)), Body: "once",
			}); err != nil {
				t.Fatalf("Append %d: got error %v, want nil", i+1, err)
			}
		}
		entries, err := p.Read(context.Background(), ScopeUser, AxisZ2, 10)
		if err != nil {
			t.Fatalf("Read after two Appends: got error %v, want nil", err)
		}
		if len(entries) != 1 {
			t.Fatalf("idempotency: got %d entries, want 1", len(entries))
		}
	})

	t.Run("append_same_key_twice_returns_the_same_entry", func(t *testing.T) {
		p := newImpl()
		first, err := p.Append(context.Background(), ScopeUser, AxisZ2, Entry{
			ID: "same-key", At: at(0), Body: "original",
		})
		if err != nil {
			t.Fatalf("first Append: got error %v, want nil", err)
		}
		second, err := p.Append(context.Background(), ScopeUser, AxisZ2, Entry{
			ID: "same-key", At: at(time.Hour), Body: "retry",
		})
		if err != nil {
			t.Fatalf("second Append: got error %v, want nil", err)
		}
		if second.ID != first.ID || second.Body != first.Body || second.At != first.At {
			t.Fatalf("retry returned %+v, want the stored %+v", second, first)
		}
	})

	t.Run("append_and_read_do_not_alias_attrs", func(t *testing.T) {
		p := newImpl()
		attrs := map[string]string{"tool": "verifier"}
		stored, err := p.Append(context.Background(), ScopeUser, AxisZ2, Entry{
			ID: "attrs", At: at(0), Body: "attrs", Attrs: attrs,
		})
		if err != nil {
			t.Fatalf("Append: got error %v, want nil", err)
		}
		attrs["tool"] = "mutated-by-caller"
		stored.Attrs["tool"] = "mutated-by-returned-entry"
		entries, err := p.Read(context.Background(), ScopeUser, AxisZ2, 10)
		if err != nil {
			t.Fatalf("Read after Append: got error %v, want nil", err)
		}
		if len(entries) != 1 {
			t.Fatalf("Read after Append: got %d entries, want 1", len(entries))
		}
		if entries[0].Attrs["tool"] != "verifier" {
			t.Fatalf("Append aliased the caller's Attrs into the store: got %v, want tool=verifier", entries[0].Attrs)
		}
		entries[0].Attrs["tool"] = "mutated-by-read"
		again, err := p.Read(context.Background(), ScopeUser, AxisZ2, 10)
		if err != nil {
			t.Fatalf("second Read: got error %v, want nil", err)
		}
		if len(again) != 1 {
			t.Fatalf("second Read: got %d entries, want 1", len(again))
		}
		if again[0].Attrs["tool"] != "verifier" {
			t.Fatalf("Read aliased the store's Attrs into the caller: got %v, want tool=verifier", again[0].Attrs)
		}
	})

	t.Run("at_is_stored_and_returned_as_utc", func(t *testing.T) {
		p := newImpl()
		want := time.Date(2026, 10, 7, 12, 0, 0, 0, time.FixedZone("minus-three", -3*60*60))
		if _, err := p.Append(context.Background(), ScopeUser, AxisZ2, Entry{
			ID: "tz", At: want, Body: "zone",
		}); err != nil {
			t.Fatalf("Append: got error %v, want nil", err)
		}
		entries, err := p.Read(context.Background(), ScopeUser, AxisZ2, 10)
		if err != nil {
			t.Fatalf("Read after Append: got error %v, want nil", err)
		}
		if len(entries) != 1 {
			t.Fatalf("Read after Append: got %d entries, want 1", len(entries))
		}
		got := entries[0].At
		if got.Location() != time.UTC {
			t.Fatalf("At came back in zone %q, want UTC", got.Location())
		}
		if got.UnixNano() != want.UnixNano() {
			t.Fatalf("At came back as %d ns, want %d ns", got.UnixNano(), want.UnixNano())
		}
	})

	t.Run("read_orders_newest_first", func(t *testing.T) {
		p := newImpl()
		for _, offset := range []time.Duration{0, 2 * time.Hour, time.Hour} {
			if _, err := p.Append(context.Background(), ScopeUser, AxisZ2, Entry{
				ID: string(rune('a' + offset/time.Hour)), At: at(offset), Body: "ordered",
			}); err != nil {
				t.Fatalf("Append: got error %v, want nil", err)
			}
		}
		entries, err := p.Read(context.Background(), ScopeUser, AxisZ2, 10)
		if err != nil {
			t.Fatalf("Read: got error %v, want nil", err)
		}
		if len(entries) != 3 {
			t.Fatalf("Read: got %d entries, want 3", len(entries))
		}
		for i := 1; i < len(entries); i++ {
			if entries[i].At.After(entries[i-1].At) {
				t.Fatalf("order: entry %d is newer than entry %d: %v", i, i-1, entries)
			}
		}
	})

	t.Run("read_honors_limit", func(t *testing.T) {
		p := newImpl()
		for i := 0; i < 4; i++ {
			if _, err := p.Append(context.Background(), ScopeUser, AxisZ2, Entry{
				ID: string(rune('a' + i)), At: at(time.Duration(i) * time.Hour), Body: "limited",
			}); err != nil {
				t.Fatalf("Append: got error %v, want nil", err)
			}
		}
		entries, err := p.Read(context.Background(), ScopeUser, AxisZ2, 2)
		if err != nil {
			t.Fatalf("Read with limit: got error %v, want nil", err)
		}
		if len(entries) != 2 {
			t.Fatalf("Read with limit 2: got %d entries, want 2", len(entries))
		}
		if entries[0].At.Before(entries[1].At) {
			t.Fatalf("Read with limit: the newest entry must come first, got %v", entries)
		}
	})

	t.Run("append_axis_out_of_scope_is_rejected", func(t *testing.T) {
		p := newImpl()
		_, err := p.Append(context.Background(), ScopeProject, AxisR, Entry{ID: "k", At: at(0), Body: "b"})
		if !errors.Is(err, ErrAxisNotInScope) {
			t.Fatalf("Append R on Project: got %v, want ErrAxisNotInScope", err)
		}
		if _, err := p.Read(context.Background(), ScopeProject, AxisR, 10); !errors.Is(err, ErrAxisNotInScope) {
			t.Fatalf("Read R on Project: got %v, want ErrAxisNotInScope", err)
		}
		if _, err := p.Search(context.Background(), ScopeProject, AxisR, "b", 10); !errors.Is(err, ErrAxisNotInScope) {
			t.Fatalf("Search R on Project: got %v, want ErrAxisNotInScope", err)
		}
	})

	t.Run("invalid_scope_is_rejected", func(t *testing.T) {
		p := newImpl()
		if _, err := p.Read(context.Background(), Scope("kit"), AxisZ2, 10); !errors.Is(err, ErrInvalidScope) {
			t.Fatalf("Read on invalid scope: got %v, want ErrInvalidScope", err)
		}
		if _, err := p.Append(context.Background(), Scope(""), AxisZ2, Entry{ID: "k"}); !errors.Is(err, ErrInvalidScope) {
			t.Fatalf("Append on invalid scope: got %v, want ErrInvalidScope", err)
		}
		if _, err := p.Search(context.Background(), Scope("release"), AxisZ2, "b", 10); !errors.Is(err, ErrInvalidScope) {
			t.Fatalf("Search on invalid scope: got %v, want ErrInvalidScope", err)
		}
	})

	t.Run("validation_wins_over_cancelled_context", func(t *testing.T) {
		p := newImpl()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := p.Read(ctx, Scope("kit"), AxisZ2, 10); !errors.Is(err, ErrInvalidScope) {
			t.Fatalf("Read on a cancelled context and an invalid scope: got %v, want ErrInvalidScope", err)
		}
		if _, err := p.Append(ctx, ScopeProject, AxisR, Entry{ID: "k", At: at(0), Body: "b"}); !errors.Is(err, ErrAxisNotInScope) {
			t.Fatalf("Append on a cancelled context and an axis out of scope: got %v, want ErrAxisNotInScope", err)
		}
		if _, err := p.Search(ctx, Scope("kit"), AxisZ2, "b", 10); !errors.Is(err, ErrInvalidScope) {
			t.Fatalf("Search on a cancelled context and an invalid scope: got %v, want ErrInvalidScope", err)
		}
	})

	t.Run("cancelled_context_wins_over_outage", func(t *testing.T) {
		p := newOutage()
		p.SetUnavailable(true)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := p.Probe(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("Probe on a cancelled context during an outage: got %v, want context.Canceled", err)
		}
		if _, err := p.Read(ctx, ScopeUser, AxisZ2, 10); !errors.Is(err, context.Canceled) {
			t.Fatalf("Read on a cancelled context during an outage: got %v, want context.Canceled", err)
		}
		if _, err := p.Append(ctx, ScopeUser, AxisZ2, Entry{ID: "k", At: at(0), Body: "b"}); !errors.Is(err, context.Canceled) {
			t.Fatalf("Append on a cancelled context during an outage: got %v, want context.Canceled", err)
		}
		if _, err := p.Search(ctx, ScopeUser, AxisZ2, "b", 10); !errors.Is(err, context.Canceled) {
			t.Fatalf("Search on a cancelled context during an outage: got %v, want context.Canceled", err)
		}
	})

	t.Run("validation_wins_over_outage", func(t *testing.T) {
		p := newOutage()
		p.SetUnavailable(true)
		if _, err := p.Read(context.Background(), Scope("kit"), AxisZ2, 10); !errors.Is(err, ErrInvalidScope) {
			t.Fatalf("Read on an invalid scope during an outage: got %v, want ErrInvalidScope", err)
		}
		if _, err := p.Append(context.Background(), ScopeProject, AxisR, Entry{ID: "k", At: at(0), Body: "b"}); !errors.Is(err, ErrAxisNotInScope) {
			t.Fatalf("Append on an axis out of scope during an outage: got %v, want ErrAxisNotInScope", err)
		}
		if _, err := p.Search(context.Background(), Scope("kit"), AxisZ2, "b", 10); !errors.Is(err, ErrInvalidScope) {
			t.Fatalf("Search on an invalid scope during an outage: got %v, want ErrInvalidScope", err)
		}
	})

	t.Run("search_finds_body_substring", func(t *testing.T) {
		p := newImpl()
		if _, err := p.Append(context.Background(), ScopeUser, AxisZ2, Entry{
			ID: "hit", At: at(0), Body: "the runtime fell over",
		}); err != nil {
			t.Fatalf("Append: got error %v, want nil", err)
		}
		hits, err := p.Search(context.Background(), ScopeUser, AxisZ2, "RUNTIME FELL", 10)
		if err != nil {
			t.Fatalf("Search: got error %v, want nil", err)
		}
		if len(hits) != 1 || hits[0].ID != "hit" {
			t.Fatalf("Search: got %v, want one hit with id hit", hits)
		}
	})

	t.Run("search_without_match_returns_not_found", func(t *testing.T) {
		p := newImpl()
		if _, err := p.Append(context.Background(), ScopeUser, AxisZ2, Entry{
			ID: "present", At: at(0), Body: "the runtime fell over",
		}); err != nil {
			t.Fatalf("Append: got error %v, want nil", err)
		}
		_, err := p.Search(context.Background(), ScopeUser, AxisZ2, "no such datum", 10)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("Search without match: got %v, want ErrNotFound", err)
		}
		if errors.Is(err, ErrUnavailable) {
			t.Fatalf("ErrNotFound must stay distinguishable from ErrUnavailable: %v", err)
		}
	})

	t.Run("probe_reports_healthy", func(t *testing.T) {
		if err := newImpl().Probe(context.Background()); err != nil {
			t.Fatalf("Probe on a live implementation: got %v, want nil", err)
		}
	})

	t.Run("unavailable_makes_every_operation_report_unavailable", func(t *testing.T) {
		p := newOutage()
		if _, err := p.Append(context.Background(), ScopeUser, AxisZ2, Entry{ID: "before", At: at(0), Body: "kept"}); err != nil {
			t.Fatalf("Append before outage: got error %v, want nil", err)
		}
		p.SetUnavailable(true)
		if err := p.Probe(context.Background()); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("Probe during outage: got %v, want ErrUnavailable", err)
		}
		if _, err := p.Read(context.Background(), ScopeUser, AxisZ2, 10); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("Read during outage: got %v, want ErrUnavailable", err)
		}
		if _, err := p.Append(context.Background(), ScopeUser, AxisZ2, Entry{ID: "during", At: at(time.Hour), Body: "dropped"}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("Append during outage: got %v, want ErrUnavailable", err)
		}
		if _, err := p.Search(context.Background(), ScopeUser, AxisZ2, "kept", 10); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("Search during outage: got %v, want ErrUnavailable", err)
		}
		p.SetUnavailable(false)
		entries, err := p.Read(context.Background(), ScopeUser, AxisZ2, 10)
		if err != nil {
			t.Fatalf("Read after outage: got error %v, want nil", err)
		}
		if len(entries) != 1 || entries[0].ID != "before" {
			t.Fatalf("outage must not touch the store: got %v, want only the entry written before it", entries)
		}
	})

	t.Run("not_found_and_unavailable_stay_distinct", func(t *testing.T) {
		p := newOutage()
		_, notFound := p.Search(context.Background(), ScopeUser, AxisZ2, "no such datum", 10)
		p.SetUnavailable(true)
		_, unavailable := p.Search(context.Background(), ScopeUser, AxisZ2, "no such datum", 10)
		p.SetUnavailable(false)
		if !errors.Is(notFound, ErrNotFound) {
			t.Fatalf("healthy miss: got %v, want ErrNotFound", notFound)
		}
		if !errors.Is(unavailable, ErrUnavailable) {
			t.Fatalf("outage miss: got %v, want ErrUnavailable", unavailable)
		}
		if errors.Is(notFound, ErrUnavailable) {
			t.Fatalf("a healthy miss must not report an outage: %v", notFound)
		}
		if errors.Is(unavailable, ErrNotFound) {
			t.Fatalf("an outage must not report a missing datum: %v", unavailable)
		}
	})

	t.Run("cancelled_context_is_respected_and_touches_nothing", func(t *testing.T) {
		p := newImpl()
		if _, err := p.Append(context.Background(), ScopeUser, AxisZ2, Entry{ID: "kept", At: at(0), Body: "kept"}); err != nil {
			t.Fatalf("Append before cancel: got error %v, want nil", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := p.Probe(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("Probe on cancelled context: got %v, want context.Canceled", err)
		}
		if _, err := p.Read(ctx, ScopeUser, AxisZ2, 10); !errors.Is(err, context.Canceled) {
			t.Fatalf("Read on cancelled context: got %v, want context.Canceled", err)
		}
		if _, err := p.Append(ctx, ScopeUser, AxisZ2, Entry{ID: "lost", At: at(time.Hour), Body: "lost"}); !errors.Is(err, context.Canceled) {
			t.Fatalf("Append on cancelled context: got %v, want context.Canceled", err)
		}
		if _, err := p.Search(ctx, ScopeUser, AxisZ2, "kept", 10); !errors.Is(err, context.Canceled) {
			t.Fatalf("Search on cancelled context: got %v, want context.Canceled", err)
		}
		entries, err := p.Read(context.Background(), ScopeUser, AxisZ2, 10)
		if err != nil {
			t.Fatalf("Read after cancel: got error %v, want nil", err)
		}
		if len(entries) != 1 || entries[0].ID != "kept" {
			t.Fatalf("a cancelled context must not modify the store, got %v", entries)
		}
	})

	t.Run("scopes_are_separate_stores", func(t *testing.T) {
		p := newImpl()
		if _, err := p.Append(context.Background(), ScopeUser, AxisZ2, Entry{ID: "u", At: at(0), Body: "user"}); err != nil {
			t.Fatalf("Append on User: got error %v, want nil", err)
		}
		if _, err := p.Append(context.Background(), ScopeProject, AxisZ3, Entry{ID: "p", At: at(0), Body: "project"}); err != nil {
			t.Fatalf("Append on Project: got error %v, want nil", err)
		}
		user, err := p.Read(context.Background(), ScopeUser, AxisZ2, 10)
		if err != nil {
			t.Fatalf("Read User: got error %v, want nil", err)
		}
		project, err := p.Read(context.Background(), ScopeProject, AxisZ3, 10)
		if err != nil {
			t.Fatalf("Read Project: got error %v, want nil", err)
		}
		if len(user) != 1 || user[0].ID != "u" {
			t.Fatalf("user base: got %v, want only u", user)
		}
		if len(project) != 1 || project[0].ID != "p" {
			t.Fatalf("project base: got %v, want only p", project)
		}
	})

	t.Run("get_returns_the_entries_in_the_order_requested", func(t *testing.T) {
		p := newImpl()
		for _, id := range []string{"a", "b", "c"} {
			if _, err := p.Append(context.Background(), ScopeUser, AxisZ2, Entry{
				ID: id, At: at(time.Duration(len(id)) * time.Hour), Body: "body of " + id,
			}); err != nil {
				t.Fatalf("Append %s: got error %v, want nil", id, err)
			}
		}
		entries, err := p.Get(context.Background(), ScopeUser, AxisZ2, []string{"c", "a", "b"})
		if err != nil {
			t.Fatalf("Get: got error %v, want nil", err)
		}
		if len(entries) != 3 {
			t.Fatalf("Get of 3 ids: got %d entries, want 3", len(entries))
		}
		for i, want := range []string{"c", "a", "b"} {
			if entries[i].ID != want {
				t.Fatalf("Get order: entry %d is %q, want %q: %v", i, entries[i].ID, want, entries)
			}
		}
		if entries[0].Body != "body of c" || entries[0].Axis != AxisZ2 || entries[0].Scope != ScopeUser {
			t.Fatalf("Get returned %+v, want the whole entry of c", entries[0])
		}
	})

	t.Run("get_with_a_missing_id_fails_the_whole_batch", func(t *testing.T) {
		p := newImpl()
		for _, id := range []string{"a", "b"} {
			if _, err := p.Append(context.Background(), ScopeUser, AxisZ2, Entry{
				ID: id, At: at(time.Hour), Body: "body of " + id,
			}); err != nil {
				t.Fatalf("Append %s: got error %v, want nil", id, err)
			}
		}
		entries, err := p.Get(context.Background(), ScopeUser, AxisZ2, []string{"a", "ghost", "b"})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get with a missing id: got %v, want ErrNotFound", err)
		}
		if errors.Is(err, ErrUnavailable) {
			t.Fatalf("a missing id must not report an outage: %v", err)
		}
		if entries != nil {
			t.Fatalf("Get with a missing id returned a partial result: %v, want none", entries)
		}
	})

	t.Run("get_with_an_empty_batch_is_rejected", func(t *testing.T) {
		p := newImpl()
		if _, err := p.Get(context.Background(), ScopeUser, AxisZ2, nil); !errors.Is(err, ErrInvalidEntry) {
			t.Fatalf("Get with no ids: got %v, want ErrInvalidEntry", err)
		}
		if _, err := p.Get(context.Background(), ScopeUser, AxisZ2, []string{}); !errors.Is(err, ErrInvalidEntry) {
			t.Fatalf("Get with an empty batch: got %v, want ErrInvalidEntry", err)
		}
	})

	t.Run("get_with_a_batch_over_the_ceiling_is_rejected", func(t *testing.T) {
		p := newImpl()
		ids := make([]string, MaxBatch+1)
		for i := range ids {
			ids[i] = "id-" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26))
		}
		if _, err := p.Get(context.Background(), ScopeUser, AxisZ2, ids); !errors.Is(err, ErrInvalidEntry) {
			t.Fatalf("Get with %d ids: got %v, want ErrInvalidEntry", len(ids), err)
		}
	})

	t.Run("get_on_an_axis_out_of_scope_is_rejected", func(t *testing.T) {
		p := newImpl()
		if _, err := p.Get(context.Background(), ScopeProject, AxisR, []string{"k"}); !errors.Is(err, ErrAxisNotInScope) {
			t.Fatalf("Get R on Project: got %v, want ErrAxisNotInScope", err)
		}
		if _, err := p.Get(context.Background(), Scope("kit"), AxisZ2, []string{"k"}); !errors.Is(err, ErrInvalidScope) {
			t.Fatalf("Get on an invalid scope: got %v, want ErrInvalidScope", err)
		}
	})

	t.Run("get_validation_wins_over_a_cancelled_context", func(t *testing.T) {
		p := newImpl()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := p.Get(ctx, Scope("kit"), AxisZ2, []string{"k"}); !errors.Is(err, ErrInvalidScope) {
			t.Fatalf("Get on a cancelled context and an invalid scope: got %v, want ErrInvalidScope", err)
		}
		if _, err := p.Get(ctx, ScopeUser, AxisZ2, nil); !errors.Is(err, ErrInvalidEntry) {
			t.Fatalf("Get on a cancelled context and an empty batch: got %v, want ErrInvalidEntry", err)
		}
	})

	t.Run("get_on_a_cancelled_context_wins_over_an_outage", func(t *testing.T) {
		p := newOutage()
		p.SetUnavailable(true)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := p.Get(ctx, ScopeUser, AxisZ2, []string{"k"}); !errors.Is(err, context.Canceled) {
			t.Fatalf("Get on a cancelled context during an outage: got %v, want context.Canceled", err)
		}
	})

	t.Run("get_during_an_outage_reports_unavailable", func(t *testing.T) {
		p := newOutage()
		if _, err := p.Append(context.Background(), ScopeUser, AxisZ2, Entry{
			ID: "kept", At: at(0), Body: "kept",
		}); err != nil {
			t.Fatalf("Append before outage: got error %v, want nil", err)
		}
		p.SetUnavailable(true)
		if _, err := p.Get(context.Background(), ScopeUser, AxisZ2, []string{"kept"}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("Get during outage: got %v, want ErrUnavailable", err)
		}
		p.SetUnavailable(false)
		entries, err := p.Get(context.Background(), ScopeUser, AxisZ2, []string{"kept"})
		if err != nil {
			t.Fatalf("Get after outage: got error %v, want nil", err)
		}
		if len(entries) != 1 || entries[0].ID != "kept" {
			t.Fatalf("outage must not touch the store: got %v, want the entry written before it", entries)
		}
	})

	t.Run("get_touches_nothing_on_a_cancelled_context", func(t *testing.T) {
		p := newImpl()
		if _, err := p.Append(context.Background(), ScopeUser, AxisZ2, Entry{
			ID: "kept", At: at(0), Body: "kept",
		}); err != nil {
			t.Fatalf("Append before cancel: got error %v, want nil", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := p.Get(ctx, ScopeUser, AxisZ2, []string{"kept"}); !errors.Is(err, context.Canceled) {
			t.Fatalf("Get on cancelled context: got %v, want context.Canceled", err)
		}
	})
}