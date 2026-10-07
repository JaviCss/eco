package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/JaviCss/eco/port"

	"modernc.org/sqlite"
)

const MaxBodyBytes = 64 * 1024
const MaxAttrs = 32
const MaxLimit = 200

const DefaultBusyTimeout = 5000
const DefaultRetries = 12

const (
	attrOrigin       = "origin"
	attrPromotedFrom = "promoted_from"
	attrSourceOrigin = "source_origin"
)

const (
	sqlSelect = `SELECT id, axis, scope, at, body, attrs FROM entries WHERE scope = ? AND axis = ? ORDER BY at DESC, id ASC LIMIT ?`
	sqlInsert = `INSERT INTO entries (id, axis, scope, at, body, attrs) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO NOTHING`
	sqlGet    = `SELECT id, axis, scope, at, body, attrs FROM entries WHERE scope = ? AND axis = ? AND id = ?`
	sqlGetAny = `SELECT axis, scope FROM entries WHERE id = ? LIMIT 1`
	sqlSearch = `SELECT e.id, e.axis, e.scope, e.at, e.body, e.attrs
FROM entries_fts JOIN entries e ON e.rowid = entries_fts.rowid
WHERE entries_fts MATCH ? AND e.scope = ? AND e.axis = ?
ORDER BY bm25(entries_fts), e.at DESC
LIMIT ?`
)

type Config struct {
	UserDB        string
	ProjectDB     string
	Origin        string
	BusyTimeout   int
	MaxRetries    int
	RetryBase     time.Duration
	DeferredTx    bool
	NoBusyTimeout bool
	ReadOnly      bool
	Profile       Profile
}

type Store struct {
	mu      sync.RWMutex
	cfg     Config
	dbs     map[port.Scope]*sql.DB
	paths   map[port.Scope]string
	retries atomic.Int64
}

func Open(cfg Config) (*Store, error) {
	if cfg.BusyTimeout == 0 {
		cfg.BusyTimeout = DefaultBusyTimeout
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = DefaultRetries
	}
	if cfg.RetryBase <= 0 {
		cfg.RetryBase = 2 * time.Millisecond
	}
	if strings.TrimSpace(cfg.Origin) == "" {
		return nil, fmt.Errorf("eco: Open: %w: origin is required", port.ErrInvalidEntry)
	}
	user, err := checkPath(cfg.UserDB)
	if err != nil {
		return nil, err
	}
	project, err := checkPath(cfg.ProjectDB)
	if err != nil {
		return nil, err
	}
	if sameFile(user, project) {
		return nil, fmt.Errorf("eco: Open: %w: user and project paths are the same file: %s", port.ErrUnavailable, user)
	}
	targets := []struct {
		scope port.Scope
		path  string
	}{{port.ScopeUser, user}, {port.ScopeProject, project}}
	for _, target := range targets {
		if info, err := os.Stat(target.path); err == nil && info.IsDir() {
			return nil, fmt.Errorf("eco: Open: %w: %s is a directory", port.ErrUnavailable, target.path)
		}
	}
	s := &Store{
		cfg:   cfg,
		dbs:   map[port.Scope]*sql.DB{},
		paths: map[port.Scope]string{port.ScopeUser: user, port.ScopeProject: project},
	}
	for _, target := range targets {
		if err := s.inspectBase(context.Background(), target.path); err != nil {
			return nil, err
		}
	}
	if cfg.ReadOnly {
		for _, target := range targets {
			if _, err := os.Stat(target.path); err != nil {
				return nil, fmt.Errorf("eco: Open: %w: %s: %v; a read-only open never creates a base", port.ErrUnavailable, target.path, err)
			}
		}
	}
	for _, target := range targets {
		if err := s.openBase(target.scope, target.path); err != nil {
			s.Close()
			return nil, err
		}
	}
	return s, nil
}

func sameFile(a, b string) bool {
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	if errA == nil && errB == nil {
		return os.SameFile(infoA, infoB)
	}
	return strings.EqualFold(resolvedDir(a), resolvedDir(b)) &&
		strings.EqualFold(filepath.Base(a), filepath.Base(b))
}

