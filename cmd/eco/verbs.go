package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/JaviCss/eco/port"
	"github.com/JaviCss/eco/store"
)

type attrList []string

func (a *attrList) String() string {
	return strings.Join(*a, ",")
}

func (a *attrList) Set(value string) error {
	*a = append(*a, value)
	return nil
}

type verbFlags struct {
	userDB    string
	projectDB string
	axis      string
	limit     int
	query     string
	ids       string
	body      string
	id        string
	attrs     attrList
	portFile  string
	parentPID int
	addr      string
}

func parseFlags(verb string, args []string) *verbFlags {
	flags := flag.NewFlagSet(verb, flag.ContinueOnError)
	v := &verbFlags{}
	flags.StringVar(&v.userDB, "user-db", "", "path of the user base")
	flags.StringVar(&v.projectDB, "project-db", "", "path of the project base")
	flags.StringVar(&v.axis, "axis", "", "axis of the entries")
	flags.IntVar(&v.limit, "limit", 10, "how many entries")
	flags.StringVar(&v.query, "query", "", "text to look for")
	flags.StringVar(&v.ids, "ids", "", "comma separated ids")
	flags.StringVar(&v.body, "body", "", "body of the entry")
	flags.StringVar(&v.id, "id", "", "id of the entry")
	flags.Var(&v.attrs, "attr", "attribute as key=value, repeatable")
	flags.StringVar(&v.portFile, "port-file", "", "path of the port file")
	flags.IntVar(&v.parentPID, "parent-pid", 0, "pid of the process that launched eco serve")
	flags.StringVar(&v.addr, "addr", "", "loopback address to bind, 127.0.0.1:0 by default")
	if err := flags.Parse(args); err != nil {
		os.Exit(exitUsage)
	}
	if flags.NArg() > 0 {
		refuse(verb, fmt.Sprintf("unexpected argument %q", flags.Arg(0)))
	}
	return v
}

func (v *verbFlags) openCLI(verb string) *store.Store {
	if strings.TrimSpace(v.userDB) == "" || strings.TrimSpace(v.projectDB) == "" {
		refuse(verb, "--user-db and --project-db are required")
	}
	return openStore(v.userDB, v.projectDB, "cli", store.ProfileHuman)
}

func (v *verbFlags) target(verb string) (port.Scope, port.Axis) {
	axis := port.Axis(strings.TrimSpace(v.axis))
	if axis == "" {
		refuse(verb, "--axis is required")
	}
	scope, ok := port.ScopeOfAxis(axis)
	if !ok {
		fail(verb, fmt.Errorf("eco: %w: axis %q has no scope", port.ErrAxisNotInScope, string(axis)))
	}
	return scope, axis
}

func (v *verbFlags) idBatch(verb string) []string {
	raw := strings.TrimSpace(v.ids)
	if raw == "" {
		refuse(verb, "--ids is required")
	}
	out := make([]string, 0, 4)
	for _, id := range strings.Split(raw, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			refuse(verb, "--ids holds an empty id")
		}
		out = append(out, id)
	}
	return out
}

func (v *verbFlags) attrMap(verb string) map[string]string {
	if len(v.attrs) == 0 {
		return nil
	}
	out := make(map[string]string, len(v.attrs))
	for _, pair := range v.attrs {
		key, value, ok := strings.Cut(pair, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			refuse(verb, fmt.Sprintf("--attr %q is not key=value", pair))
		}
		out[key] = value
	}
	return out
}

func (v *verbFlags) boundedLimit(verb string) int {
	if v.limit <= 0 || v.limit > store.MaxLimit {
		refuse(verb, fmt.Sprintf("--limit %d is out of range (1..%d)", v.limit, store.MaxLimit))
	}
	return v.limit
}

type entriesOutput struct {
	Entries   []port.Entry `json:"entries"`
	Truncated bool         `json:"truncated"`
	Omitted   int          `json:"omitted"`
}

func printEntries(entries []port.Entry) {
	if entries == nil {
		entries = []port.Entry{}
	}
	raw, err := json.Marshal(entriesOutput{Entries: entries})
	if err != nil {
		fail("output", err)
	}
	fmt.Println(string(raw))
}

func readVerb(args []string) {
	v := parseFlags("read", args)
	s := v.openCLI("read")
	defer s.Close()
	scope, axis := v.target("read")
	entries, err := s.Read(context.Background(), scope, axis, v.boundedLimit("read"))
	if err != nil {
		fail("read", err)
	}
	printEntries(entries)
}

func searchVerb(args []string) {
	v := parseFlags("search", args)
	s := v.openCLI("search")
	defer s.Close()
	scope, axis := v.target("search")
	if strings.TrimSpace(v.query) == "" {
		refuse("search", "--query is required")
	}
	entries, err := s.Search(context.Background(), scope, axis, v.query, v.boundedLimit("search"))
	if err != nil {
		fail("search", err)
	}
	printEntries(entries)
}

func getVerb(args []string) {
	v := parseFlags("get", args)
	s := v.openCLI("get")
	defer s.Close()
	scope, axis := v.target("get")
	entries, err := s.Get(context.Background(), scope, axis, v.idBatch("get"))
	if err != nil {
		fail("get", err)
	}
	printEntries(entries)
}

func appendVerb(args []string) {
	v := parseFlags("append", args)
	s := v.openCLI("append")
	defer s.Close()
	scope, axis := v.target("append")
	if strings.TrimSpace(v.body) == "" {
		refuse("append", "--body is required")
	}
	id := strings.TrimSpace(v.id)
	if id == "" {
		id = port.DefaultID(scope, axis, v.body)
	}
	stored, err := s.Append(context.Background(), scope, axis, port.Entry{
		ID:    id,
		Body:  v.body,
		Attrs: v.attrMap("append"),
	})
	if err != nil {
		fail("append", err)
	}
	printEntries([]port.Entry{stored})
}

func promoteVerb(args []string) {
	v := parseFlags("promote", args)
	s := v.openCLI("promote")
	defer s.Close()
	entries, err := store.Promote(context.Background(), s, s, v.idBatch("promote"))
	if err != nil {
		fail("promote", err)
	}
	printEntries(entries)
}

func probeVerb(args []string) {
	v := parseFlags("probe", args)
	hasBases := strings.TrimSpace(v.userDB) != "" || strings.TrimSpace(v.projectDB) != ""
	if strings.TrimSpace(v.portFile) != "" && hasBases {
		refuse("probe", "give --port-file or the bases, not both")
	}
	switch {
	case strings.TrimSpace(v.portFile) != "":
		probeOverPortFile(v.portFile)
	case hasBases:
		probeBases(v)
	default:
		refuse("probe", "give --user-db and --project-db, or --port-file")
	}
}

func probeBases(v *verbFlags) {
	s := v.openCLI("probe")
	defer s.Close()
	if err := s.Probe(context.Background()); err != nil {
		fail("probe", err)
	}
	printJSON(map[string]any{"status": "ok", "pid": os.Getpid()})
}

func printJSON(payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		fail("output", err)
	}
	fmt.Println(string(raw))
}