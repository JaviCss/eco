package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JaviCss/eco/port"
)

func atOffset(n int) time.Time {
	return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC).Add(time.Duration(n) * time.Minute)
}

type promoterHarness struct {
	targets map[port.Scope]port.Port
}

func newHarness(t *testing.T) *promoterHarness {
	t.Helper()
	return &promoterHarness{targets: map[port.Scope]port.Port{
		port.ScopeUser:    newStoreAt(t, t.TempDir()),
		port.ScopeProject: port.NewFake(),
	}}
}

func (h *promoterHarness) fillZ3(t *testing.T, ids ...string) {
	t.Helper()
	z3 := h.targets[port.ScopeProject]
	for i, id := range ids {
		_, err := z3.Append(context.Background(), port.ScopeProject, port.AxisZ3, port.Entry{
			ID:   id,
			At:   atOffset(i),
			Body: "body of " + id,
		})
		if err != nil {
			t.Fatalf("seed Z3 %s: %v", id, err)
		}
	}
}

func (h *promoterHarness) promote(t *testing.T, ids ...string) ([]port.Entry, error) {
	t.Helper()
	return Promote(context.Background(), h.targets[port.ScopeProject], h.targets[port.ScopeUser], ids)
}

func (h *promoterHarness) z2(t *testing.T) []port.Entry {
	t.Helper()
	entries, err := h.targets[port.ScopeUser].Read(context.Background(), port.ScopeUser, port.AxisZ2, MaxLimit)
	if err != nil {
		t.Fatalf("read Z2: %v", err)
	}
	return entries
}

func TestPromoteMissingIDFailsTheWholeBatch(t *testing.T) {
	h := newHarness(t)
	h.fillZ3(t, "a", "b", "c")
	_, err := h.promote(t, "a", "ghost", "c")
	if !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("promoting a batch with a missing id: got %v, want ErrNotFound", err)
	}
	if got := h.z2(t); len(got) != 0 {
		t.Fatalf("a failed batch wrote %d entries to Z2, want 0: %v", len(got), got)
	}
}

func TestPromoteUnavailableTargetWritesNothing(t *testing.T) {
	h := newHarness(t)
	h.fillZ3(t, "a", "b")
	outage := &outageStore{inner: newStoreAt(t, t.TempDir())}
	outage.SetUnavailable(true)
	_, err := Promote(context.Background(), h.targets[port.ScopeProject], outage, []string{"a", "b"})
	if !errors.Is(err, port.ErrUnavailable) {
		t.Fatalf("promoting into an unavailable Z2: got %v, want ErrUnavailable", err)
	}
	outage.SetUnavailable(false)
	entries, err := outage.Read(context.Background(), port.ScopeUser, port.AxisZ2, MaxLimit)
	if err != nil {
		t.Fatalf("read Z2 after the outage: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("an unavailable Z2 received %d entries, want 0: %v", len(entries), entries)
	}
}

func TestPromoteRejectsIDOccupiedByAnotherBody(t *testing.T) {
	dir := t.TempDir()
	source := newStoreAt(t, dir)
	target := newStoreAt(t, t.TempDir())
	if _, err := source.Append(context.Background(), port.ScopeProject, port.AxisZ3, port.Entry{ID: "a", Body: "the distilled truth"}); err != nil {
		t.Fatalf("seed Z3: %v", err)
	}
	if _, err := target.Append(context.Background(), port.ScopeUser, port.AxisZ2, port.Entry{ID: "promoted:a", Body: "something else entirely"}); err != nil {
		t.Fatalf("seed the occupied id: %v", err)
	}
	_, err := Promote(context.Background(), source, target, []string{"a"})
	if !errors.Is(err, ErrPromotedConflict) {
		t.Fatalf("promoting onto an occupied id: got %v, want ErrPromotedConflict", err)
	}
	entries, err := target.Read(context.Background(), port.ScopeUser, port.AxisZ2, MaxLimit)
	if err != nil {
		t.Fatalf("read Z2: %v", err)
	}
	if len(entries) != 1 || entries[0].Body != "something else entirely" {
		t.Fatalf("a rejected promotion changed Z2: %v", entries)
	}
}

func TestPromoteIsIdempotent(t *testing.T) {
	h := newHarness(t)
	h.fillZ3(t, "a", "b")
	first, err := h.promote(t, "a", "b")
	if err != nil {
		t.Fatalf("first promotion: %v", err)
	}
	second, err := h.promote(t, "a", "b")
	if err != nil {
		t.Fatalf("second promotion: %v", err)
	}
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("promotion returned %d then %d entries, want 2 and 2", len(first), len(second))
	}
	if got := h.z2(t); len(got) != 2 {
		t.Fatalf("two promotions of the same batch left %d entries, want 2: %v", len(got), got)
	}
}