func resolvedDir(path string) string {
	dir := filepath.Dir(path)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Clean(real)
	}
	return filepath.Clean(dir)
}

func checkPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("eco: Open: %w: empty database path", port.ErrInvalidEntry)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("eco: Open: %w: %s: %v", port.ErrInvalidEntry, path, err)
	}
	abs = filepath.Clean(abs)
	volume := filepath.VolumeName(abs)
	rest := strings.TrimPrefix(abs, volume)
	current := volume + string(os.PathSeparator)
	for _, part := range strings.Split(rest, string(os.PathSeparator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return abs, nil
			}
			return "", fmt.Errorf("eco: Open: %w: %s: %v", port.ErrUnavailable, current, err)
		}
		if isReparse(info) {
			return "", fmt.Errorf("eco: Open: %w: %s is a reparse point", port.ErrUnavailable, current)
		}
	}
	return abs, nil
}

func dsn(path string, busy int) string {
	return dsnWith(path, busy, true)
}

func (s *Store) dsn(path string) string {
	if s.cfg.ReadOnly {
		return readOnlyDSN(path, s.cfg.BusyTimeout)
	}
	if s.cfg.NoBusyTimeout {
		return dsnWith(path, -1, !s.cfg.DeferredTx)
	}
	return dsnWith(path, s.cfg.BusyTimeout, !s.cfg.DeferredTx)
}

func readOnlyDSN(path string, busy int) string {
	forward := filepath.ToSlash(path)
	forward = strings.ReplaceAll(forward, "?", "%3F")
	forward = strings.ReplaceAll(forward, "#", "%23")
	return "file:" + forward +
		"?_pragma=busy_timeout(" + strconv.Itoa(busy) + ")" +
		"&_pragma=query_only(1)" +
		"&_pragma=trusted_schema(0)"
}

func dsnWith(path string, busy int, immediate bool) string {
	forward := filepath.ToSlash(path)
	forward = strings.ReplaceAll(forward, "?", "%3F")
	forward = strings.ReplaceAll(forward, "#", "%23")
	params := []string{}
	if busy >= 0 {
		params = append(params, "_pragma=busy_timeout("+strconv.Itoa(busy)+")")
	}
	params = append(params,
		"_pragma=journal_mode(WAL)",
		"_pragma=synchronous(NORMAL)",
		"_pragma=trusted_schema(0)",
	)
	if immediate {
		params = append(params, "_txlock=immediate")
	}
	return "file:" + forward + "?" + strings.Join(params, "&")
}

func (s *Store) openBase(scope port.Scope, path string) error {
	var lastErr error
	for attempt := 0; attempt <= s.cfg.MaxRetries; attempt++ {
		lastErr = s.openOnce(scope, path)
		if lastErr == nil {
			return nil
		}
		if !isBusy(lastErr) {
			return lastErr
		}
		s.retries.Add(1)
		if attempt == s.cfg.MaxRetries {
			break
		}
		time.Sleep(s.cfg.RetryBase * time.Duration(1<<attempt))
	}
	return lastErr
}

func (s *Store) openOnce(scope port.Scope, path string) error {
	db, err := sql.Open("sqlite", s.dsn(path))
	if err != nil {
		return fmt.Errorf("eco: Open: %s: %w: %s: %v", scope, port.ErrUnavailable, path, err)
	}
	if err := s.prepareBase(db, path); err != nil {
		db.Close()
		return err
	}
	s.mu.Lock()
	s.dbs[scope] = db
	s.mu.Unlock()
	return nil
}

func inspectDSN(path string, busy int) string {
	forward := filepath.ToSlash(path)
	forward = strings.ReplaceAll(forward, "?", "%3F")
	forward = strings.ReplaceAll(forward, "#", "%23")
	return "file:" + forward +
		"?_pragma=busy_timeout(" + strconv.Itoa(busy) + ")" +
		"&_pragma=trusted_schema(0)"
}

