package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/JaviCss/eco/httpdoor"
	"github.com/JaviCss/eco/port"
	"github.com/JaviCss/eco/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func verbsOf(out string) []string {
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "eco" {
			seen[fields[1]] = true
		}
	}
	list := make([]string, 0, len(seen))
	for verb := range seen {
		list = append(list, verb)
	}
	sort.Strings(list)
	return list
}

func toolText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	var out []string
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			out = append(out, text.Text)
		}
	}
	return strings.Join(out, "\n")
}

func runtimeStore(t *testing.T, user, project string) *store.Store {
	t.Helper()
	s, err := store.Open(store.Config{
		UserDB:    user,
		ProjectDB: project,
		Origin:    "test",
		Profile:   store.ProfileRuntime,
	})
	if err != nil {
		t.Fatalf("open the bases for the assertion: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mcpSession(t *testing.T, binary, user, project string) *mcp.ClientSession {
	t.Helper()
	transport := &mcp.CommandTransport{
		Command: exec.Command(binary, "mcp", "--user-db", user, "--project-db", project),
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "eco-cli-test", Version: "0.0.1"}, nil)
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("connect to eco mcp over stdio: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func TestHelpListsTheTenVerbs(t *testing.T) {
	binary := buildEco(t)
	out, code := run(t, binary, "--help")
	if code != 0 {
		t.Fatalf("--help exited %d:\n%s", code, out)
	}
	got := verbsOf(out)
	want := []string{"append", "doctor", "get", "help", "mcp", "probe", "promote", "read", "search", "serve"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("--help lists %v, want exactly %v", got, want)
	}
}

func TestMCPWithoutBasesIsAUsageError(t *testing.T) {
	binary := buildEco(t)
	out, code := run(t, binary, "mcp")
	if code != exitUsage {
		t.Fatalf("mcp without --user-db and --project-db exited %d, want %d:\n%s", code, exitUsage, out)
	}
	if !strings.Contains(out, "--user-db") {
		t.Fatalf("the refusal does not name the missing flags:\n%s", out)
	}
	t.Logf("MCP_WITHOUT_BASES_EXIT=%d OUT=%s", code, strings.TrimSpace(out))
}

func TestMCPExposesFourToolsOverStdio(t *testing.T) {
	binary := buildEco(t)
	dir := t.TempDir()
	user := filepath.Join(dir, "user.db")
	project := filepath.Join(dir, "project.db")
	seedBases(t, user, project)
	session := mcpSession(t, binary, user, project)
	list, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	names := make([]string, 0, len(list.Tools))
	schemas := map[string]string{}
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
		raw, marshalErr := json.Marshal(tool.InputSchema)
		if marshalErr != nil {
			t.Fatalf("marshal the schema of %s: %v", tool.Name, marshalErr)
		}
		schemas[tool.Name] = string(raw)
	}
	sort.Strings(names)
	want := []string{"eco_append", "eco_probe", "eco_read", "eco_search"}
	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Fatalf("tools/list = %v, want exactly %v", names, want)
	}
	raw, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("marshal tools/list: %v", err)
	}
	t.Logf("tools=%v tools_list_bytes_compact=%d", names, len(raw))
	if len(raw) > 2048 {
		t.Fatalf("tools/list weighs %d bytes, want at most 2048", len(raw))
	}
	if !strings.Contains(schemas["eco_append"], `"Z3"`) || strings.Contains(schemas["eco_append"], `"Z2"`) {
		t.Fatalf("eco_append's enum must be {Z3,Y,X}: %s", schemas["eco_append"])
	}
	for _, tool := range []string{"eco_read", "eco_search"} {
		if !strings.Contains(schemas[tool], `"Z2"`) {
			t.Fatalf("%s's enum must be {Z2,Z3,Y,X}: %s", tool, schemas[tool])
		}
	}
	if strings.Contains(schemas["eco_read"], `"R"`) || strings.Contains(schemas["eco_append"], `"R"`) {
		t.Fatalf("no schema may name R: read=%s append=%s", schemas["eco_read"], schemas["eco_append"])
	}
}