func TestStoreSearchTreatsQueryAsLiteralText(t *testing.T) {
	store := newStoreAt(t, t.TempDir())
	cases := []struct {
		name  string
		body  string
		query string
	}{
		{"and_is_text", "the a AND b of it", "a AND b"},
		{"near_is_text", "the NEAR(a b) of it", "NEAR(a b)"},
		{"star_is_text", "the a*b of it", "a*b"},
		{"quotes_are_text", `the a "b" c of it`, `a "b" c`},
		{"trigram_finds_substring", "the runtime fell over", "untime"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStoreAt(t, t.TempDir())
			if _, err := s.Append(context.Background(), port.ScopeUser, port.AxisZ2, port.Entry{ID: "literal", Body: tc.body}); err != nil {
				t.Fatalf("seed: %v", err)
			}
			if _, err := s.Append(context.Background(), port.ScopeUser, port.AxisZ2, port.Entry{ID: "decoy", Body: "a b c of it"}); err != nil {
				t.Fatalf("seed decoy: %v", err)
			}
			hits, err := s.Search(context.Background(), port.ScopeUser, port.AxisZ2, tc.query, 10)
			if err != nil {
				t.Fatalf("Search %q: got %v, want nil", tc.query, err)
			}
			if len(hits) != 1 || hits[0].ID != "literal" {
				t.Fatalf("Search %q: got %v, want only the literal entry", tc.query, hits)
			}
		})
	}
	_ = store
}

