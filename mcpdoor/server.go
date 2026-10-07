package mcpdoor

import (
	"bufio"
	"context"
	"io"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	toolRead   = "eco_read"
	toolSearch = "eco_search"
	toolAppend = "eco_append"
	toolProbe  = "eco_probe"
)

type Server struct {
	sdk *mcp.Server
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

func axisSchema(values []string) map[string]any {
	enum := make([]any, 0, len(values))
	for _, value := range values {
		enum = append(enum, value)
	}
	return map[string]any{"type": "string", "enum": enum}
}

func limitSchema() map[string]any {
	return map[string]any{"type": "integer", "minimum": 1, "maximum": maxLimit}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	req := make([]any, 0, len(required))
	for _, name := range required {
		req = append(req, name)
	}
	return map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   req,
	}
}

type readArgs struct {
	Axis  string `json:"axis"`
	Limit int    `json:"limit"`
}

type searchArgs struct {
	Axis  string `json:"axis"`
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type appendArgs struct {
	Axis  string            `json:"axis"`
	ID    string            `json:"id"`
	Body  string            `json:"body"`
	Attrs map[string]string `json:"attrs"`
}

func New(client Client) *Server {
	sdk := mcp.NewServer(&mcp.Implementation{Name: "eco-mcpdoor", Version: "0.1.0"}, nil)
	backend := &door{client: client}

	mcp.AddTool(sdk, &mcp.Tool{
		Name:        toolRead,
		Description: "Read recent entries from an axis.",
		InputSchema: objectSchema(map[string]any{
			"axis":  axisSchema(readAxes),
			"limit": limitSchema(),
		}, []string{"axis"}),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in readArgs) (*mcp.CallToolResult, any, error) {
		return backend.read(ctx, in.Axis, in.Limit).result, nil, nil
	})

	mcp.AddTool(sdk, &mcp.Tool{
		Name:        toolSearch,
		Description: "Search entries of an axis by substring.",
		InputSchema: objectSchema(map[string]any{
			"axis":  axisSchema(readAxes),
			"query": map[string]any{"type": "string"},
			"limit": limitSchema(),
		}, []string{"axis", "query"}),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchArgs) (*mcp.CallToolResult, any, error) {
		return backend.search(ctx, in.Axis, in.Query, in.Limit).result, nil, nil
	})

	mcp.AddTool(sdk, &mcp.Tool{
		Name:        toolAppend,
		Description: "Append an entry to Z3, Y or X.",
		InputSchema: objectSchema(map[string]any{
			"axis":  axisSchema(appendAxes),
			"id":    map[string]any{"type": "string"},
			"body":  map[string]any{"type": "string"},
			"attrs": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
		}, []string{"axis", "body"}),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in appendArgs) (*mcp.CallToolResult, any, error) {
		return backend.append(ctx, in.Axis, in.ID, in.Body, in.Attrs).result, nil, nil
	})

	mcp.AddTool(sdk, &mcp.Tool{
		Name:        toolProbe,
		Description: "Report Eco availability as ok, forbidden or unavailable.",
		InputSchema: objectSchema(map[string]any{}, nil),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		return backend.probe(ctx).result, nil, nil
	})

	return &Server{sdk: sdk}
}

func (s *Server) Serve(ctx context.Context, in *bufio.Reader, out io.Writer) error {
	transport := &mcp.IOTransport{
		Reader: io.NopCloser(in),
		Writer: nopWriteCloser{out},
	}
	return s.sdk.Run(ctx, transport)
}