func TestMCPAppendOnZ2IsRefusedAndChangesNothing(t *testing.T) {
	binary := buildEco(t)
	dir := t.TempDir()
	user := filepath.Join(dir, "user.db")
	project := filepath.Join(dir, "project.db")
	seedBases(t, user, project)
	session := mcpSession(t, binary, user, project)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "eco_append",
		Arguments: map[string]any{"axis": "Z2", "body": "the agent smuggled a distilled truth"},
	})
	if err != nil {
		t.Fatalf("CallTool eco_append: %v", err)
	}
	text := toolText(t, result)
	t.Logf("is_error=%t text=%s", result.IsError, text)
	if !result.IsError {
		t.Fatal("eco_append with axis Z2 must come back as an error")
	}
	if strings.Contains(text, user) || strings.Contains(text, project) || strings.Contains(text, dir) {
		t.Fatalf("the refusal carries the base path:\n%s", text)
	}
	check := runtimeStore(t, user, project)
	if _, err := check.Get(context.Background(), port.ScopeUser, port.AxisZ2, []string{"the-agent-smuggled-a-distilled-truth"}); err == nil {
		t.Fatal("the refused append reached Z2")
	}
	if entries, err := check.Read(context.Background(), port.ScopeUser, port.AxisZ2, 10); err != nil {
		t.Fatalf("read Z2: %v", err)
	} else if len(entries) != 0 {
		t.Fatalf("Z2 holds %d entries after the refused append, want 0: %v", len(entries), entries)
	}
}

func TestMCPEcoProbeAnswersOK(t *testing.T) {
	binary := buildEco(t)
	dir := t.TempDir()
	user := filepath.Join(dir, "user.db")
	project := filepath.Join(dir, "project.db")
	seedBases(t, user, project)
	session := mcpSession(t, binary, user, project)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "eco_probe", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool eco_probe: %v", err)
	}
	if result.IsError {
		t.Fatalf("eco_probe on a healthy pair: %s", toolText(t, result))
	}
	if text := toolText(t, result); !strings.Contains(text, `"ok"`) {
		t.Fatalf("eco_probe must answer with a typed code: %s", text)
	}
}

func TestCLIExitCodes(t *testing.T) {
	binary := buildEco(t)
	dir := t.TempDir()
	user := filepath.Join(dir, "user.db")
	project := filepath.Join(dir, "project.db")
	seedBases(t, user, project)

	if out, code := run(t, binary, "read", "--user-db", user, "--axis", "Z2"); code != exitUsage {
		t.Fatalf("read without --project-db exited %d, want %d:\n%s", code, exitUsage, out)
	}
	if out, code := run(t, binary, "read", "--user-db", user, "--project-db", project, "--axis", "R", "--limit", "10"); code != exitForbidden {
		t.Fatalf("read on R with the human profile exited %d, want %d:\n%s", code, exitForbidden, out)
	}
	if out, code := run(t, binary, "read", "--user-db", user, "--project-db", project, "--axis", "Q", "--limit", "10"); code != exitInvalid {
		t.Fatalf("read on an axis without a scope exited %d, want %d:\n%s", code, exitInvalid, out)
	}
	if out, code := run(t, binary, "append", "--user-db", user, "--project-db", project, "--axis", "Z2", "--body", "smuggled"); code != exitForbidden {
		t.Fatalf("append on Z2 exited %d, want %d:\n%s", code, exitForbidden, out)
	}
	if out, code := run(t, binary, "promote", "--user-db", user, "--project-db", project, "--ids", "ghost"); code != exitNotFound {
		t.Fatalf("promote with a missing id exited %d, want %d:\n%s", code, exitNotFound, out)
	}
	check := runtimeStore(t, user, project)
	if entries, err := check.Read(context.Background(), port.ScopeUser, port.AxisZ2, 10); err != nil {
		t.Fatalf("read Z2: %v", err)
	} else if len(entries) != 0 {
		t.Fatalf("Z2 holds %d entries after the refused commands, want 0: %v", len(entries), entries)
	}

	if out, code := run(t, binary, "append", "--user-db", user, "--project-db", project, "--axis", "Z3", "--id", "distilled", "--body", "the distilled truth"); code != 0 {
		t.Fatalf("append on Z3 exited %d:\n%s", code, out)
	}
	if out, code := run(t, binary, "promote", "--user-db", user, "--project-db", project, "--ids", "distilled"); code != 0 {
		t.Fatalf("promote exited %d:\n%s", code, out)
	}
	out, code := run(t, binary, "get", "--user-db", user, "--project-db", project, "--axis", "Z2", "--ids", "promoted:distilled")
	if code != 0 {
		t.Fatalf("get exited %d:\n%s", code, out)
	}
	var payload entriesOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &payload); err != nil {
		t.Fatalf("the get output is not JSON (%v):\n%s", err, out)
	}
	if len(payload.Entries) != 1 {
		t.Fatalf("get returned %d entries, want 1:\n%s", len(payload.Entries), out)
	}
	attrs := payload.Entries[0].Attrs
	if attrs["source_origin"] == "" || attrs["promoted_from"] != "distilled" {
		t.Fatalf("the promoted entry lost its provenance: %v", attrs)
	}
	t.Logf("promoted attrs: %v", attrs)
}