func TestStoreSearchShortQueryIsNotFound(t *testing.T) {
	s := newStoreAt(t, t.TempDir())
	if _, err := s.Append(context.Background(), port.ScopeUser, port.AxisZ2, port.Entry{ID: "x", Body: "abc def"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for _, query := range []string{"", "a", "ab"} {
		_, err := s.Search(context.Background(), port.ScopeUser, port.AxisZ2, query, 10)
		if !errors.Is(err, port.ErrNotFound) {
			t.Fatalf("Search %q: got %v, want ErrNotFound", query, err)
		}
		if errors.Is(err, port.ErrUnavailable) {
			t.Fatalf("Search %q reported an outage: %v", query, err)
		}
	}
}

func TestStoreProvenanceAndLimits(t *testing.T) {
	t.Run("at_follows_the_judge_not_the_criterion", func(t *testing.T) {
		s := newStoreAt(t, t.TempDir())
		given := atOffset(-100000)
		stored, err := s.Append(context.Background(), port.ScopeUser, port.AxisZ2, port.Entry{ID: "at-given", At: given, Body: "b"})
		if err != nil {
			t.Fatalf("Append with an explicit At: %v", err)
		}
		if stored.At.UnixNano() != given.UnixNano() {
			t.Fatalf("the Store rewrote a caller At: got %v, want %v (at_is_stored_and_returned_as_utc is the judge)", stored.At, given)
		}
		if got := stored.Attrs[attrOrigin]; got != "runtime" {
			t.Fatalf("origin = %q, want runtime", got)
		}
		blank, err := s.Append(context.Background(), port.ScopeUser, port.AxisZ2, port.Entry{ID: "at-blank", Body: "b"})
		if err != nil {
			t.Fatalf("Append with a zero At: %v", err)
		}
		if blank.At.IsZero() {
			t.Fatal("a zero At must be stamped with the Store's own clock")
		}
	})

	t.Run("reserved_attrs_from_the_caller_are_rejected", func(t *testing.T) {
		s := newStoreAt(t, t.TempDir())
		for _, key := range []string{attrOrigin, attrPromotedFrom, attrSourceOrigin} {
			_, err := s.Append(context.Background(), port.ScopeUser, port.AxisZ2, port.Entry{
				ID: "res-" + key, Body: "b", Attrs: map[string]string{key: "forged"},
			})
			if !errors.Is(err, port.ErrInvalidEntry) {
				t.Fatalf("Append with a caller-supplied %q: got %v, want ErrInvalidEntry", key, err)
			}
		}
	})

	t.Run("body_and_attrs_limits", func(t *testing.T) {
		s := newStoreAt(t, t.TempDir())
		_, err := s.Append(context.Background(), port.ScopeUser, port.AxisZ2, port.Entry{
			ID: "fat", Body: strings.Repeat("x", MaxBodyBytes+1),
		})
		if !errors.Is(err, port.ErrInvalidEntry) {
			t.Fatalf("a body of %d bytes: got %v, want ErrInvalidEntry", MaxBodyBytes+1, err)
		}
		many := map[string]string{}
		for i := 0; i < MaxAttrs+1; i++ {
			many[fmt.Sprintf("k%02d", i)] = "v"
		}
		_, err = s.Append(context.Background(), port.ScopeUser, port.AxisZ2, port.Entry{ID: "wide", Body: "b", Attrs: many})
		if !errors.Is(err, port.ErrInvalidEntry) {
			t.Fatalf("%d attrs: got %v, want ErrInvalidEntry", len(many), err)
		}
	})

	t.Run("limit_out_of_range", func(t *testing.T) {
		s := newStoreAt(t, t.TempDir())
		if _, err := s.Append(context.Background(), port.ScopeUser, port.AxisZ2, port.Entry{ID: "l", Body: "b"}); err != nil {
			t.Fatalf("seed: %v", err)
		}
		for _, limit := range []int{0, -1, MaxLimit + 1} {
			if _, err := s.Read(context.Background(), port.ScopeUser, port.AxisZ2, limit); !errors.Is(err, port.ErrInvalidEntry) {
				t.Fatalf("Read with limit %d: got %v, want ErrInvalidEntry", limit, err)
			}
			if _, err := s.Search(context.Background(), port.ScopeUser, port.AxisZ2, "abc", limit); !errors.Is(err, port.ErrInvalidEntry) {
				t.Fatalf("Search with limit %d: got %v, want ErrInvalidEntry", limit, err)
			}
		}
	})
}

func TestStoreDefaultIDIsAppliedByTheStore(t *testing.T) {
	s := newStoreAt(t, t.TempDir())
	if _, err := s.Append(context.Background(), port.ScopeUser, port.AxisZ2, port.Entry{ID: "", Body: "no key"}); !errors.Is(err, port.ErrInvalidEntry) {
		t.Fatalf("Append with an empty id: got %v, want ErrInvalidEntry", err)
	}
	want := port.DefaultID(port.ScopeUser, port.AxisZ2, "the body")
	stored, err := s.Append(context.Background(), port.ScopeUser, port.AxisZ2, port.Entry{ID: want, Body: "the body"})
	if err != nil {
		t.Fatalf("Append with the DefaultID: %v", err)
	}
	if stored.ID != want {
		t.Fatalf("stored id = %q, want %q", stored.ID, want)
	}
}

func TestStoreSurvivesTwoConcurrentProcesses(t *testing.T) {
	runTwoProcesses(t, false)
}

func TestStoreLeaksBusyWithoutTheRetry(t *testing.T) {
	runTwoProcesses(t, true)
}

func runTwoProcesses(t *testing.T, control bool) {
	t.Helper()
	if testing.Short() {
		t.Skip("the two-process run is the expensive evidence run")
	}
	dir := t.TempDir()
	user := filepath.Join(dir, "user.db")
	project := filepath.Join(dir, "project.db")
	seed, err := Open(Config{Profile: ProfileRuntime, UserDB: user, ProjectDB: project, Origin: "runtime"})
	if err != nil {
		t.Fatalf("seed Open: %v", err)
	}
	seed.Close()

	self, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go toolchain: %v", err)
	}
	workers, rows := 4, 2000
	binary := filepath.Join(dir, "busy.test.exe")
	build := exec.Command(self, "test", "-c", "-o", binary, ".")
	build.Env = append(build.Environ(), "GOFLAGS=-mod=mod")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the helper binary: %v\n%s", err, out)
	}
	var wg sync.WaitGroup
	reports := make([]string, 2)
	codes := make([]int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cmd := exec.Command(binary, "-test.run=TestBusyHelper", "-test.v")
			env := append(cmd.Environ(),
				"ECO_BUSY_HELPER=1",
				fmt.Sprintf("ECO_BUSY_USER_DB=%s", user),
				fmt.Sprintf("ECO_BUSY_PROJECT_DB=%s", project),
				fmt.Sprintf("ECO_BUSY_WORKERS=%d", workers),
				fmt.Sprintf("ECO_BUSY_ROWS=%d", rows),
			)
			if control {
				env = append(env, "ECO_BUSY_NO_RETRY=1", "ECO_BUSY_TIMEOUT=0", "ECO_BUSY_IMMEDIATE=0")
			}
			cmd.Env = env
			out, err := cmd.CombinedOutput()
			reports[idx] = string(out)
			if err != nil {
				var ee *exec.ExitError
				if errors.As(err, &ee) {
					codes[idx] = ee.ExitCode()
				} else {
					codes[idx] = -1
				}
			}
		}(i)
	}
	wg.Wait()
	leaked := 0
	for i := range reports {
		t.Logf("--- helper %d (exit=%d, control=%t) ---\n%s", i, codes[i], control, reports[i])
		leaked += helperBusyLeak(t, reports[i])
	}
	if !control {
		if leaked != 0 {
			t.Fatalf("the Store leaked %d SQLITE_BUSY to the caller across the two processes", leaked)
		}
	} else if leaked == 0 {
		t.Fatal("the control run leaked no SQLITE_BUSY at all, so it does not prove the retry is what absorbs them")
	}
	if control {
		return
	}

	check, err := Open(Config{Profile: ProfileRuntime, UserDB: user, ProjectDB: project, Origin: "runtime"})
	if err != nil {
		t.Fatalf("reopen after the two processes: %v", err)
	}
	defer check.Close()
	db, err := check.db(port.ScopeUser)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM entries WHERE scope = ? AND axis = ?`, string(port.ScopeUser), string(port.AxisZ2)).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 2*workers*rows {
		t.Fatalf("the two processes left %d rows, want %d", count, 2*workers*rows)
	}
	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatalf("integrity_check: %v", err)
	}
	if integrity != "ok" {
		t.Fatalf("integrity_check = %q, want ok", integrity)
	}
	var ftsRows int
	if err := db.QueryRow(`SELECT count(*) FROM entries_fts`).Scan(&ftsRows); err != nil {
		t.Fatalf("count fts rows: %v", err)
	}
	if ftsRows != count {
		t.Fatalf("entries_fts holds %d rows, want %d: the triggers drifted from the table", ftsRows, count)
	}
	if err := check.Probe(context.Background()); err != nil {
		t.Fatalf("Probe after the two processes: %v", err)
	}
}

var helperLineRe = regexp.MustCompile(`HELPER .*busy_leaked=(\d+)`)

func helperBusyLeak(t *testing.T, report string) int {
	t.Helper()
	total := 0
	found := false
	for _, line := range strings.Split(report, "\n") {
		match := helperLineRe.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		found = true
		value, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("parse %q: %v", match[1], err)
		}
		total += value
	}
	if !found {
		t.Fatalf("the helper reported no HELPER line:\n%s", report)
	}
	return total
}

func openAt(t *testing.T, dir string, profile Profile, origin string) *Store {
	t.Helper()
	s, err := Open(Config{
		UserDB:    filepath.Join(dir, "user.db"),
		ProjectDB: filepath.Join(dir, "project.db"),
		Origin:    origin,
		Profile:   profile,
	})
	if err != nil {
		t.Fatalf("Open with profile %s: got error %v, want nil", profile, err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestPromoteThroughAPortWithAZ3BiggerThanThePage(t *testing.T) {
	source := port.NewFake()
	target := openAt(t, t.TempDir(), ProfileRuntime, "runtime")
	for i := 0; i < 250; i++ {
		id := fmt.Sprintf("z3-%03d", i)
		if _, err := source.Append(context.Background(), port.ScopeProject, port.AxisZ3, port.Entry{
			ID: id, At: atOffset(i), Body: "body of " + id,
		}); err != nil {
			t.Fatalf("seed Z3 %s: %v", id, err)
		}
	}
	out, err := Promote(context.Background(), source, target, []string{"z3-240", "z3-000"})
	if err != nil {
		t.Fatalf("Promote through a Port out of a Z3 of 250: got %v, want nil", err)
	}
	if len(out) != 2 {
		t.Fatalf("Promote through a Port out of a Z3 of 250: got %d entries, want 2: %v", len(out), out)
	}
	for i, want := range []string{PromotedID("z3-240"), PromotedID("z3-000")} {
		if out[i].ID != want {
			t.Fatalf("Promote order: entry %d is %q, want %q", i, out[i].ID, want)
		}
	}
	got, err := target.Read(context.Background(), port.ScopeUser, port.AxisZ2, MaxLimit)
	if err != nil {
		t.Fatalf("read Z2: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Z2 holds %d entries, want 2: %v", len(got), got)
	}
}

func TestPromoteKeepsTheProvenanceOfTheSourceRow(t *testing.T) {
	source := openAt(t, t.TempDir(), ProfileRuntime, "distiller")
	target := openAt(t, t.TempDir(), ProfileHuman, "cli")
	if _, err := source.Append(context.Background(), port.ScopeProject, port.AxisZ3, port.Entry{
		ID: "distilled", Body: "the distilled truth",
	}); err != nil {
		t.Fatalf("seed Z3: %v", err)
	}
	out, err := Promote(context.Background(), source, target, []string{"distilled"})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("Promote returned %d entries, want 1", len(out))
	}
	attrs := out[0].Attrs
	if attrs[attrSourceOrigin] != "distiller" {
		t.Fatalf("source_origin = %q, want distiller: %v", attrs[attrSourceOrigin], attrs)
	}
	if attrs[attrPromotedFrom] != "distilled" {
		t.Fatalf("promoted_from = %q, want distilled: %v", attrs[attrPromotedFrom], attrs)
	}
	if attrs[attrOrigin] != "cli" {
		t.Fatalf("origin = %q, want cli (the process that promoted): %v", attrs[attrOrigin], attrs)
	}
}

func TestPromoteRefusesATargetThatIsNotAStore(t *testing.T) {
	source := port.NewFake()
	if _, err := source.Append(context.Background(), port.ScopeProject, port.AxisZ3, port.Entry{
		ID: "a", At: atOffset(0), Body: "body of a",
	}); err != nil {
		t.Fatalf("seed Z3: %v", err)
	}
	target := port.NewFake()
	_, err := Promote(context.Background(), source, target, []string{"a"})
	if !errors.Is(err, port.ErrUnavailable) {
		t.Fatalf("Promote into a target that is not a Store: got %v, want ErrUnavailable", err)
	}
	if !strings.Contains(err.Error(), "not a *Store") {
		t.Fatalf("the refusal must name the reason: got %v", err)
	}
	if got := readZ2(t, target); len(got) != 0 {
		t.Fatalf("a refused promotion wrote %d entries to Z2, want 0: %v", len(got), got)
	}
}

func TestPromoteRefusesAnAgentProfileTarget(t *testing.T) {
	source := openAt(t, t.TempDir(), ProfileAgent, "mcp")
	if _, err := source.Append(context.Background(), port.ScopeProject, port.AxisZ3, port.Entry{
		ID: "a", Body: "body of a",
	}); err != nil {
		t.Fatalf("an agent Profile must be able to write Z3: %v", err)
	}
	target := openAt(t, t.TempDir(), ProfileAgent, "mcp")
	_, err := Promote(context.Background(), source, target, []string{"a"})
	if !errors.Is(err, port.ErrForbidden) {
		t.Fatalf("Promote with a ProfileAgent Store: got %v, want ErrForbidden", err)
	}
	if got := readZ2(t, target); len(got) != 0 {
		t.Fatalf("a forbidden promotion wrote %d entries to Z2, want 0: %v", len(got), got)
	}
}

func TestPromoteWithACancelledContextTouchesNothing(t *testing.T) {
	source := openAt(t, t.TempDir(), ProfileRuntime, "runtime")
	target := openAt(t, t.TempDir(), ProfileHuman, "runtime")
	if _, err := source.Append(context.Background(), port.ScopeProject, port.AxisZ3, port.Entry{
		ID: "a", Body: "body of a",
	}); err != nil {
		t.Fatalf("seed Z3: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Promote(ctx, source, target, []string{"a"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Promote on a cancelled context: got %v, want context.Canceled", err)
	}
	if got := readZ2(t, target); len(got) != 0 {
		t.Fatalf("a cancelled promotion wrote %d entries to Z2, want 0: %v", len(got), got)
	}
}

func TestPromoteIntoZ2IsAllOrNothing(t *testing.T) {
	source := port.NewFake()
	ids := make([]string, 0, 24)
	for i := 0; i < 24; i++ {
		id := fmt.Sprintf("z3-%02d", i)
		ids = append(ids, id)
		body := "body of " + id
		if i == 12 {
			body = strings.Repeat("x", MaxBodyBytes+1)
		}
		if _, err := source.Append(context.Background(), port.ScopeProject, port.AxisZ3, port.Entry{ID: id, Body: body}); err != nil {
			t.Fatalf("seed Z3 %s: %v", id, err)
		}
	}
	target := openAt(t, t.TempDir(), ProfileHuman, "runtime")
	_, err := Promote(context.Background(), source, target, ids)
	if !errors.Is(err, port.ErrInvalidEntry) {
		t.Fatalf("a batch whose thirteenth body is too long: got %v, want ErrInvalidEntry", err)
	}
	if got := readZ2(t, target); len(got) != 0 {
		t.Fatalf("a batch that failed on its thirteenth row left %d entries in Z2, want 0: %v", len(got), got)
	}
}

func holdTheWriteLock(t *testing.T, path string) func() {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatalf("open the intruder: %v", err)
	}
	conn, err := db.Conn(context.Background())
	if err != nil {
		db.Close()
		t.Fatalf("pin a connection: %v", err)
	}
	if _, err := conn.ExecContext(context.Background(), "BEGIN EXCLUSIVE"); err != nil {
		conn.Close()
		db.Close()
		t.Fatalf("BEGIN EXCLUSIVE: %v", err)
	}
	return func() {
		if _, err := conn.ExecContext(context.Background(), "ROLLBACK"); err != nil {
			t.Errorf("ROLLBACK: %v", err)
		}
		conn.Close()
		db.Close()
	}
}

func TestPromoteCancelledDuringTheBatchLeavesNoRows(t *testing.T) {
	for _, delay := range []time.Duration{15 * time.Millisecond, 40 * time.Millisecond} {
		t.Run(delay.String(), func(t *testing.T) {
			source := port.NewFake()
			ids := make([]string, 0, 200)
			for i := 0; i < 200; i++ {
				id := fmt.Sprintf("z3-%03d", i)
				ids = append(ids, id)
				if _, err := source.Append(context.Background(), port.ScopeProject, port.AxisZ3, port.Entry{ID: id, Body: "body of " + id}); err != nil {
					t.Fatalf("seed Z3 %s: %v", id, err)
				}
			}
			dir := t.TempDir()
			target, err := Open(Config{
				UserDB:        filepath.Join(dir, "user.db"),
				ProjectDB:     filepath.Join(dir, "project.db"),
				Origin:        "runtime",
				Profile:       ProfileHuman,
				NoBusyTimeout: true,
			})
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			t.Cleanup(func() { _ = target.Close() })
			release := holdTheWriteLock(t, filepath.Join(dir, "user.db"))
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() {
				_, err := Promote(ctx, source, target, ids)
				done <- err
			}()
			time.Sleep(delay)
			cancel()
			err = <-done
			release()
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Promote cancelled %s into the batch: got %v, want context.Canceled", delay, err)
			}
			if got := readZ2(t, target); len(got) != 0 {
				t.Fatalf("a batch cancelled %s left %d entries in Z2, want 0: %v", delay, len(got), got)
			}
		})
	}
}