func (s *Store) inspectBase(ctx context.Context, path string) error {
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	var lastErr error
	for attempt := 0; attempt <= s.cfg.MaxRetries; attempt++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		lastErr = s.inspectOnce(path)
		if lastErr == nil {
			return nil
		}
		if !isBusy(lastErr) {
			return lastErr
		}
		s.retries.Add(1)
		if attempt == s.cfg.MaxRetries {
			break
		}
		time.Sleep(s.cfg.RetryBase * time.Duration(1<<attempt))
	}
	return lastErr
}

func (s *Store) inspectOnce(path string) error {
	db, err := sql.Open("sqlite", inspectDSN(path, s.cfg.BusyTimeout))
	if err != nil {
		return fmt.Errorf("eco: Open: %w: %s: %v", port.ErrUnavailable, path, err)
	}
	defer db.Close()
	var appID int
	var userVersion int
	if err := db.QueryRow(`PRAGMA application_id`).Scan(&appID); err != nil {
		return fmt.Errorf("eco: Open: %w: %s: cannot read application_id: %v", port.ErrUnavailable, path, err)
	}
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&userVersion); err != nil {
		return fmt.Errorf("eco: Open: %w: %s: cannot read user_version: %v", port.ErrUnavailable, path, err)
	}
	if appID != ApplicationID {
		return fmt.Errorf("eco: Open: %w: %s: application_id %d is not eco's %d", port.ErrUnavailable, path, appID, ApplicationID)
	}
	if userVersion > SchemaVersion {
		return fmt.Errorf("eco: Open: %w: %s: user_version %d is newer than this binary's %d", port.ErrUnavailable, path, userVersion, SchemaVersion)
	}
	return nil
}

func (s *Store) prepareBase(db *sql.DB, path string) error {
	var appID int
	var userVersion int
	if err := db.QueryRow(`PRAGMA application_id`).Scan(&appID); err != nil {
		return fmt.Errorf("eco: Open: %w: %s: cannot read application_id: %v", port.ErrUnavailable, path, err)
	}
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&userVersion); err != nil {
		return fmt.Errorf("eco: Open: %w: %s: cannot read user_version: %v", port.ErrUnavailable, path, err)
	}
	if s.cfg.ReadOnly {
		if appID != ApplicationID {
			return fmt.Errorf("eco: Open: %w: %s: application_id %d is not eco's %d", port.ErrUnavailable, path, appID, ApplicationID)
		}
		if userVersion > SchemaVersion {
			return fmt.Errorf("eco: Open: %w: %s: user_version %d is newer than this binary's %d", port.ErrUnavailable, path, userVersion, SchemaVersion)
		}
		return nil
	}
	if appID == 0 && userVersion == 0 {
		if _, err := db.Exec(fmt.Sprintf(`PRAGMA application_id = %d`, ApplicationID)); err != nil {
			return fmt.Errorf("eco: Open: %w: %s: cannot stamp application_id: %v", port.ErrUnavailable, path, err)
		}
		if err := migrate(context.Background(), db, 0); err != nil {
			return fmt.Errorf("eco: Open: %w: %s: %v", port.ErrUnavailable, path, err)
		}
		return nil
	}
	if appID != ApplicationID {
		return fmt.Errorf("eco: Open: %w: %s: application_id %d is not eco's %d", port.ErrUnavailable, path, appID, ApplicationID)
	}
	if userVersion > SchemaVersion {
		return fmt.Errorf("eco: Open: %w: %s: user_version %d is newer than this binary's %d", port.ErrUnavailable, path, userVersion, SchemaVersion)
	}
	if err := migrate(context.Background(), db, userVersion); err != nil {
		return fmt.Errorf("eco: Open: %w: %s: %v", port.ErrUnavailable, path, err)
	}
	return nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var first error
	for scope, db := range s.dbs {
		if err := db.Close(); err != nil && first == nil {
			first = err
		}
		delete(s.dbs, scope)
	}
	return first
}

func (s *Store) Retries() int64 {
	return s.retries.Load()
}

func (s *Store) db(scope port.Scope) (*sql.DB, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	db, ok := s.dbs[scope]
	if !ok || db == nil {
		return nil, fmt.Errorf("eco: %s: %w: database is closed", scope, port.ErrUnavailable)
	}
	return db, nil
}

