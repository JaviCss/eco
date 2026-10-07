package store

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/JaviCss/eco/port"
)

var ErrPromotedConflict = errors.New("eco: promoted id exists with a different body")

const scanCeiling = 200000

type PromotedTarget struct {
	SourceID string
	TargetID string
	Body     string
	Attrs    map[string]string
}

func PromotedID(sourceID string) string {
	return "promoted:" + sourceID
}

func planPromotion(source port.Port, ids []string) ([]PromotedTarget, error) {
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	seen, err := scanFor(context.Background(), source, wanted)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, id := range ids {
		if _, ok := seen[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("eco: Promote: %w: not in Z3: %v", port.ErrNotFound, missing)
	}
	out := make([]PromotedTarget, 0, len(ids))
	for _, id := range ids {
		entry := seen[id]
		attrs := map[string]string{}
		for key, value := range entry.Attrs {
			attrs[key] = value
		}
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

func scanFor(ctx context.Context, source port.Port, wanted map[string]bool) (map[string]port.Entry, error) {
	if scanner, ok := source.(promotionScanner); ok {
		return scanner.scanForPromotion(ctx, port.ScopeProject, port.AxisZ3, wanted)
	}
	seen := map[string]port.Entry{}
	entries, err := source.Read(ctx, port.ScopeProject, port.AxisZ3, MaxLimit)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if wanted[entry.ID] {
			seen[entry.ID] = entry
		}
	}
	return seen, nil
}

type promotionScanner interface {
	scanForPromotion(ctx context.Context, scope port.Scope, axis port.Axis, wanted map[string]bool) (map[string]port.Entry, error)
}

func (s *Store) scanForPromotion(ctx context.Context, scope port.Scope, axis port.Axis, wanted map[string]bool) (map[string]port.Entry, error) {
	db, err := s.db(scope)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, sqlSelect, string(scope), string(axis), scanCeiling)
	if err != nil {
		return nil, fmt.Errorf("eco: Promote: %w: %v", port.ErrUnavailable, err)
	}
	defer rows.Close()
	seen := map[string]port.Entry{}
	for rows.Next() {
		entry, scanErr := scanEntry(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("eco: Promote: %w: %v", port.ErrUnavailable, scanErr)
		}
		if wanted[entry.ID] {
			seen[entry.ID] = entry
			if len(seen) == len(wanted) {
				return seen, nil
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("eco: Promote: %w: %v", port.ErrUnavailable, err)
	}
	return seen, nil
}

type promotedAppender interface {
	appendPromoted(ctx context.Context, scope port.Scope, axis port.Axis, entry port.Entry) (port.Entry, error)
}

type promotedPreflight interface {
	preflightPromotion(ctx context.Context, scope port.Scope, axis port.Axis, targets []PromotedTarget) error
}

func Promote(ctx context.Context, source port.Port, target port.Port, ids []string) ([]port.Entry, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("eco: Promote: %w: empty batch", port.ErrInvalidEntry)
	}
	plan, err := planPromotion(source, ids)
	if err != nil {
		return nil, err
	}
	if pre, ok := target.(promotedPreflight); ok {
		if err := pre.preflightPromotion(ctx, port.ScopeUser, port.AxisZ2, plan); err != nil {
			return nil, err
		}
	}
	appender, canPromote := target.(promotedAppender)
	out := make([]port.Entry, 0, len(plan))
	for _, promoted := range plan {
		entry := port.Entry{
			ID:    promoted.TargetID,
			Axis:  port.AxisZ2,
			Scope: port.ScopeUser,
			Body:  promoted.Body,
			Attrs: promoted.Attrs,
		}
		var stored port.Entry
		if canPromote {
			stored, err = appender.appendPromoted(ctx, port.ScopeUser, port.AxisZ2, entry)
		} else {
			stored, err = target.Append(ctx, port.ScopeUser, port.AxisZ2, entry)
		}
		if err != nil {
			return out, err
		}
		if stored.Body != promoted.Body {
			return out, fmt.Errorf("eco: Promote: %w: %s holds a different body than Z3 %s", ErrPromotedConflict, promoted.TargetID, promoted.SourceID)
		}
		out = append(out, stored)
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