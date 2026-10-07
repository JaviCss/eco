package httpdoor_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JaviCss/eco/httpdoor"
	"github.com/JaviCss/eco/port"
	"github.com/JaviCss/eco/store"
)

const testToken = "tok-0123456789abcdef"

type door struct {
	t     *testing.T
	url   string
	token string
}

func startDoor(t *testing.T, client port.Port) door {
	t.Helper()
	srv, err := httpdoor.New(client, httpdoor.Config{Addr: "127.0.0.1:0", Token: testToken, Nonce: "nonce-1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for srv.Addr() == "" {
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("Serve never published an address")
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Serve returned %v, want nil", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Serve did not return after the context was cancelled")
		}
	})
	return door{t: t, url: "http://" + srv.Addr(), token: testToken}
}

func (d door) post(path string, body any, mutate func(*http.Request)) (int, []byte) {
	d.t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		d.t.Fatalf("marshal: %v", err)
	}
	return d.raw(http.MethodPost, path, raw, mutate)
}

func (d door) raw(method, path string, raw []byte, mutate func(*http.Request)) (int, []byte) {
	d.t.Helper()
	req, err := http.NewRequest(method, d.url+path, bytes.NewReader(raw))
	if err != nil {
		d.t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if mutate != nil {
		mutate(req)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		d.t.Fatalf("do %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		d.t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, payload
}

func (d door) authed(method, path string, raw []byte) (int, []byte) {
	d.t.Helper()
	return d.raw(method, path, raw, func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+d.token)
	})
}

func (d door) authedPost(path string, body any) (int, []byte) {
	d.t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		d.t.Fatalf("marshal: %v", err)
	}
	return d.authed(http.MethodPost, path, raw)
}

func decode(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("decode %q: %v", string(payload), err)
	}
	return out
}

func assertError(t *testing.T, payload []byte, wantName string) {
	t.Helper()
	got := decode(t, payload)
	if got["error"] != wantName {
		t.Fatalf("error body: got %q, want %q", got["error"], wantName)
	}
}

func readBody(scope port.Scope, axis port.Axis, limit int) map[string]any {
	return map[string]any{"scope": string(scope), "axis": string(axis), "limit": limit}
}

func TestNewRejectsNonLoopbackAddrs(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:0", "[::]:0", "1.2.3.4:9", "localhost:0", "127.0.0.1"} {
		_, err := httpdoor.New(port.NewFake(), httpdoor.Config{Addr: addr, Token: testToken})
		if err == nil {
			t.Fatalf("New with %q: got nil, want a typed error", addr)
		}
		if !strings.Contains(err.Error(), "forbidden") {
			t.Fatalf("New with %q: got %v, want it to wrap port.ErrForbidden", addr, err)
		}
	}
}

func TestNewWithoutTokenIsForbidden(t *testing.T) {
	_, err := httpdoor.New(port.NewFake(), httpdoor.Config{Addr: "127.0.0.1:0"})
	if err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("New without a token: got %v, want it to wrap port.ErrForbidden", err)
	}
}

func TestServeBindsLoopbackV4AndV6(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:0", "[::1]:0"} {
		srv, err := httpdoor.New(port.NewFake(), httpdoor.Config{Addr: addr, Token: testToken})
		if err != nil {
			t.Fatalf("New %q: %v", addr, err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- srv.Serve(ctx) }()
		deadline := time.Now().Add(5 * time.Second)
		for srv.Addr() == "" && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if srv.Addr() == "" {
			cancel()
			t.Fatalf("Serve %q never published an address", addr)
		}
		if srv.Port() <= 0 {
			cancel()
			t.Fatalf("Serve %q: Port() = %d, want a real port", addr, srv.Port())
		}
		if !strings.HasSuffix(srv.Addr(), fmt.Sprintf(":%d", srv.Port())) {
			cancel()
			t.Fatalf("Serve %q: Addr() = %q does not carry Port() = %d", addr, srv.Addr(), srv.Port())
		}
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Serve %q returned %v, want nil", addr, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("Serve %q did not return after cancel", addr)
		}
	}
}