func (s *Store) shutdown() error {
	return s.Close()
}

func (s *Store) restart() error {
	for _, scope := range []port.Scope{port.ScopeUser, port.ScopeProject} {
		if err := s.openBase(scope, s.paths[scope]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) reopenUser() error {
	return s.openBase(port.ScopeUser, s.paths[port.ScopeUser])
}

func isBusy(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		switch se.Code() & 0xff {
		case 5, 261, 517:
			return true
		}
	}
	return false
}

func (s *Store) withRetry(ctx context.Context, op string, fn func() error) error {
	backoff := s.cfg.RetryBase
	var err error
	for attempt := 0; attempt <= s.cfg.MaxRetries; attempt++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		err = fn()
		if err == nil {
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if !isBusy(err) {
			return err
		}
		s.retries.Add(1)
		if attempt == s.cfg.MaxRetries {
			break
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if backoff < 250*time.Millisecond {
			backoff *= 2
		}
	}
	return err
}

func checkTarget(scope port.Scope, axis port.Axis) error {
	if !scope.Valid() {
		return fmt.Errorf("eco: %w: %q", port.ErrInvalidScope, string(scope))
	}
	owner, ok := port.ScopeOfAxis(axis)
	if !ok {
		return fmt.Errorf("eco: %w: axis %q has no scope", port.ErrAxisNotInScope, string(axis))
	}
	if owner != scope {
		return fmt.Errorf("eco: %w: axis %q lives in scope %q", port.ErrAxisNotInScope, string(axis), string(owner))
	}
	return nil
}

func checkLimit(limit int) error {
	if limit <= 0 || limit > MaxLimit {
		return fmt.Errorf("eco: %w: limit %d out of range (1..%d)", port.ErrInvalidEntry, limit, MaxLimit)
	}
	return nil
}

func checkEntry(entry port.Entry, reserved bool) (port.Entry, error) {
	if strings.TrimSpace(entry.ID) == "" {
		return port.Entry{}, fmt.Errorf("eco: Append: %w: empty id", port.ErrInvalidEntry)
	}
	if len(entry.Body) > MaxBodyBytes {
		return port.Entry{}, fmt.Errorf("eco: Append: %w: body of %d bytes exceeds %d", port.ErrInvalidEntry, len(entry.Body), MaxBodyBytes)
	}
	if len(entry.Attrs) > MaxAttrs {
		return port.Entry{}, fmt.Errorf("eco: Append: %w: %d attrs exceed %d", port.ErrInvalidEntry, len(entry.Attrs), MaxAttrs)
	}
	attrs := make(map[string]string, len(entry.Attrs)+1)
	for key, value := range entry.Attrs {
		if !reserved && (key == attrOrigin || key == attrPromotedFrom || key == attrSourceOrigin) {
			return port.Entry{}, fmt.Errorf("eco: Append: %w: attr %q is reserved", port.ErrInvalidEntry, key)
		}
		attrs[key] = value
	}
	entry.Attrs = attrs
	return entry, nil
}

func encodeAttrs(attrs map[string]string) (string, error) {
	if len(attrs) == 0 {
		return "{}", nil
	}
	raw, err := json.Marshal(attrs)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func decodeAttrs(raw string) map[string]string {
	out := map[string]string{}
	if raw == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return map[string]string{}
	}
	return out
}

func scanEntry(row interface {
	Scan(dest ...any) error
}) (port.Entry, error) {
	var entry port.Entry
	var axis string
	var scope string
	var at int64
	var attrs string
	if err := row.Scan(&entry.ID, &axis, &scope, &at, &entry.Body, &attrs); err != nil {
		return port.Entry{}, err
	}
	entry.Axis = port.Axis(axis)
	entry.Scope = port.Scope(scope)
	entry.At = time.Unix(0, at).UTC()
	entry.Attrs = decodeAttrs(attrs)
	return entry, nil
}

type BaseInfo struct {
	SQLiteVersion string
	UserVersion   int
	ApplicationID int
	JournalMode   string
	SizeBytes     int64
}

func (s *Store) Inspect(scope port.Scope) (BaseInfo, error) {
	db, err := s.db(scope)
	if err != nil {
		return BaseInfo{}, err
	}
	var info BaseInfo
	if err := db.QueryRow(`SELECT sqlite_version()`).Scan(&info.SQLiteVersion); err != nil {
		return BaseInfo{}, fmt.Errorf("eco: Inspect: %s: %w: %v", scope, port.ErrUnavailable, err)
	}
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&info.UserVersion); err != nil {
		return BaseInfo{}, fmt.Errorf("eco: Inspect: %s: %w: %v", scope, port.ErrUnavailable, err)
	}
	if err := db.QueryRow(`PRAGMA application_id`).Scan(&info.ApplicationID); err != nil {
		return BaseInfo{}, fmt.Errorf("eco: Inspect: %s: %w: %v", scope, port.ErrUnavailable, err)
	}
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&info.JournalMode); err != nil {
		return BaseInfo{}, fmt.Errorf("eco: Inspect: %s: %w: %v", scope, port.ErrUnavailable, err)
	}
	if s.paths != nil {
		if stat, err := os.Stat(s.paths[scope]); err == nil {
			info.SizeBytes = stat.Size()
		}
	}
	return info, nil
}

