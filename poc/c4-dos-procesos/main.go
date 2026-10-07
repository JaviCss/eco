package main

import (
	"bytes"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"modernc.org/sqlite"
)

const createTable = "CREATE TABLE IF NOT EXISTS t(id INTEGER PRIMARY KEY, worker INT, n INT, body TEXT);"

var resultRe = regexp.MustCompile(`^CHILD_RESULT ok=(\d+) busy=(\d+) other=(\d+) max_insert_ms=(\d+) total_ms=(\d+) first_busy_raw="(.*)" first_other_raw="(.*)"$`)

func recordMax(target *atomic.Int64, v int64) {
	for {
		cur := target.Load()
		if v <= cur {
			return
		}
		if target.CompareAndSwap(cur, v) {
			return
		}
	}
}

func parseBool(s string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes", "on":
		return true, true
	case "false", "0", "no", "off":
		return false, true
	}
	return false, false
}

func sanitize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " | ")
	s = strings.ReplaceAll(s, "\n", " | ")
	s = strings.ReplaceAll(s, "\r", " | ")
	return strings.TrimSpace(s)
}

func buildDSN(dbPath string, busyMS int, immediate bool) string {
	dsn := "file:" + strings.ReplaceAll(dbPath, "\\", "/")
	var parts []string
	if busyMS > 0 {
		parts = append(parts, fmt.Sprintf("_pragma=busy_timeout(%d)", busyMS))
	}
	parts = append(parts, "_pragma=journal_mode(WAL)")
	if immediate {
		parts = append(parts, "_txlock=immediate")
	}
	if len(parts) == 0 {
		return dsn
	}
	return dsn + "?" + strings.Join(parts, "&")
}

func busyCode(err error) (int, bool) {
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code() & 0xff, true
	}
	return 0, false
}

func classify(err error) (string, string) {
	if code, ok := busyCode(err); ok {
		if code == 5 {
			return "busy", err.Error()
		}
		return "other", err.Error()
	}
	s := err.Error()
	if strings.Contains(s, "SQLITE_BUSY") || strings.Contains(s, "database is locked") {
		return "busy", s
	}
	return "other", s
}

func child(dbPath string, busyMS int, immediate bool, workers, rows int) {
	dsn := buildDSN(dbPath, busyMS, immediate)
	fmt.Println("child_dsn = " + dsn)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		fmt.Println("CHILD_RESULT ok=0 busy=0 other=0 first_busy_raw=\"OPEN: " + sanitize(err.Error()) + "\"")
		return
	}
	defer db.Close()
	db.SetMaxOpenConns(workers)

	var okCount, busyCount, otherCount atomic.Int64
	var maxInsert atomic.Int64
	var mu sync.Mutex
	firstBusy := ""
	firstOther := ""
	payload := strings.Repeat("ab", 64)
	base := int64(os.Getpid()) * 100000000
	started := time.Now()

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			stmt := "INSERT INTO t(id, worker, n, body) VALUES (?, ?, ?, ?);"
			for n := 0; n < rows; n++ {
				id := base + int64(worker)*1000000 + int64(n)
				t0 := time.Now()
				_, err := db.Exec(stmt, id, worker, n, fmt.Sprintf("worker=%d n=%d %s", worker, n, payload))
				elapsed := time.Since(t0).Milliseconds()
				recordMax(&maxInsert, elapsed)
				if err == nil {
					okCount.Add(1)
					continue
				}
				kind, raw := classify(err)
				mu.Lock()
				if kind == "busy" {
					busyCount.Add(1)
					if firstBusy == "" {
						firstBusy = raw
					}
				} else {
					otherCount.Add(1)
					if firstOther == "" {
						firstOther = raw
					}
				}
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()

	fmt.Printf("CHILD_RESULT ok=%d busy=%d other=%d max_insert_ms=%d total_ms=%d first_busy_raw=%q first_other_raw=%q\n",
		okCount.Load(), busyCount.Load(), otherCount.Load(),
		maxInsert.Load(), time.Since(started).Milliseconds(),
		sanitize(firstBusy), sanitize(firstOther))
}

