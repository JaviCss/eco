package port

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFakePassesConformance(t *testing.T) {
	Conformance(t, func() Port { return NewFake() }, func() Outage { return NewFake() })
}

func TestAxisScopeTable(t *testing.T) {
	cases := []struct {
		axis  Axis
		scope Scope
	}{
		{AxisZ2, ScopeUser},
		{AxisR, ScopeUser},
		{AxisV, ScopeUser},
		{AxisManifest, ScopeUser},
		{AxisZ3, ScopeProject},
		{AxisY, ScopeProject},
		{AxisX, ScopeProject},
	}
	for _, c := range cases {
		got, ok := ScopeOfAxis(c.axis)
		if !ok {
			t.Errorf("ScopeOfAxis(%q): not in the axis table", c.axis)
			continue
		}
		if got != c.scope {
			t.Errorf("ScopeOfAxis(%q) = %q, want %q", c.axis, got, c.scope)
		}
	}
	if _, ok := ScopeOfAxis(Axis("Z1")); ok {
		t.Errorf("ScopeOfAxis(Z1) must be absent: Z1 is read only by release")
	}
	if _, ok := ScopeOfAxis(Axis("W2")); ok {
		t.Errorf("ScopeOfAxis(W2) must be absent: W2 arrives with the compositor")
	}
}

func TestAppendROnProjectIsRejected(t *testing.T) {
	p := NewFake()
	_, err := p.Append(context.Background(), ScopeProject, AxisR, Entry{
		ID: "k", At: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), Body: "b",
	})
	if !errors.Is(err, ErrAxisNotInScope) {
		t.Fatalf("Append R on Project: got %v, want ErrAxisNotInScope", err)
	}
}

func TestFakeIsConcurrencySafe(t *testing.T) {
	p := NewFake()
	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func(worker int) {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 25; j++ {
				if _, err := p.Append(context.Background(), ScopeUser, AxisZ2, Entry{
					ID:   string(rune('a' + j)),
					Body: "concurrent",
				}); err != nil {
					t.Errorf("Append from worker %d: %v", worker, err)
					return
				}
				if _, err := p.Read(context.Background(), ScopeUser, AxisZ2, 5); err != nil {
					t.Errorf("Read from worker %d: %v", worker, err)
					return
				}
			}
		}(i)
	}
	for i := 0; i < 4; i++ {
		<-done
	}
}