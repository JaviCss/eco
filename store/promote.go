package store

import (
	"context"
	"errors"
	"fmt"

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
	appendPromoted(ctx context.Context, scope port.Scope, axis port.Axis, entry port.Entry) (port.Entry, error)
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
	plan, err := planPromotion(ctx, source, ids)
	if err != nil {
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
	if err := destination.preflightPromotion(ctx, port.ScopeUser, port.AxisZ2, plan); err != nil {
		return nil, err
	}
	out := make([]port.Entry, 0, len(plan))
	for _, promoted := range plan {
		entry := port.Entry{
			ID:    promoted.TargetID,
			Axis:  port.AxisZ2,
			Scope: port.ScopeUser,
			Body:  promoted.Body,
			Attrs: promoted.Attrs,
		}
		stored, err := destination.appendPromoted(ctx, port.ScopeUser, port.AxisZ2, entry)
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

func (s *Store) promotionProfile() Profile {
	return s.cfg.Profile
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