func TestHeaderOrder(t *testing.T) {
	d := startDoor(t, port.NewFake())
	body, err := json.Marshal(readBody(port.ScopeUser, port.AxisZ2, 10))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("host", func(t *testing.T) {
		code, payload := d.authed(http.MethodPost, "/v1/read", body)
		_ = payload
		if code != http.StatusOK {
			t.Fatalf("sanity read: got %d, want 200", code)
		}
		code, payload = d.raw(http.MethodPost, "/v1/read", body, func(req *http.Request) {
			req.Host = "eco.example:9999"
			req.Header.Set("Authorization", "Bearer "+d.token)
		})
		if code != 421 {
			t.Fatalf("foreign Host: got %d, want 421 (%s)", code, payload)
		}
		assertError(t, payload, "host mismatch")
	})

	t.Run("origin", func(t *testing.T) {
		code, payload := d.raw(http.MethodPost, "/v1/read", body, func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer "+d.token)
			req.Header.Set("Origin", "http://evil.example")
		})
		if code != 403 {
			t.Fatalf("Origin present: got %d, want 403 (%s)", code, payload)
		}
		assertError(t, payload, "forbidden")
	})

	t.Run("content type", func(t *testing.T) {
		code, payload := d.raw(http.MethodPost, "/v1/read", body, func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer "+d.token)
			req.Header.Set("Content-Type", "text/plain")
		})
		if code != 415 {
			t.Fatalf("Content-Type text/plain: got %d, want 415 (%s)", code, payload)
		}
		assertError(t, payload, "unsupported media type")
	})

	t.Run("charset is accepted", func(t *testing.T) {
		code, _ := d.raw(http.MethodPost, "/v1/read", body, func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer "+d.token)
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
		})
		if code != http.StatusOK {
			t.Fatalf("Content-Type with charset: got %d, want 200", code)
		}
	})

	t.Run("body too large", func(t *testing.T) {
		huge := make([]byte, httpdoor.MaxBodyBytesRequest+1)
		for i := range huge {
			huge[i] = 'x'
		}
		code, payload := d.authed(http.MethodPost, "/v1/read", huge)
		if code != 413 {
			t.Fatalf("body of %d bytes: got %d, want 413 (%s)", len(huge), code, payload)
		}
		assertError(t, payload, "payload too large")
	})
}

func TestAuthorizationIsRequiredOnEveryRoute(t *testing.T) {
	d := startDoor(t, port.NewFake())
	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"read", http.MethodPost, "/v1/read", `{"scope":"user","axis":"Z2","limit":10}`},
		{"search", http.MethodPost, "/v1/search", `{"scope":"user","axis":"Z2","query":"x","limit":10}`},
		{"get", http.MethodPost, "/v1/get", `{"scope":"user","axis":"Z2","ids":["a"]}`},
		{"append", http.MethodPost, "/v1/append", `{"scope":"user","axis":"Z2","entry":{"ID":"a","Body":"x"}}`},
		{"promote", http.MethodPost, "/v1/promote", `{"ids":["a"]}`},
		{"probe", http.MethodGet, "/v1/probe", ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, payload := d.raw(tc.method, tc.path, []byte(tc.body), nil)
			if code != 401 {
				t.Fatalf("anonymous %s: got %d, want 401 (%s)", tc.name, code, payload)
			}
			assertError(t, payload, "unauthorized")
		})
	}

	t.Run("foreign token", func(t *testing.T) {
		code, payload := d.raw(http.MethodPost, "/v1/read", []byte(`{"scope":"user","axis":"Z2","limit":10}`), func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer not-the-token")
		})
		if code != 401 {
			t.Fatalf("foreign token: got %d, want 401 (%s)", code, payload)
		}
		assertError(t, payload, "unauthorized")
	})

	t.Run("right token", func(t *testing.T) {
		code, payload := d.authed(http.MethodGet, "/v1/probe", nil)
		if code != http.StatusOK {
			t.Fatalf("right token: got %d, want 200 (%s)", code, payload)
		}
	})
}

func TestUnknownRouteAndMethod(t *testing.T) {
	d := startDoor(t, port.NewFake())
	code, payload := d.authedPost("/v1/nope", map[string]any{})
	if code != 404 {
		t.Fatalf("unknown route: got %d, want 404 (%s)", code, payload)
	}
	assertError(t, payload, "unknown route")

	code, payload = d.authed(http.MethodGet, "/v1/read", nil)
	if code != 405 {
		t.Fatalf("GET /v1/read: got %d, want 405 (%s)", code, payload)
	}
	assertError(t, payload, "method not allowed")
}

