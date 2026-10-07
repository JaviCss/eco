package port

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"
)

var (
	ErrUnavailable    = errors.New("eco: unavailable")
	ErrNotFound       = errors.New("eco: not found")
	ErrInvalidScope   = errors.New("eco: invalid scope")
	ErrAxisNotInScope = errors.New("eco: axis not in scope")
	ErrInvalidEntry   = errors.New("eco: invalid entry")
	ErrForbidden      = errors.New("eco: forbidden")
)

const MaxBatch = 200

type Scope string

const (
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
)

type Axis string

const (
	AxisZ2       Axis = "Z2"
	AxisZ3       Axis = "Z3"
	AxisY        Axis = "Y"
	AxisX        Axis = "X"
	AxisR        Axis = "R"
	AxisV        Axis = "V"
	AxisManifest Axis = "Manifest"
)

type Entry struct {
	ID    string
	Axis  Axis
	Scope Scope
	At    time.Time
	Body  string
	Attrs map[string]string
}

type Port interface {
	Get(ctx context.Context, scope Scope, axis Axis, ids []string) ([]Entry, error)
	Read(ctx context.Context, scope Scope, axis Axis, limit int) ([]Entry, error)
	Append(ctx context.Context, scope Scope, axis Axis, entry Entry) (Entry, error)
	Search(ctx context.Context, scope Scope, axis Axis, query string, limit int) ([]Entry, error)
	Probe(ctx context.Context) error
}

type Outage interface {
	Port
	SetUnavailable(bool)
}

var axisScopes = map[Axis]Scope{
	AxisZ2:       ScopeUser,
	AxisR:        ScopeUser,
	AxisV:        ScopeUser,
	AxisManifest: ScopeUser,
	AxisZ3:       ScopeProject,
	AxisY:        ScopeProject,
	AxisX:        ScopeProject,
}

func (s Scope) Valid() bool {
	return s == ScopeUser || s == ScopeProject
}

func ScopeOfAxis(axis Axis) (Scope, bool) {
	scope, ok := axisScopes[axis]
	return scope, ok
}

func checkTarget(scope Scope, axis Axis) error {
	if !scope.Valid() {
		return fmt.Errorf("eco: %w: %q", ErrInvalidScope, string(scope))
	}
	owner, ok := axisScopes[axis]
	if !ok {
		return fmt.Errorf("eco: %w: axis %q has no scope", ErrAxisNotInScope, string(axis))
	}
	if owner != scope {
		return fmt.Errorf("eco: %w: axis %q lives in scope %q", ErrAxisNotInScope, string(axis), string(owner))
	}
	return nil
}

func DefaultID(scope Scope, axis Axis, body string) string {
	sum := sha256.Sum256([]byte(string(scope) + "\x00" + string(axis) + "\x00" + body))
	return hex.EncodeToString(sum[:])
}

func Merge(user, project []Entry) []Entry {
	out := make([]Entry, 0, len(user)+len(project))
	out = append(out, user...)
	out = append(out, project...)
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.After(out[j].At)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func sortedNewest(entries []Entry, limit int) []Entry {
	out := make([]Entry, len(entries))
	copy(out, entries)
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.After(out[j].At)
		}
		return out[i].ID < out[j].ID
	})
	return limitTo(out, limit)
}

func limitTo(entries []Entry, limit int) []Entry {
	if limit > 0 && len(entries) > limit {
		return entries[:limit]
	}
	return entries
}