package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JaviCss/eco/port"
	"github.com/JaviCss/eco/store"
)

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "-h", "--help", "help":
		usage(os.Stdout)
		return
	case "doctor":
		doctor(os.Args[2:])
		return
	default:
		fmt.Fprintf(os.Stderr, "eco: unknown verb %q\n", os.Args[1])
		usage(os.Stderr)
		os.Exit(2)
	}
}

func usage(w *os.File) {
	fmt.Fprintln(w, "eco: the memory of ARN")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "usage:")
	fmt.Fprintln(w, "  eco doctor --user-db <path> --project-db <path>")
	fmt.Fprintln(w, "  eco --help")
}

func doctor(args []string) {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	userDB := flags.String("user-db", "", "path of the user base")
	projectDB := flags.String("project-db", "", "path of the project base")
	if err := flags.Parse(args); err != nil {
		os.Exit(2)
	}
	if strings.TrimSpace(*userDB) == "" || strings.TrimSpace(*projectDB) == "" {
		fmt.Fprintln(os.Stderr, "eco doctor: --user-db and --project-db are required")
		os.Exit(2)
	}
	s, err := store.Open(store.Config{
		UserDB:    *userDB,
		ProjectDB: *projectDB,
		Origin:    "doctor",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "eco doctor: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()
	if err := s.Probe(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "eco doctor: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("user db: " + *userDB)
	report(s, port.ScopeUser, *userDB)
	fmt.Println("project db: " + *projectDB)
	report(s, port.ScopeProject, *projectDB)
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
		fmt.Printf("  %s: unreadable: %v\n", scope, err)
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