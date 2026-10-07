package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func toolSchema(required []string) map[string]any {
	properties := map[string]any{}
	for _, name := range required {
		properties[name] = map[string]any{"type": "string"}
	}
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

type toolSpec struct {
	name        string
	description string
	params      []string
}

func okHandler(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "ok"}},
	}, nil
}

func main() {
	specs := []toolSpec{
		{"read", "Read a file.", []string{"path"}},
		{"write", "Write a file.", []string{"path"}},
		{"list", "List a dir.", []string{"path"}},
		{"grep", "Search text.", []string{"pattern"}},
		{"stat", "Stat a path.", []string{"path"}},
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "eco-poc", Version: "0.0.1"}, nil)
	for _, spec := range specs {
		server.AddTool(&mcp.Tool{
			Name:        spec.name,
			Description: spec.description,
			InputSchema: toolSchema(spec.params),
		}, okHandler)
	}

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		log.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "eco-poc-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer session.Close()

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		log.Fatal(err)
	}

	raw, err := json.Marshal(result)
	if err != nil {
		log.Fatal(err)
	}
	pretty, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		log.Fatal(err)
	}

	outDir := filepath.Join("..", "out")
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}
	jsonPath := filepath.Join(outDir, "c5-tools-list.json")
	if err := os.WriteFile(jsonPath, append(pretty, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("tools_count: %d\n", len(result.Tools))
	fmt.Printf("tools_list_bytes_compact: %d\n", len(raw))
	fmt.Printf("tools_list_bytes_indented: %d\n", len(pretty))
	fmt.Printf("valid_utf8: %t\n", utf8.Valid(raw))
	fmt.Printf("tools_list_tokens_est_bytes_div_4: %.2f\n", float64(len(raw))/4)
	fmt.Printf("tools_list_json_file: %s\n", jsonPath)
}