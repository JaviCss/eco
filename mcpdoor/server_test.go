package mcpdoor

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/JaviCss/eco/port"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxToolsListBytes = 2048

type stdioPipes struct {
	serverReader *bufio.Reader
	serverWriter *os.File
	clientReader *os.File
	clientWriter *os.File
	closers      []io.Closer
}

func (p *stdioPipes) close() {
	for _, closer := range p.closers {
		closer.Close()
	}
}

func newStdioPipes() (*stdioPipes, error) {
	inRead, inWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		inRead.Close()
		inWrite.Close()
		return nil, err
	}
	return &stdioPipes{
		serverReader: bufio.NewReader(inRead),
		serverWriter: outWrite,
		clientReader: outRead,
		clientWriter: inWrite,
		closers:      []io.Closer{inRead, inWrite, outRead, outWrite},
	}, nil
}

type doorSession struct {
	session *mcp.ClientSession
	cancel  context.CancelFunc
	server  *Server
}

func newDoorSession(t *testing.T, client Client) *doorSession {
	t.Helper()
	pipes, err := newStdioPipes()
	if err != nil {
		t.Fatalf("stdio pipes: %v", err)
	}

	server := New(client)
	ctx, cancel := context.WithCancel(context.Background())
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(ctx, pipes.serverReader, pipes.serverWriter)
	}()

	clientTransport := &mcp.IOTransport{
		Reader: pipes.clientReader,
		Writer: pipes.clientWriter,
	}

	impl := &mcp.Implementation{Name: "eco-test-client", Version: "0.0.1"}
	session, err := mcp.NewClient(impl, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		cancel()
		pipes.close()
		t.Fatalf("client connect over stdio pipes: %v", err)
	}

	t.Cleanup(func() {
		session.Close()
		cancel()
		pipes.close()
		select {
		case <-serveErr:
		case <-time.After(5 * time.Second):
		}
	})

	return &doorSession{session: session, cancel: cancel, server: server}
}

func (s *doorSession) listTools(t *testing.T) *mcp.ListToolsResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := s.session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	return result
}

func (s *doorSession) callTool(t *testing.T, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := s.session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("tools/call %s: %v", name, err)
	}
	return result
}

func TestStdioToolsListHasExactlyFourToolsUnderCeiling(t *testing.T) {
	session := newDoorSession(t, port.NewFake())
	result := session.listTools(t)

	want := map[string]bool{"eco_read": true, "eco_search": true, "eco_append": true, "eco_probe": true}
	got := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		got = append(got, tool.Name)
	}
	sort.Strings(got)
	wantNames := []string{"eco_append", "eco_probe", "eco_read", "eco_search"}
	if len(got) != len(wantNames) {
		t.Fatalf("tools = %d (%v), want %d", len(got), got, len(wantNames))
	}
	for i, name := range wantNames {
		if got[i] != name {
			t.Fatalf("tools[%d] = %q, want %q", i, got[i], name)
		}
		if !want[name] {
			t.Fatalf("unexpected tool %q", name)
		}
	}

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal tools/list: %v", err)
	}
	t.Logf("tools_list_bytes_compact = %d", len(raw))
	if len(raw) > maxToolsListBytes {
		t.Fatalf("tools/list compact bytes = %d, want <= %d", len(raw), maxToolsListBytes)
	}

	compact := string(raw)
	for _, banned := range []string{"promote", `"get"`, "Manifest", "streamable", "StreamableHTTP"} {
		if strings.Contains(compact, banned) {
			t.Fatalf("tools/list payload mentions %q", banned)
		}
	}
}