func TestHappyPathReadSearchGetAppendProbe(t *testing.T) {
	fake := port.NewFake()
	d := startDoor(t, fake)
	ctx := context.Background()

	code, payload := d.authedPost("/v1/append", map[string]any{
		"scope": "project", "axis": "Z3",
		"entry": map[string]any{"ID": "z3-1", "Body": "the needle is here", "Attrs": map[string]string{"kind": "note"}},
	})
	if code != 200 {
		t.Fatalf("append: got %d, want 200 (%s)", code, payload)
	}
	appended := decode(t, payload)["entries"].([]any)
	if len(appended) != 1 {
		t.Fatalf("append: got %d entries, want 1", len(appended))
	}
	first := appended[0].(map[string]any)
	if first["ID"] != "z3-1" || first["Axis"] != "Z3" || first["Scope"] != "project" {
		t.Fatalf("append entry: got %+v", first)
	}
	if first["Attrs"].(map[string]any)["kind"] != "note" {
		t.Fatalf("append attrs: got %+v", first["Attrs"])
	}

	code, payload = d.authedPost("/v1/read", readBody(port.ScopeProject, port.AxisZ3, 10))
	if code != 200 {
		t.Fatalf("read: got %d, want 200 (%s)", code, payload)
	}
	if got := decode(t, payload); got["truncated"] != false || got["omitted"].(float64) != 0 {
		t.Fatalf("read flags: got %+v", got)
	}

	code, payload = d.authedPost("/v1/search", map[string]any{"scope": "project", "axis": "Z3", "query": "needle", "limit": 10})
	if code != 200 {
		t.Fatalf("search: got %d, want 200 (%s)", code, payload)
	}
	if len(decode(t, payload)["entries"].([]any)) != 1 {
		t.Fatalf("search: got %s", payload)
	}

	code, payload = d.authedPost("/v1/get", map[string]any{"scope": "project", "axis": "Z3", "ids": []string{"z3-1"}})
	if code != 200 {
		t.Fatalf("get: got %d, want 200 (%s)", code, payload)
	}
	if len(decode(t, payload)["entries"].([]any)) != 1 {
		t.Fatalf("get: got %s", payload)
	}

	code, payload = d.authed(http.MethodGet, "/v1/probe", nil)
	if code != 200 {
		t.Fatalf("probe: got %d, want 200 (%s)", code, payload)
	}
	if decode(t, payload)["ok"] != true {
		t.Fatalf("probe body: got %s", payload)
	}

	if _, err := fake.Get(ctx, port.ScopeProject, port.AxisZ3, []string{"z3-1"}); err != nil {
		t.Fatalf("the entry is not in the fake: %v", err)
	}
}

func TestErrorsAreTyped(t *testing.T) {
	fake := port.NewFake()
	d := startDoor(t, fake)

	code, payload := d.authedPost("/v1/get", map[string]any{"scope": "project", "axis": "Z3", "ids": []string{"missing"}})
	if code != 404 {
		t.Fatalf("missing id: got %d, want 404 (%s)", code, payload)
	}
	assertError(t, payload, "not found")

	code, payload = d.authedPost("/v1/read", readBody("nope", "Z2", 10))
	if code != 400 {
		t.Fatalf("bad scope: got %d, want 400 (%s)", code, payload)
	}
	assertError(t, payload, "invalid scope")

	code, payload = d.authedPost("/v1/read", readBody(port.ScopeUser, port.AxisZ3, 10))
	if code != 400 {
		t.Fatalf("axis out of scope: got %d, want 400 (%s)", code, payload)
	}
	assertError(t, payload, "axis not in scope")

	code, payload = d.authedPost("/v1/append", map[string]any{"scope": "project", "axis": "Z3", "entry": map[string]any{"Body": "no id"}})
	if code != 400 {
		t.Fatalf("entry without id: got %d, want 400 (%s)", code, payload)
	}
	assertError(t, payload, "invalid entry")

	fake.SetUnavailable(true)
	code, payload = d.authed(http.MethodGet, "/v1/probe", nil)
	if code != 503 {
		t.Fatalf("probe while down: got %d, want 503 (%s)", code, payload)
	}
	assertError(t, payload, "unavailable")
	fake.SetUnavailable(false)
}

type leaky struct {
	port.Port
}

func (l leaky) Read(context.Context, port.Scope, port.Axis, int) ([]port.Entry, error) {
	return nil, fmt.Errorf("eco: Read: %w: cannot open C:/secret/base.db: no such file", port.ErrUnavailable)
}

func (l leaky) Search(context.Context, port.Scope, port.Axis, string, int) ([]port.Entry, error) {
	return nil, fmt.Errorf("eco: Search: %w: cannot open C:/secret/base.db: no such file", port.ErrUnavailable)
}

