package store

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JaviCss/eco/port"
)

func TestBusyHelper(t *testing.T) {
	if os.Getenv("ECO_BUSY_HELPER") != "1" {
		t.Log("not the helper process; the two-process driver runs this in a child")
		return
	}
	userDB := os.Getenv("ECO_BUSY_USER_DB")
	projectDB := os.Getenv("ECO_BUSY_PROJECT_DB")
	workers := envInt(t, "ECO_BUSY_WORKERS", 4)
	rows := envInt(t, "ECO_BUSY_ROWS", 2000)
	noRetry := os.Getenv("ECO_BUSY_NO_RETRY") == "1"
	busyTimeout := DefaultBusyTimeout
	if os.Getenv("ECO_BUSY_TIMEOUT") != "" {
		busyTimeout = envInt(t, "ECO_BUSY_TIMEOUT", DefaultBusyTimeout)
	}
	immediate := os.Getenv("ECO_BUSY_IMMEDIATE") != "0"

	retries := DefaultRetries
	if noRetry {
		retries = 1
	}
	openConfig := Config{
		UserDB:      userDB,
		ProjectDB:   projectDB,
		Origin:      "runtime",
		BusyTimeout: busyTimeout,
		MaxRetries:  retries,
		DeferredTx:  !immediate,
	}
	s, err := Open(openConfig)
	if err != nil {
		t.Fatalf("helper Open: %v", err)
	}
	if noRetry {
		control := openConfig
		control.NoBusyTimeout = true
		control.DeferredTx = true
		s.cfg = control
		if err := s.reopenUser(); err != nil {
			t.Fatalf("helper control reopen: %v", err)
		}
	}
	defer s.Close()
	t.Logf("HELPER_CONFIG busy_timeout=%d immediate=%t max_retries=%d no_busy_timeout=%t dsn=%s", busyTimeout, immediate, retries, noRetry, s.dsn(userDB))

	var okCount, busyCount, otherCount int64
	var mu sync.Mutex
	var firstBusy string
	var maxElapsed int64
	payload := fmt.Sprintf("worker payload %s", strings.Repeat("x", 64))
	base := int64(os.Getpid()) * 1000000
	started := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for n := 0; n < rows; n++ {
				id := fmt.Sprintf("p%d-w%d-%05d", os.Getpid(), worker, n)
				t0 := time.Now()
				_, err := s.Append(context.Background(), port.ScopeUser, port.AxisZ2, port.Entry{
					ID: id, Body: payload,
				})
				elapsed := time.Since(t0).Milliseconds()
				mu.Lock()
				if elapsed > maxElapsed {
					maxElapsed = elapsed
				}
				mu.Unlock()
				if err == nil {
					okCount++
					continue
				}
				if isBusy(err) || strings.Contains(err.Error(), "SQLITE_BUSY") || strings.Contains(err.Error(), "database is locked") {
					busyCount++
					mu.Lock()
					if firstBusy == "" {
						firstBusy = err.Error()
					}
					mu.Unlock()
					continue
				}
				otherCount++
				t.Logf("helper unexpected error: %v", err)
			}
		}(w)
	}
	wg.Wait()
	t.Logf("HELPER pid=%d ok=%d busy_leaked=%d other=%d retries=%d max_append_ms=%d total_ms=%d id_base=%d first_busy=%q",
		os.Getpid(), okCount, busyCount, otherCount, s.Retries(), maxElapsed,
		time.Since(started).Milliseconds(), base, firstBusy)
	if !noRetry && busyCount > 0 {
		t.Fatalf("the Store leaked %d SQLITE_BUSY to the caller; the retry must absorb them", busyCount)
	}
	if otherCount > 0 {
		t.Fatalf("the helper saw %d errors that are neither ok nor BUSY", otherCount)
	}
}

func pickRetries(noRetry bool) int {
	if noRetry {
		return 1
	}
	return DefaultRetries
}

func envInt(t *testing.T, key string, fallback int) int {
	t.Helper()
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("%s=%q: %v", key, raw, err)
	}
	return value
}