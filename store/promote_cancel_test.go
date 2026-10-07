package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/JaviCss/eco/port"

	"modernc.org/sqlite"
)

type batchProbe struct {
	mu              sync.Mutex
	begins          int
	inserts         int
	cancelled       bool
	beginsAtCancel  int
	insertsAtCancel int
	cancel          context.CancelFunc
	cancelAt        int
}

func (p *batchProbe) noteBegin() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.begins++
}

func (p *batchProbe) noteExec(query string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(query)), "INSERT") {
		return
	}
	p.inserts++
	if p.cancelled || p.inserts < p.cancelAt {
		return
	}
	p.cancelled = true
	p.beginsAtCancel = p.begins
	p.insertsAtCancel = p.inserts
	p.mu.Unlock()
	p.cancel()
	p.mu.Lock()
}

func (p *batchProbe) snapshot() (begins, inserts int, cancelled bool, beginsAtCancel, insertsAtCancel int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.begins, p.inserts, p.cancelled, p.beginsAtCancel, p.insertsAtCancel
}

type probeConnector struct {
	inner driver.Connector
	probe *batchProbe
}

func (c *probeConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &probeConn{inner: conn, probe: c.probe}, nil
}

func (c *probeConnector) Driver() driver.Driver { return c.inner.Driver() }

type probeConn struct {
	inner driver.Conn
	probe *batchProbe
}

func (c *probeConn) Prepare(query string) (driver.Stmt, error) { return c.inner.Prepare(query) }

func (c *probeConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if preparer, ok := c.inner.(driver.ConnPrepareContext); ok {
		return preparer.PrepareContext(ctx, query)
	}
	return c.inner.Prepare(query)
}

func (c *probeConn) Close() error { return c.inner.Close() }

func (c *probeConn) Begin() (driver.Tx, error) {
	c.probe.noteBegin()
	return c.inner.Begin()
}

func (c *probeConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.probe.noteBegin()
	if beginner, ok := c.inner.(driver.ConnBeginTx); ok {
		return beginner.BeginTx(ctx, opts)
	}
	return c.inner.Begin()
}

func (c *probeConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	result, err := c.execContext(ctx, query, args)
	c.probe.noteExec(query)
	return result, err
}

func (c *probeConn) execContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if execer, ok := c.inner.(driver.ExecerContext); ok {
		return execer.ExecContext(ctx, query, args)
	}
	stmt, err := c.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	values := make([]driver.Value, 0, len(args))
	for _, arg := range args {
		values = append(values, arg.Value)
	}
	return stmt.Exec(values)
}

func (c *probeConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if queryer, ok := c.inner.(driver.QueryerContext); ok {
		return queryer.QueryContext(ctx, query, args)
	}
	stmt, err := c.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	values := make([]driver.Value, 0, len(args))
	for _, arg := range args {
		values = append(values, arg.Value)
	}
	return stmt.Query(values)
}

const cancelInsideBatchAt = 3

func TestPromoteCancelledInsideTheBatchRollsBackEveryRow(t *testing.T) {
	source := port.NewFake()
	ids := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		id := fmt.Sprintf("z3-%03d", i)
		ids = append(ids, id)
		if _, err := source.Append(context.Background(), port.ScopeProject, port.AxisZ3, port.Entry{ID: id, Body: "body of " + id}); err != nil {
			t.Fatalf("seed Z3 %s: %v", id, err)
		}
	}
	dir := t.TempDir()
	user := filepath.Join(dir, "user.db")
	target, err := Open(Config{
		UserDB:    user,
		ProjectDB: filepath.Join(dir, "project.db"),
		Origin:    "runtime",
		Profile:   ProfileHuman,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = target.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probe := &batchProbe{cancel: cancel, cancelAt: cancelInsideBatchAt}
	connector, err := sqlite.NewConnector(target.dsn(user))
	if err != nil {
		t.Fatalf("open an instrumented connector: %v", err)
	}
	instrumented := sql.OpenDB(&probeConnector{inner: connector, probe: probe})
	t.Cleanup(func() { _ = instrumented.Close() })
	target.mu.Lock()
	replaced := target.dbs[port.ScopeUser]
	target.dbs[port.ScopeUser] = instrumented
	target.mu.Unlock()
	if replaced != nil {
		t.Cleanup(func() { _ = replaced.Close() })
	}

	_, err = Promote(ctx, source, target, ids)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Promote cancelled inside the batch: got %v, want context.Canceled", err)
	}
	begins, inserts, cancelled, beginsAtCancel, insertsAtCancel := probe.snapshot()
	if !cancelled {
		t.Fatal("the probe never cancelled the context: the test would pass without exercising the batch")
	}
	if beginsAtCancel < 1 {
		t.Fatalf("the context was cancelled with %d transactions open, want at least 1", beginsAtCancel)
	}
	if insertsAtCancel != cancelInsideBatchAt {
		t.Fatalf("the context was cancelled at the INSERT number %d, want %d", insertsAtCancel, cancelInsideBatchAt)
	}
	if inserts < insertsAtCancel {
		t.Fatalf("the probe saw %d INSERTs, want the %d that cancelled it", inserts, insertsAtCancel)
	}
	if begins < beginsAtCancel {
		t.Fatalf("the probe saw %d transactions, want at least the %d open at the cancel", begins, beginsAtCancel)
	}
	if got := readZ2(t, target); len(got) != 0 {
		t.Fatalf("a batch cancelled with the transaction open left %d entries in Z2, want 0: %v", len(got), got)
	}
}