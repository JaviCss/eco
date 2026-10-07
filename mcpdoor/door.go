package mcpdoor

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/JaviCss/eco/port"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const MaxToolResultBytes = 32 * 1024

const maxLimit = 200

var readAxes = []string{"Z2", "Z3", "Y", "X"}

var appendAxes = []string{"Z3", "Y", "X"}

type Client interface {
	Read(ctx context.Context, scope port.Scope, axis port.Axis, limit int) ([]port.Entry, error)
	Search(ctx context.Context, scope port.Scope, axis port.Axis, query string, limit int) ([]port.Entry, error)
	Append(ctx context.Context, scope port.Scope, axis port.Axis, entry port.Entry) (port.Entry, error)
	Probe(ctx context.Context) error
}

type entriesPayload struct {
	Entries   []port.Entry `json:"entries"`
	Truncated bool         `json:"truncated,omitempty"`
	Omitted   int          `json:"omitted,omitempty"`
}

type probePayload struct {
	Status string `json:"status"`
}

type toolResponse struct {
	result *mcp.CallToolResult
}

type door struct {
	client Client
}

func sentinelText(err error) string {
	switch {
	case errors.Is(err, port.ErrNotFound):
		return "eco: not found"
	case errors.Is(err, port.ErrForbidden):
		return "eco: forbidden"
	case errors.Is(err, port.ErrInvalidEntry):
		return "eco: invalid entry"
	case errors.Is(err, port.ErrUnavailable):
		return "eco: unavailable"
	case errors.Is(err, port.ErrAxisNotInScope):
		return "eco: axis not in scope"
	case errors.Is(err, port.ErrInvalidScope):
		return "eco: invalid scope"
	default:
		return "eco: unavailable"
	}
}

func failure(err error) toolResponse {
	return toolResponse{result: &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: sentinelText(err)}},
	}}
}

func success(payload []byte) toolResponse {
	return toolResponse{result: &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}},
	}}
}

func encode(payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func fitEntries(entries []port.Entry) ([]byte, error) {
	if entries == nil {
		entries = []port.Entry{}
	}
	full, err := encode(entriesPayload{Entries: entries})
	if err != nil {
		return nil, err
	}
	if len(full) <= MaxToolResultBytes {
		return full, nil
	}
	total := len(entries)
	candidate := func(kept int) ([]byte, error) {
		return encode(entriesPayload{
			Entries:   entries[:kept],
			Truncated: true,
			Omitted:   total - kept,
		})
	}
	low, high, best := 0, total, 0
	for low <= high {
		mid := (low + high) / 2
		raw, err := candidate(mid)
		if err != nil {
			return nil, err
		}
		if len(raw) <= MaxToolResultBytes {
			best = mid
			low = mid + 1
			continue
		}
		high = mid - 1
	}
	return candidate(best)
}

func resolveAxis(raw string, allowed []string) (port.Axis, port.Scope, bool) {
	matched := false
	for _, candidate := range allowed {
		if candidate == raw {
			matched = true
			break
		}
	}
	if !matched {
		return "", "", false
	}
	axis := port.Axis(raw)
	scope, ok := port.ScopeOfAxis(axis)
	if !ok {
		return "", "", false
	}
	return axis, scope, true
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return maxLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

func (d *door) read(ctx context.Context, axisRaw string, limit int) toolResponse {
	axis, scope, ok := resolveAxis(axisRaw, readAxes)
	if !ok {
		return failure(port.ErrForbidden)
	}
	entries, err := d.client.Read(ctx, scope, axis, clampLimit(limit))
	if err != nil {
		return failure(err)
	}
	raw, err := fitEntries(entries)
	if err != nil {
		return failure(port.ErrInvalidEntry)
	}
	return success(raw)
}

func (d *door) search(ctx context.Context, axisRaw, query string, limit int) toolResponse {
	axis, scope, ok := resolveAxis(axisRaw, readAxes)
	if !ok {
		return failure(port.ErrForbidden)
	}
	entries, err := d.client.Search(ctx, scope, axis, query, clampLimit(limit))
	if err != nil {
		return failure(err)
	}
	raw, err := fitEntries(entries)
	if err != nil {
		return failure(port.ErrInvalidEntry)
	}
	return success(raw)
}

func (d *door) append(ctx context.Context, axisRaw, id, body string, attrs map[string]string) toolResponse {
	axis, scope, ok := resolveAxis(axisRaw, appendAxes)
	if !ok {
		return failure(port.ErrForbidden)
	}
	if id == "" {
		id = port.DefaultID(scope, axis, body)
	}
	written, err := d.client.Append(ctx, scope, axis, port.Entry{
		ID:    id,
		Axis:  axis,
		Scope: scope,
		Body:  body,
		Attrs: attrs,
	})
	if err != nil {
		return failure(err)
	}
	raw, err := fitEntries([]port.Entry{written})
	if err != nil {
		return failure(port.ErrInvalidEntry)
	}
	return success(raw)
}

func (d *door) probe(ctx context.Context) toolResponse {
	status := "ok"
	if err := d.client.Probe(ctx); err != nil {
		switch {
		case errors.Is(err, port.ErrForbidden):
			status = "forbidden"
		default:
			status = "unavailable"
		}
	}
	raw, err := encode(probePayload{Status: status})
	if err != nil {
		return failure(port.ErrInvalidEntry)
	}
	return success(raw)
}