func (l leaky) Get(context.Context, port.Scope, port.Axis, []string) ([]port.Entry, error) {
	return nil, fmt.Errorf("eco: Get: %w: cannot open C:/secret/base.db: no such file", port.ErrUnavailable)
}

func (l leaky) Append(context.Context, port.Scope, port.Axis, port.Entry) (port.Entry, error) {
	return port.Entry{}, fmt.Errorf("eco: Append: %w: cannot open C:/secret/base.db: no such file", port.ErrUnavailable)
}

func (l leaky) Probe(context.Context) error {
	return fmt.Errorf("eco: Probe: %w: cannot open C:/secret/base.db: no such file", port.ErrUnavailable)
}

func TestNoBasePathLeaks(t *testing.T) {
	d := startDoor(t, leaky{Port: port.NewFake()})
	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"read", http.MethodPost, "/v1/read", `{"scope":"user","axis":"Z2","limit":10}`},
		{"search", http.MethodPost, "/v1/search", `{"scope":"user","axis":"Z2","query":"x","limit":10}`},
		{"get", http.MethodPost, "/v1/get", `{"scope":"user","axis":"Z2","ids":["a"]}`},
		{"append", http.MethodPost, "/v1/append", `{"scope":"user","axis":"Z2","entry":{"ID":"a","Body":"x"}}`},
		{"promote", http.MethodPost, "/v1/promote", `{"ids":["a"]}`},
		{"probe", http.MethodGet, "/v1/probe", ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, payload := d.authed(tc.method, tc.path, []byte(tc.body))
			if code != 503 {
				t.Fatalf("%s: got %d, want 503 (%s)", tc.name, code, payload)
			}
			text := string(payload)
			for _, forbidden := range []string{"secret", "base.db", "C:", "no such file"} {
				if strings.Contains(text, forbidden) {
					t.Fatalf("%s leaked %q: %s", tc.name, forbidden, text)
				}
			}
			assertError(t, payload, "unavailable")
		})
	}
}

func TestReadTruncatesToTheResultCeiling(t *testing.T) {
	fake := port.NewFake()
	d := startDoor(t, fake)
	ctx := context.Background()
	body := strings.Repeat("y", 1024)
	for i := 0; i < 500; i++ {
		if _, err := fake.Append(ctx, port.ScopeUser, port.AxisZ2, port.Entry{ID: fmt.Sprintf("z2-%03d", i), Body: body}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	code, payload := d.authedPost("/v1/read", readBody(port.ScopeUser, port.AxisZ2, 500))
	if code != 200 {
		t.Fatalf("read: got %d, want 200 (%s)", code, payload)
	}
	if len(payload) > httpdoor.MaxResultBytes {
		t.Fatalf("response of %d bytes exceeds the %d ceiling", len(payload), httpdoor.MaxResultBytes)
	}
	got := decode(t, payload)
	if got["truncated"] != true {
		t.Fatalf("truncated: got %v, want true", got["truncated"])
	}
	omitted := int(got["omitted"].(float64))
	if omitted <= 0 {
		t.Fatalf("omitted: got %d, want > 0", omitted)
	}
	kept := len(got["entries"].([]any))
	if kept+omitted != 500 {
		t.Fatalf("kept %d + omitted %d = %d, want 500", kept, omitted, kept+omitted)
	}
	t.Logf("TRUNCATED_BYTES=%d KEPT=%d OMITTED=%d", len(payload), kept, omitted)
}

type promoteTarget struct {
	*store.Store
	source port.Port
}

func (p promoteTarget) PromoteSource() port.Port { return p.source }

func TestPromoteFromTheFakeIntoAProfileHumanStore(t *testing.T) {
	dir := t.TempDir()
	target, err := store.Open(store.Config{
		UserDB:    filepath.Join(dir, "user.db"),
		ProjectDB: filepath.Join(dir, "project.db"),
		Origin:    "httpdoor-test",
		Profile:   store.ProfileHuman,
	})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = target.Close() })
	if _, err := target.Append(context.Background(), port.ScopeProject, port.AxisZ3, port.Entry{ID: "seed", Body: "already there"}); err != nil {
		t.Fatalf("seed the target: %v", err)
	}

	source := port.NewFake()
	if _, err := source.Append(context.Background(), port.ScopeProject, port.AxisZ3, port.Entry{ID: "z3-1", Body: "promote me"}); err != nil {
		t.Fatalf("seed the fake: %v", err)
	}

	d := startDoor(t, promoteTarget{Store: target, source: source})
	code, payload := d.authedPost("/v1/promote", map[string]any{"ids": []string{"z3-1"}})
	if code != 200 {
		t.Fatalf("promote: got %d, want 200 (%s)", code, payload)
	}
	entries := decode(t, payload)["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("promote: got %d entries, want 1 (%s)", len(entries), payload)
	}
	promoted := entries[0].(map[string]any)
	wantID := store.PromotedID("z3-1")
	if promoted["ID"] != wantID || promoted["Body"] != "promote me" || promoted["Axis"] != "Z2" || promoted["Scope"] != "user" {
		t.Fatalf("promoted entry: got %+v, want ID %s in user/Z2", promoted, wantID)
	}
	stored, err := target.Get(context.Background(), port.ScopeUser, port.AxisZ2, []string{wantID})
	if err != nil {
		t.Fatalf("the promoted entry is not in the store: %v", err)
	}
	if stored[0].Body != "promote me" {
		t.Fatalf("stored body: got %q", stored[0].Body)
	}
	if stored[0].Attrs["promoted_from"] != "z3-1" {
		t.Fatalf("promoted_from: got %+v", stored[0].Attrs)
	}
	t.Logf("PROMOTED=%s", wantID)
}