func TestStdioEnumRejectsAxesBeforeThePort(t *testing.T) {
	spyPort := newSpy()
	session := newDoorSession(t, spyPort)

	cases := []struct {
		name string
		axis string
	}{
		{"eco_append", "R"},
		{"eco_append", "Z2"},
	}
	for _, tc := range cases {
		result := session.callTool(t, tc.name, map[string]any{"axis": tc.axis, "body": "x"})
		if !result.IsError {
			t.Fatalf("%s axis=%s: IsError = false, want a schema error", tc.name, tc.axis)
		}
		text := textOf(t, result)
		if !strings.Contains(text, "axis") {
			t.Fatalf("%s axis=%s: text = %q, want a schema complaint about axis", tc.name, tc.axis, text)
		}
		t.Logf("%s axis=%s rejected by schema: %s", tc.name, tc.axis, text)
	}

	if calls := spyPort.recorded(); len(calls) != 0 {
		t.Fatalf("port calls = %v, want none (schema must reject before the port)", calls)
	}
}

func TestStdioReadTruncates(t *testing.T) {
	fake := port.NewFake()
	seedZ3(t, fake, 500, 1024)
	session := newDoorSession(t, fake)

	result := session.callTool(t, "eco_read", map[string]any{"axis": "Z3", "limit": maxLimit})
	if result.IsError {
		t.Fatalf("IsError = true, text = %q", textOf(t, result))
	}
	text := textOf(t, result)
	t.Logf("tools_call_result_bytes = %d", len(text))
	if len(text) > MaxToolResultBytes {
		t.Fatalf("result bytes = %d, want <= %d", len(text), MaxToolResultBytes)
	}
	var decoded entriesPayload
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if !decoded.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if decoded.Omitted <= 0 {
		t.Fatalf("omitted = %d, want > 0", decoded.Omitted)
	}
	if len(decoded.Entries)+decoded.Omitted != maxLimit {
		t.Fatalf("kept %d + omitted %d != %d", len(decoded.Entries), decoded.Omitted, maxLimit)
	}
	for i, entry := range decoded.Entries {
		if len(entry.Body) != 1024 {
			t.Fatalf("entries[%d].Body length = %d, want 1024", i, len(entry.Body))
		}
	}
}

func TestStdioAppendReadSearchRoundTrip(t *testing.T) {
	fake := port.NewFake()
	session := newDoorSession(t, fake)

	appendResult := session.callTool(t, "eco_append", map[string]any{
		"axis":  "Z3",
		"id":    "note-1",
		"body":  "el proverbio del desarrollo",
		"attrs": map[string]string{"kind": "proverbio"},
	})
	if appendResult.IsError {
		t.Fatalf("append IsError = true, text = %q", textOf(t, appendResult))
	}

	readResult := session.callTool(t, "eco_read", map[string]any{"axis": "Z3", "limit": 10})
	if readResult.IsError {
		t.Fatalf("read IsError = true, text = %q", textOf(t, readResult))
	}
	if !strings.Contains(textOf(t, readResult), "note-1") {
		t.Fatalf("read payload does not mention note-1: %s", textOf(t, readResult))
	}

	searchResult := session.callTool(t, "eco_search", map[string]any{"axis": "Z3", "query": "proverbio", "limit": 10})
	if searchResult.IsError {
		t.Fatalf("search IsError = true, text = %q", textOf(t, searchResult))
	}
	if !strings.Contains(textOf(t, searchResult), "note-1") {
		t.Fatalf("search payload does not mention note-1: %s", textOf(t, searchResult))
	}
}

