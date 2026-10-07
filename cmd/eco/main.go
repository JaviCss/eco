package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JaviCss/eco/port"
	"github.com/JaviCss/eco/store"
)

const (
	exitUsage       = 2
	exitUnavailable = 3
	exitNotFound    = 4
	exitForbidden   = 5
	exitInvalid     = 6
	exitFailed      = 1
)

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(exitUsage)
	}
	verb := os.Args[1]
	args := os.Args[2:]
	switch verb {
	case "-h", "--help", "help":
		usage(os.Stdout)
		return
	case "doctor":
		doctor(args)
		return
	case "read":
		readVerb(args)
		return
	case "search":
		searchVerb(args)
		return
	case "get":
		getVerb(args)
		return
	case "append":
		appendVerb(args)
		return
	case "promote":
		promoteVerb(args)
		return
	case "probe":
		probeVerb(args)
		return
	case "mcp":
		mcpVerb(args)
		return
	case "serve":
		serveVerb(args)
		return
	case "-v", "--version", "version":
		versionVerb(args)
		return
	default:
		fmt.Fprintf(os.Stderr, "eco: unknown verb %q\n", verb)
		usage(os.Stderr)
		os.Exit(exitUsage)
	}
}

func versionVerb(args []string) {
	if len(args) > 0 {
		refuse("version", fmt.Sprintf("unexpected argument %q", args[0]))
	}
	fmt.Println(versionLine())
}

func usage(w *os.File) {
	fmt.Fprintln(w, "eco: the memory of ARN")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "verbs:")
	fmt.Fprintln(w, "  eco read --user-db <p> --project-db <p> --axis <axis> [--limit n]")
	fmt.Fprintln(w, "  eco search --user-db <p> --project-db <p> --axis <axis> --query <text> [--limit n]")
	fmt.Fprintln(w, "  eco get --user-db <p> --project-db <p> --axis <axis> --ids <id,id>")
	fmt.Fprintln(w, "  eco append --user-db <p> --project-db <p> --axis <axis> --body <text> [--id <id>] [--attr k=v]...")
	fmt.Fprintln(w, "  eco promote --user-db <p> --project-db <p> --ids <id,id>")
	fmt.Fprintln(w, "  eco probe (--user-db <p> --project-db <p> | --port-file <p>)")
	fmt.Fprintln(w, "  eco doctor --user-db <p> --project-db <p>")
	fmt.Fprintln(w, "  eco mcp --user-db <p> --project-db <p>")
	fmt.Fprintln(w, "  eco serve --user-db <p> --project-db <p> --parent-pid <pid> --port-file <p> [--addr <host:port>]")
	fmt.Fprintln(w, "  eco version")
	fmt.Fprintln(w, "  eco help")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "exit codes: 2 usage, 3 unavailable, 4 not found, 5 forbidden, 6 invalid")
}

func sentinelName(err error) string {
	switch {
	case errors.Is(err, port.ErrNotFound):
		return "eco: not found"
	case errors.Is(err, port.ErrForbidden):
		return "eco: forbidden"
	case errors.Is(err, port.ErrUnavailable):
		return "eco: unavailable"
	case errors.Is(err, port.ErrInvalidScope):
		return "eco: invalid scope"
	case errors.Is(err, port.ErrAxisNotInScope):
		return "eco: axis not in scope"
	case errors.Is(err, port.ErrInvalidEntry):
		return "eco: invalid entry"
	default:
		return "eco: failed"
	}
}

func exitFor(err error) int {
	switch {
	case errors.Is(err, port.ErrForbidden):
		return exitForbidden
	case errors.Is(err, port.ErrNotFound):
		return exitNotFound
	case errors.Is(err, port.ErrUnavailable):
		return exitUnavailable
	case errors.Is(err, port.ErrInvalidScope), errors.Is(err, port.ErrAxisNotInScope), errors.Is(err, port.ErrInvalidEntry):
		return exitInvalid
	default:
		return exitFailed
	}
}

func fail(verb string, err error) {
	fmt.Fprintf(os.Stderr, "eco %s: %s\n", verb, sentinelName(err))
	os.Exit(exitFor(err))
}

func refuse(verb, message string) {
	fmt.Fprintf(os.Stderr, "eco %s: %s\n", verb, message)
	os.Exit(exitUsage)
}

func openStore(userDB, projectDB, origin string, profile store.Profile) *store.Store {
	s, err := store.Open(store.Config{
		UserDB:    userDB,
		ProjectDB: projectDB,
		Origin:    origin,
		Profile:   profile,
	})
	if err != nil {
		fail("open", err)
	}
	return s
}

func doctor(args []string) {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	userDB := flags.String("user-db", "", "path of the user base")
	projectDB := flags.String("project-db", "", "path of the project base")
	if err := flags.Parse(args); err != nil {
		os.Exit(exitUsage)
	}
	if strings.TrimSpace(*userDB) == "" || strings.TrimSpace(*projectDB) == "" {
		fmt.Fprintln(os.Stderr, "eco doctor: --user-db and --project-db are required")
		os.Exit(exitUsage)
	}
	readOnly, err := store.Open(store.Config{
		UserDB:    *userDB,
		ProjectDB: *projectDB,
		Origin:    "doctor",
		ReadOnly:  true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "eco doctor: %s\n", sentinelName(err))
		os.Exit(exitFailed)
	}
	defer readOnly.Close()
	if err := readOnly.Probe(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "eco doctor: %s\n", sentinelName(err))
		os.Exit(exitFailed)
	}
	version, revision, modified := buildStamp()
	fmt.Printf("version: eco %s\n", version)
	fmt.Printf("revision: %s\n", revisionLine(revision))
	if modified {
		fmt.Println("modified: true")
	}
	fmt.Println("user db: " + *userDB)
	report(readOnly, port.ScopeUser, *userDB)
	fmt.Println("project db: " + *projectDB)
	report(readOnly, port.ScopeProject, *projectDB)
	if warn, path := syncFolder(*userDB); warn {
		fmt.Printf("WARN user db is in a synced or network folder, where WAL does not work: %s\n", path)
	}
	if warn, path := syncFolder(*projectDB); warn {
		fmt.Printf("WARN project db is in a synced or network folder, where WAL does not work: %s\n", path)
	}
}

func report(s *store.Store, scope port.Scope, path string) {
	info, err := s.Inspect(scope)
	if err != nil {
		fmt.Printf("  %s: unreadable: %s\n", scope, sentinelName(err))
		return
	}
	fmt.Printf("  sqlite_version: %s\n", info.SQLiteVersion)
	fmt.Printf("  user_version: %d\n", info.UserVersion)
	fmt.Printf("  application_id: %d\n", info.ApplicationID)
	fmt.Printf("  journal_mode: %s\n", info.JournalMode)
	fmt.Printf("  size_bytes: %d\n", info.SizeBytes)
}

func syncFolder(path string) (bool, string) {
	lower := strings.ToLower(filepath.ToSlash(path))
	if strings.HasPrefix(lower, "//") || strings.HasPrefix(path, `\\`) {
		return true, "network path"
	}
	for _, part := range strings.Split(lower, "/") {
		if strings.Contains(part, "onedrive") || strings.Contains(part, "dropbox") || strings.Contains(part, "google drive") {
			return true, part
		}
	}
	return false, ""
}