func TestPromoteIntoAFakeIsUnavailable(t *testing.T) {
	d := startDoor(t, port.NewFake())
	code, payload := d.authedPost("/v1/promote", map[string]any{"ids": []string{"z3-1"}})
	if code != 503 && code != 404 {
		t.Fatalf("promote into a fake: got %d, want 503 or 404 (%s)", code, payload)
	}
	text := string(payload)
	if strings.Contains(text, "fake.go") || strings.Contains(text, ".go:") {
		t.Fatalf("promote leaked an implementation path: %s", text)
	}
}

func TestWritePortFileIsAtomicAndPrivate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "eco.port")
	info := httpdoor.PortInfo{PID: 4242, Port: 54321, Nonce: "nonce-abc", Token: "tok-abc"}
	if err := httpdoor.WritePortFile(path, info); err != nil {
		t.Fatalf("WritePortFile: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("the port file is not JSON: %v (%q)", err, raw)
	}
	for key, want := range map[string]any{"pid": 4242.0, "port": 54321.0, "nonce": "nonce-abc", "token": "tok-abc"} {
		if fields[key] != want {
			t.Fatalf("port file %q: got %v, want %v (%q)", key, fields[key], want, raw)
		}
	}
	if len(fields) != 4 {
		t.Fatalf("port file carries extra fields: %q", raw)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range leftovers {
		if name != path {
			t.Fatalf("the temporary file survived: %s", name)
		}
	}

	read, err := httpdoor.ReadPortFile(path)
	if err != nil {
		t.Fatalf("ReadPortFile: %v", err)
	}
	if read != info {
		t.Fatalf("ReadPortFile: got %+v, want %+v", read, info)
	}

	acls, err := httpdoor.PortFileACL(path)
	if err != nil {
		t.Fatalf("PortFileACL: %v", err)
	}
	t.Logf("ACL=%v", acls)
	if len(acls) != 1 {
		t.Fatalf("the DACL has %d entries (%v), want only the current user", len(acls), acls)
	}
	for _, forbidden := range []string{"S-1-1-0", "S-1-5-32-545", "S-1-5-32-544"} {
		for _, ace := range acls {
			if strings.Contains(ace, forbidden) {
				t.Fatalf("the DACL grants %s to %v", forbidden, acls)
			}
		}
	}

	out, err := exec.Command("icacls", path).CombinedOutput()
	if err != nil {
		t.Fatalf("icacls: %v", err)
	}
	t.Logf("ICACLS_OUTPUT_START\n%s\nICACLS_OUTPUT_END", string(out))
	text := strings.ToLower(string(out))
	for _, forbidden := range []string{"everyone", "builtin\\users", "nt authority", "administrators"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("icacls still grants %s:\n%s", forbidden, out)
		}
	}
	current, err := user.Current()
	if err != nil {
		t.Fatalf("user.Current: %v", err)
	}
	if !strings.Contains(text, strings.ToLower(current.Username)) {
		t.Fatalf("icacls does not mention the current user %s:\n%s", current.Username, out)
	}
}

func TestReadPortFileRejectsGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "eco.port")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := httpdoor.ReadPortFile(path); err == nil {
		t.Fatal("ReadPortFile of garbage: got nil, want an error")
	}
}