func parent(dbPath string, busyMS int, immediate bool, workers, rows int) {
	_ = os.Remove(dbPath)
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Remove(dbPath + suffix)
	}

	schemaDB, err := sql.Open("sqlite", buildDSN(dbPath, busyMS, immediate))
	if err != nil {
		fmt.Println("PARENT_FATAL " + err.Error())
		return
	}
	if _, err := schemaDB.Exec(createTable); err != nil {
		fmt.Println("PARENT_FATAL " + err.Error())
		schemaDB.Close()
		return
	}
	schemaDB.Close()
	fmt.Println("schema = " + createTable)

	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}

	args := []string{
		"-mode", "child",
		"-db", dbPath,
		fmt.Sprintf("-busy=%d", busyMS),
		fmt.Sprintf("-immediate=%t", immediate),
		fmt.Sprintf("-workers=%d", workers),
		fmt.Sprintf("-rows=%d", rows),
	}
	fmt.Printf("exec = %q %s\n", self, strings.Join(args, " "))

	outputs := make([]string, 2)
	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			var buf bytes.Buffer
			cmd := exec.Command(self, args...)
			cmd.Stdout = &buf
			cmd.Stderr = &buf
			err := cmd.Run()
			outputs[idx] = buf.String()
			codes[idx] = 0
			if err != nil {
				var ee *exec.ExitError
				if errors.As(err, &ee) {
					codes[idx] = ee.ExitCode()
				} else {
					codes[idx] = -1
				}
			}
		}(i)
	}
	wg.Wait()

	var totalOK, totalBusy, totalOther int64
	var maxInsert atomic.Int64
	firstBusy := ""
	firstOther := ""
	for i := 0; i < 2; i++ {
		fmt.Printf("--- child %d (exit=%d) ---\n%s", i, codes[i], outputs[i])
		for _, ln := range strings.Split(strings.TrimRight(outputs[i], "\n"), "\n") {
			m := resultRe.FindStringSubmatch(strings.TrimSpace(ln))
			if m == nil {
				continue
			}
			var ok, busy, other int64
			fmt.Sscanf(m[1], "%d", &ok)
			fmt.Sscanf(m[2], "%d", &busy)
			fmt.Sscanf(m[3], "%d", &other)
			var mx int64
			fmt.Sscanf(m[4], "%d", &mx)
			recordMax(&maxInsert, mx)
			totalOK += ok
			totalBusy += busy
			totalOther += other
			if firstBusy == "" && strings.TrimSpace(m[6]) != "" {
				firstBusy = strings.TrimSpace(m[6])
			}
			if firstOther == "" && strings.TrimSpace(m[7]) != "" {
				firstOther = strings.TrimSpace(m[7])
			}
		}
	}

	db, err := sql.Open("sqlite", buildDSN(dbPath, busyMS, immediate))
	if err != nil {
		fmt.Println("PARENT_FATAL " + err.Error())
		return
	}
	defer db.Close()

	var rowCount int
	if err := db.QueryRow("SELECT count(*) FROM t;").Scan(&rowCount); err != nil {
		fmt.Println("PARENT_FATAL " + err.Error())
		return
	}
	var integrity string
	if err := db.QueryRow("PRAGMA integrity_check;").Scan(&integrity); err != nil {
		fmt.Println("PARENT_FATAL " + err.Error())
		return
	}
	var journal string
	_ = db.QueryRow("PRAGMA journal_mode;").Scan(&journal)

	fmt.Println()
	fmt.Printf("SELECT count(*) FROM t;\nrows = %d\n", rowCount)
	fmt.Printf("PRAGMA integrity_check;\n%s\n", integrity)
	fmt.Printf("PRAGMA journal_mode;\n%s\n", journal)

	expected := int64(2 * workers * rows)
	fmt.Println()
	fmt.Printf("PARENT ok=%d busy=%d other=%d max_insert_ms=%d rows=%d expected=%d integrity_check=%s first_busy_raw=%q first_other_raw=%q\n",
		totalOK, totalBusy, totalOther, maxInsert.Load(), rowCount, expected, integrity,
		sanitize(firstBusy), sanitize(firstOther))
}

func main() {
	mode := flag.String("mode", "parent", "parent|child")
	dbPath := flag.String("db", "", "database file path")
	busyMS := flag.Int("busy", 5000, "busy_timeout milliseconds")
	immediateFlag := flag.Bool("immediate", false, "use _txlock=immediate")
	workers := flag.Int("workers", 4, "worker goroutines per child")
	rows := flag.Int("rows", 2000, "inserts per worker")
	flag.Parse()

	immediate := *immediateFlag
	if flag.NArg() > 0 {
		if b, ok := parseBool(flag.Arg(0)); ok {
			immediate = b
		}
	}

	if *dbPath == "" {
		fmt.Println("PARENT_FATAL -db is required")
		flag.Usage()
		os.Exit(2)
	}

	switch *mode {
	case "parent":
		parent(*dbPath, *busyMS, immediate, *workers, *rows)
	case "child":
		child(*dbPath, *busyMS, immediate, *workers, *rows)
	default:
		fmt.Println("PARENT_FATAL unknown mode " + *mode)
		os.Exit(2)
	}
}