func TestStdioErrorsNeverLeakBasePaths(t *testing.T) {
	const secretPath = "C:/secret/base.db"

	sentinels := []struct {
		err  error
		text string
	}{
		{port.ErrNotFound, "eco: not found"},
		{port.ErrForbidden, "eco: forbidden"},
		{port.ErrInvalidEntry, "eco: invalid entry"},
		{port.ErrUnavailable, "eco: unavailable"},
		{port.ErrAxisNotInScope, "eco: axis not in scope"},
		{port.ErrInvalidScope, "eco: invalid scope"},
	}

	for _, sentinel := range sentinels {
		spyPort := newSpy()
		spyPort.readErr = &pathError{msg: "eco: read: open " + secretPath, wrapped: sentinel.err}
		spyPort.searchErr = &pathError{msg: "eco: search: open " + secretPath, wrapped: sentinel.err}
		spyPort.appendErr = &pathError{msg: "eco: append: open " + secretPath, wrapped: sentinel.err}
		spyPort.probeErr = &pathError{msg: "eco: probe: open " + secretPath, wrapped: sentinel.err}
		session := newDoorSession(t, spyPort)

		results := map[string]*mcp.CallToolResult{
			"eco_read":   session.callTool(t, "eco_read", map[string]any{"axis": "Z3", "limit": 5}),
			"eco_search": session.callTool(t, "eco_search", map[string]any{"axis": "Z3", "query": "q", "limit": 5}),
			"eco_append": session.callTool(t, "eco_append", map[string]any{"axis": "Z3", "id": "x", "body": "b"}),
			"eco_probe":  session.callTool(t, "eco_probe", map[string]any{}),
		}

		for name, result := range results {
			text := textOf(t, result)
			for _, leak := range []string{secretPath, "C:/secret", "base.db", "C:\\"} {
				if strings.Contains(text, leak) {
					t.Fatalf("%s: response text leaks %q: %s", name, leak, text)
				}
			}
			if name == "eco_probe" {
				var decoded probePayload
				if err := json.Unmarshal([]byte(text), &decoded); err != nil {
					t.Fatalf("eco_probe payload is not JSON: %v", err)
				}
				continue
			}
			if !result.IsError {
				t.Fatalf("%s: IsError = false, want true", name)
			}
			if text != sentinel.text {
				t.Fatalf("%s: text = %q, want %q", name, text, sentinel.text)
			}
		}
	}
}

func TestStdioUnmappedStoreErrorStaysOpaque(t *testing.T) {
	spyPort := newSpy()
	spyPort.readErr = &pathError{msg: "eco: read: open C:/secret/base.db: disk on fire", wrapped: boomErr{}}
	session := newDoorSession(t, spyPort)

	result := session.callTool(t, "eco_read", map[string]any{"axis": "Z3", "limit": 5})
	if !result.IsError {
		t.Fatal("IsError = false, want true")
	}
	text := textOf(t, result)
	if text != "eco: unavailable" {
		t.Fatalf("text = %q, want %q", text, "eco: unavailable")
	}
	if strings.Contains(text, "secret") || strings.Contains(text, "disk on fire") {
		t.Fatalf("text leaks the raw store error: %s", text)
	}
}

func TestStdioProbeReportsTypedCodes(t *testing.T) {
	healthy := port.NewFake()
	session := newDoorSession(t, healthy)
	result := session.callTool(t, "eco_probe", map[string]any{})
	if result.IsError {
		t.Fatalf("IsError = true, want false")
	}
	var decoded probePayload
	if err := json.Unmarshal([]byte(textOf(t, result)), &decoded); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if decoded.Status != "ok" {
		t.Fatalf("status = %q, want %q", decoded.Status, "ok")
	}

	down := port.NewFake()
	down.SetUnavailable(true)
	downSession := newDoorSession(t, down)
	downResult := downSession.callTool(t, "eco_probe", map[string]any{})
	if downResult.IsError {
		t.Fatalf("IsError = true, want false for an unavailable port (typed code instead)")
	}
	if err := json.Unmarshal([]byte(textOf(t, downResult)), &decoded); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if decoded.Status != "unavailable" {
		t.Fatalf("status = %q, want %q", decoded.Status, "unavailable")
	}

	forbidden := newSpy()
	forbidden.probeErr = port.ErrForbidden
	forbiddenSession := newDoorSession(t, forbidden)
	forbiddenResult := forbiddenSession.callTool(t, "eco_probe", map[string]any{})
	if forbiddenResult.IsError {
		t.Fatal("IsError = true, want false")
	}
	if err := json.Unmarshal([]byte(textOf(t, forbiddenResult)), &decoded); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if decoded.Status != "forbidden" {
		t.Fatalf("status = %q, want %q", decoded.Status, "forbidden")
	}
}

type pathError struct {
	msg     string
	wrapped error
}

func (e *pathError) Error() string { return e.msg }

func (e *pathError) Unwrap() error { return e.wrapped }