func (s *Store) Get(ctx context.Context, scope port.Scope, axis port.Axis, ids []string) ([]port.Entry, error) {
	if err := checkTarget(scope, axis); err != nil {
		return nil, err
	}
	if err := checkBatch(ids); err != nil {
		return nil, err
	}
	if err := s.checkRead(axis); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	db, err := s.db(scope)
	if err != nil {
		return nil, err
	}
	out := make([]port.Entry, 0, len(ids))
	var missing []string
	for _, id := range ids {
		entry, scanErr := scanEntry(db.QueryRowContext(ctx, sqlGet, string(scope), string(axis), id))
		switch {
		case scanErr == nil:
			out = append(out, entry)
		case isNotFound(scanErr):
			missing = append(missing, id)
		default:
			return nil, fmt.Errorf("eco: Get: %w: %v", port.ErrUnavailable, scanErr)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("eco: Get: %w: not in %s/%s: %v", port.ErrNotFound, scope, axis, missing)
	}
	return out, nil
}

func checkBatch(ids []string) error {
	if len(ids) == 0 {
		return fmt.Errorf("eco: Get: %w: empty batch", port.ErrInvalidEntry)
	}
	if len(ids) > port.MaxBatch {
		return fmt.Errorf("eco: Get: %w: batch of %d ids exceeds %d", port.ErrInvalidEntry, len(ids), port.MaxBatch)
	}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("eco: Get: %w: empty id in the batch", port.ErrInvalidEntry)
		}
	}
	return nil
}

func (s *Store) Read(ctx context.Context, scope port.Scope, axis port.Axis, limit int) ([]port.Entry, error) {
	if err := checkTarget(scope, axis); err != nil {
		return nil, err
	}
	if err := checkLimit(limit); err != nil {
		return nil, err
	}
	if err := s.checkRead(axis); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	db, err := s.db(scope)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, sqlSelect, string(scope), string(axis), limit)
	if err != nil {
		return nil, fmt.Errorf("eco: Read: %w: %v", port.ErrUnavailable, err)
	}
	defer rows.Close()
	out := []port.Entry{}
	for rows.Next() {
		entry, scanErr := scanEntry(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("eco: Read: %w: %v", port.ErrUnavailable, scanErr)
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("eco: Read: %w: %v", port.ErrUnavailable, err)
	}
	return out, nil
}

func (s *Store) Append(ctx context.Context, scope port.Scope, axis port.Axis, entry port.Entry) (port.Entry, error) {
	return s.append(ctx, scope, axis, entry)
}

