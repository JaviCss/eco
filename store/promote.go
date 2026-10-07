package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/JaviCss/eco/port"
)

var ErrPromotedConflict = errors.New("eco: promoted id exists with a different body")

type PromotedTarget struct {
	SourceID string
	TargetID string
	Body     string
	Attrs    map[string]string
}

func PromotedID(sourceID string) string {
	return "promoted:" + sourceID
}

type promoter interface {
	appendPromotedBatch(ctx context.Context, scope port.Scope, axis port.Axis, entries []port.Entry) ([]port.Entry, error)
	preflightPromotion(ctx context.Context, scope port.Scope, axis port.Axis, targets []PromotedTarget) error
	promotionProfile() Profile
}

func planPromotion(ctx context.Context, source port.Port, ids []string) ([]PromotedTarget, error) {
	entries, err := source.Get(ctx, port.ScopeProject, port.AxisZ3, ids)
	if err != nil {
		return nil, err
	}
	out := make([]PromotedTarget, 0, len(entries))
	for _, entry := range entries {
		attrs := map[string]string{}
		for key, value := range entry.Attrs {
			attrs[key] = value
		}
		attrs[attrSourceOrigin] = entry.Attrs[attrOrigin]
		attrs[attrPromotedFrom] = entry.ID
		delete(attrs, attrOrigin)
		out = append(out, PromotedTarget{
			SourceID: entry.ID,
			TargetID: PromotedID(entry.ID),
			Body:     entry.Body,
			Attrs:    attrs,
		})
	}
	return out, nil
}

func Promote(ctx context.Context, source port.Port, target port.Port, ids []string) ([]port.Entry, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("eco: Promote: %w: empty batch", port.ErrInvalidEntry)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	destination, ok := target.(promoter)
	if !ok {
		return nil, fmt.Errorf("eco: Promote: %w: the target is not a *Store: only the Store stamps the reserved attrs of Z2", port.ErrUnavailable)
	}
	profile := destination.promotionProfile()
	if !profile.mayPromote() {
		return nil, fmt.Errorf("eco: Promote: %w: profile %s cannot promote into Z2", port.ErrForbidden, profile)
	}
	plan, err := planPromotion(ctx, source, ids)
	if err != nil {
		return nil, err
	}
	if err := destination.preflightPromotion(ctx, port.ScopeUser, port.AxisZ2, plan); err != nil {
		return nil, unwrapContext(ctx, err)
	}
	batch := make([]port.Entry, 0, len(plan))
	for _, promoted := range plan {
		batch = append(batch, port.Entry{
			ID:    promoted.TargetID,
			Axis:  port.AxisZ2,
			Scope: port.ScopeUser,
			Body:  promoted.Body,
			Attrs: promoted.Attrs,
		})
	}
	stored, err := destination.appendPromotedBatch(ctx, port.ScopeUser, port.AxisZ2, batch)
	if err != nil {
		return nil, unwrapContext(ctx, err)
	}
	return stored, nil
}

func unwrapContext(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

func (s *Store) promotionProfile() Profile {
	return s.cfg.Profile
}

func (s *Store) appendPromotedBatch(ctx context.Context, scope port.Scope, axis port.Axis, entries []port.Entry) ([]port.Entry, error) {
	if err := checkTarget(scope, axis); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	db, err := s.db(scope)
	if err != nil {
		return nil, err
	}
	rows := make([][]any, 0, len(entries))
	for _, entry := range entries {
		checked, err := checkEntry(entry, true)
		if err != nil {
			return nil, fmt.Errorf("eco: Promote: %w", err)
		}
		checked.Axis = axis
		checked.Scope = scope
		if checked.At.IsZero() {
			checked.At = time.Now()
		}
		checked.At = checked.At.UTC()
		checked.Attrs[attrOrigin] = s.cfg.Origin
		attrs, err := encodeAttrs(checked.Attrs)
		if err != nil {
			return nil, fmt.Errorf("eco: Promote: %w: %v", port.ErrInvalidEntry, err)
		}
		rows = append(rows, []any{checked.ID, string(axis), string(scope), checked.At.UnixNano(), checked.Body, attrs})
	}
	var stored []port.Entry
	if err := s.withRetry(ctx, "Promote", func() error {
		out, err := s.writePromotedBatch(ctx, db, rows, entries, scope, axis)
		if err != nil {
			return err
		}
		stored = out
		return nil
	}); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if errors.Is(err, ErrPromotedConflict) || errors.Is(err, port.ErrInvalidEntry) {
			return nil, err
		}
		return nil, fmt.Errorf("eco: Promote: %w: %v", port.ErrUnavailable, err)
	}
	return stored, nil
}

func (s *Store) writePromotedBatch(ctx context.Context, db *sql.DB, rows [][]any, entries []port.Entry, scope port.Scope, axis port.Axis) ([]port.Entry, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := make([]port.Entry, 0, len(rows))
	for i, args := range rows {
		if _, err := tx.ExecContext(ctx, sqlInsert, args...); err != nil {
			tx.Rollback()
			return nil, err
		}
		got, err := scanEntry(tx.QueryRowContext(ctx, sqlGet, string(scope), string(axis), entries[i].ID))
		if err != nil {
			tx.Rollback()
			return nil, err
		}
		if got.Body != entries[i].Body {
			tx.Rollback()
			return nil, fmt.Errorf("eco: Promote: %w: %s holds a different body than Z3 %s", ErrPromotedConflict, entries[i].ID, entries[i].Attrs[attrPromotedFrom])
		}
		out = append(out, got)
	}
	if err := tx.Commit(); err != nil {
		tx.Rollback()
		return nil, err
	}
	return out, nil
}

func (s *Store) preflightPromotion(ctx context.Context, scope port.Scope, axis port.Axis, targets []PromotedTarget) error {
	db, err := s.db(scope)
	if err != nil {
		return err
	}
	for _, promoted := range targets {
		row := db.QueryRowContext(ctx, sqlGet, string(scope), string(axis), promoted.TargetID)
		var existing string
		switch err := row.Scan(new(string), new(string), new(string), new(int64), &existing, new(string)); {
		case err == nil:
			if existing != promoted.Body {
				return fmt.Errorf("eco: Promote: %w: %s already holds a different body", ErrPromotedConflict, promoted.TargetID)
			}
		case isNotFound(err):
			continue
		default:
			return fmt.Errorf("eco: Promote: %w: %v", port.ErrUnavailable, err)
		}
	}
	return nil
}
