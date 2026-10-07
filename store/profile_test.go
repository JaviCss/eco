package store

import (
	"context"
	"errors"
	"testing"

	"github.com/JaviCss/eco/port"
)

func TestProfileZeroIsTheNarrowOne(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Config{
		UserDB:    dir + "/user.db",
		ProjectDB: dir + "/project.db",
		Origin:    "agent",
	})
	if err != nil {
		t.Fatalf("Open with no Profile: got error %v, want nil", err)
	}
	defer s.Close()
	if s.cfg.Profile != ProfileAgent {
		t.Fatalf("a Config with no Profile opened as %s, want agent", s.cfg.Profile)
	}
	for _, axis := range []port.Axis{port.AxisZ2, port.AxisR, port.AxisV, port.AxisManifest} {
		if _, err := s.Append(context.Background(), port.ScopeUser, axis, port.Entry{ID: "k-" + string(axis), Body: "b"}); !errors.Is(err, port.ErrForbidden) {
			t.Fatalf("Append %s with ProfileAgent: got %v, want ErrForbidden", axis, err)
		}
	}
	for _, axis := range []port.Axis{port.AxisZ3, port.AxisY, port.AxisX} {
		scope, ok := port.ScopeOfAxis(axis)
		if !ok {
			t.Fatalf("axis %s has no scope", axis)
		}
		if _, err := s.Append(context.Background(), scope, axis, port.Entry{ID: "k-" + string(axis), Body: "b"}); err != nil {
			t.Fatalf("Append %s with ProfileAgent: got %v, want nil", axis, err)
		}
	}
	if _, err := s.Read(context.Background(), port.ScopeUser, port.AxisZ2, 10); err != nil {
		t.Fatalf("Read Z2 with ProfileAgent: got %v, want nil", err)
	}
	if _, err := s.Search(context.Background(), port.ScopeUser, port.AxisZ2, "body", 10); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("Search Z2 with ProfileAgent: got %v, want ErrNotFound", err)
	}
	if _, err := s.Get(context.Background(), port.ScopeUser, port.AxisZ2, []string{"k-Z3"}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("Get Z2 with ProfileAgent: got %v, want ErrNotFound", err)
	}
	if _, err := s.Read(context.Background(), port.ScopeUser, port.AxisR, 10); !errors.Is(err, port.ErrForbidden) {
		t.Fatalf("Read R with ProfileAgent: got %v, want ErrForbidden", err)
	}
	if _, err := s.Search(context.Background(), port.ScopeUser, port.AxisManifest, "body", 10); !errors.Is(err, port.ErrForbidden) {
		t.Fatalf("Search Manifest with ProfileAgent: got %v, want ErrForbidden", err)
	}
}

func TestProfileRuntimeIsTheOnlyOneThatTouchesR(t *testing.T) {
	s := openAt(t, t.TempDir(), ProfileRuntime, "runtime")
	for _, tc := range []struct {
		scope port.Scope
		axis  port.Axis
	}{
		{port.ScopeUser, port.AxisZ2},
		{port.ScopeUser, port.AxisR},
		{port.ScopeUser, port.AxisV},
		{port.ScopeUser, port.AxisManifest},
	} {
		if _, err := s.Append(context.Background(), tc.scope, tc.axis, port.Entry{ID: "rt-" + string(tc.axis), Body: "b"}); err != nil {
			t.Fatalf("Append %s with ProfileRuntime: got %v, want nil", tc.axis, err)
		}
		if _, err := s.Read(context.Background(), tc.scope, tc.axis, 10); err != nil {
			t.Fatalf("Read %s with ProfileRuntime: got %v, want nil", tc.axis, err)
		}
	}
}

func TestProfileHumanPromotesAndAgentDoesNot(t *testing.T) {
	source := openAt(t, t.TempDir(), ProfileHuman, "cli")
	if _, err := source.Append(context.Background(), port.ScopeProject, port.AxisZ3, port.Entry{ID: "a", Body: "body of a"}); err != nil {
		t.Fatalf("seed Z3: %v", err)
	}
	human := openAt(t, t.TempDir(), ProfileHuman, "cli")
	if _, err := Promote(context.Background(), source, human, []string{"a"}); err != nil {
		t.Fatalf("Promote with a ProfileHuman Store: got %v, want nil", err)
	}
	if got := readZ2(t, human); len(got) != 1 {
		t.Fatalf("Z2 holds %d entries, want 1: %v", len(got), got)
	}
	if !ProfileHuman.mayPromote() || ProfileAgent.mayPromote() || !ProfileRuntime.mayPromote() {
		t.Fatalf("mayPromote: human=%t agent=%t runtime=%t, want true false true",
			ProfileHuman.mayPromote(), ProfileAgent.mayPromote(), ProfileRuntime.mayPromote())
	}
}