func TestCLIErrorTextNeverCarriesTheBasePath(t *testing.T) {
	binary := buildEco(t)
	dir := t.TempDir()
	user := filepath.Join(dir, "user.db")
	project := filepath.Join(dir, "project.db")
	seedBases(t, user, project)
	for _, args := range [][]string{
		{"get", "--user-db", user, "--project-db", project, "--axis", "Z2", "--ids", "ghost"},
		{"search", "--user-db", user, "--project-db", project, "--axis", "Z2", "--query", "no such datum"},
		{"append", "--user-db", user, "--project-db", project, "--axis", "Z2", "--body", "smuggled"},
		{"promote", "--user-db", user, "--project-db", project, "--ids", "ghost"},
		{"get", "--user-db", filepath.Join(dir, "missing", "user.db"), "--project-db", project, "--axis", "Z2", "--ids", "ghost"},
	} {
		out, code := run(t, binary, args...)
		if code == 0 {
			t.Fatalf("%v exited 0:\n%s", args, out)
		}
		for _, secret := range []string{user, project, dir} {
			if strings.Contains(out, secret) {
				t.Fatalf("%v leaked %q:\n%s", args, secret, out)
			}
		}
		t.Logf("%v exited %d: %s", args, code, strings.TrimSpace(out))
	}
}

func waitForFile(t *testing.T, path string, limit time.Duration) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s did not appear within %s", path, limit)
}

func TestProbeOverThePortFileValidatesTheNonce(t *testing.T) {
	binary := buildEco(t)
	dir := t.TempDir()
	user := filepath.Join(dir, "user.db")
	project := filepath.Join(dir, "project.db")
	seedBases(t, user, project)
	portFile := filepath.Join(dir, "eco.port")
	server := exec.Command(binary, "serve",
		"--user-db", user, "--project-db", project,
		"--parent-pid", strconv.Itoa(os.Getpid()), "--port-file", portFile)
	server.SysProcAttr = nil
	if err := server.Start(); err != nil {
		t.Fatalf("start eco serve: %v", err)
	}
	defer func() {
		server.Process.Kill()
		server.Wait()
	}()
	waitForFile(t, portFile, 15*time.Second)

	out, code := run(t, binary, "probe", "--port-file", portFile)
	if code != 0 {
		t.Fatalf("probe over the port file exited %d:\n%s", code, out)
	}
	if !strings.Contains(out, `"nonce"`) {
		t.Fatalf("probe must report the nonce it validated:\n%s", out)
	}
	t.Logf("probe: %s", strings.TrimSpace(out))

	info, err := httpdoor.ReadPortFile(portFile)
	if err != nil {
		t.Fatalf("read the port file: %v", err)
	}
	info.Nonce = "0fa1c0de-not-the-nonce"
	if err := httpdoor.WritePortFile(portFile, info); err != nil {
		t.Fatalf("write the tampered port file: %v", err)
	}
	out, code = run(t, binary, "probe", "--port-file", portFile)
	if code != exitUnavailable {
		t.Fatalf("probe with a nonce that does not match exited %d, want %d:\n%s", code, exitUnavailable, out)
	}
	if strings.Contains(out, portFile) || strings.Contains(out, dir) {
		t.Fatalf("the failure carries the port file path:\n%s", out)
	}
	t.Logf("tampered probe: %s", strings.TrimSpace(out))
}