func (s *Store) append(ctx context.Context, scope port.Scope, axis port.Axis, entry port.Entry) (port.Entry, error) {
	if err := checkTarget(scope, axis); err != nil {
		return port.Entry{}, err
	}
	if err := s.checkWrite(axis); err != nil {
		return port.Entry{}, err
	}
	entry, err := checkEntry(entry, false)
	if err != nil {
		return port.Entry{}, err
	}
	if err := ctx.Err(); err != nil {
		return port.Entry{}, err
	}
	db, err := s.db(scope)
	if err != nil {
		return port.Entry{}, err
	}
	entry.Axis = axis
	entry.Scope = scope
	if entry.At.IsZero() {
		entry.At = time.Now()
	}
	entry.At = entry.At.UTC()
	entry.Attrs[attrOrigin] = s.cfg.Origin
	attrs, err := encodeAttrs(entry.Attrs)
	if err != nil {
		return port.Entry{}, fmt.Errorf("eco: Append: %w: %v", port.ErrInvalidEntry, err)
	}
	args := []any{entry.ID, string(axis), string(scope), entry.At.UnixNano(), entry.Body, attrs}
	if err := s.withRetry(ctx, "Append", func() error {
		return s.writeOne(ctx, db, args)
	}); err != nil {
		return port.Entry{}, fmt.Errorf("eco: Append: %w: %v", port.ErrUnavailable, err)
	}
	var stored port.Entry
	if err := s.withRetry(ctx, "Append", func() error {
		got, scanErr := scanEntry(db.QueryRowContext(ctx, sqlGet, string(scope), string(axis), entry.ID))
		stored = got
		return scanErr
	}); err != nil {
		if isNotFound(err) {
			if holderAxis, holderScope, held := s.axisHolding(ctx, db, entry.ID); held {
				return port.Entry{}, fmt.Errorf("eco: Append: %w: id %q is already used by %s/%s", port.ErrInvalidEntry, entry.ID, holderScope, holderAxis)
			}
		}
		return port.Entry{}, fmt.Errorf("eco: Append: %w: %v", port.ErrUnavailable, err)
	}
	return stored, nil
}

func (s *Store) writeOne(ctx context.Context, db *sql.DB, args []any) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, sqlInsert, args...); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) axisHolding(ctx context.Context, db *sql.DB, id string) (string, string, bool) {
	var axis string
	var scope string
	if err := db.QueryRowContext(ctx, sqlGetAny, id).Scan(&axis, &scope); err != nil {
		return "", "", false
	}
	return axis, scope, true
}

func phrase(query string) string {
	return `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
}

func (s *Store) Search(ctx context.Context, scope port.Scope, axis port.Axis, query string, limit int) ([]port.Entry, error) {
	if err := checkTarget(scope, axis); err != nil {
		return nil, err
	}
	if err := checkLimit(limit); err != nil {
		return nil, err
	}
	if err := s.checkRead(axis); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	db, err := s.db(scope)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(query)
	if len([]rune(trimmed)) < 3 {
		return nil, fmt.Errorf("eco: Search: %w: query of %d characters has nothing to search", port.ErrNotFound, len([]rune(trimmed)))
	}
	rows, err := db.QueryContext(ctx, sqlSearch, phrase(trimmed), string(scope), string(axis), limit)
	if err != nil {
		return nil, fmt.Errorf("eco: Search: %w: %v", port.ErrUnavailable, err)
	}
	defer rows.Close()
	out := []port.Entry{}
	for rows.Next() {
		entry, scanErr := scanEntry(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("eco: Search: %w: %v", port.ErrUnavailable, scanErr)
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("eco: Search: %w: %v", port.ErrUnavailable, err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("eco: Search: %w: %q in %s/%s", port.ErrNotFound, query, scope, axis)
	}
	return out, nil
}

func (s *Store) Probe(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, scope := range []port.Scope{port.ScopeUser, port.ScopeProject} {
		db, err := s.db(scope)
		if err != nil {
			return err
		}
		var one int
		if err := db.QueryRowContext(ctx, `SELECT 1`).Scan(&one); err != nil {
			return fmt.Errorf("eco: Probe: %s: %w: %v", scope, port.ErrUnavailable, err)
		}
	}
	return nil
}
