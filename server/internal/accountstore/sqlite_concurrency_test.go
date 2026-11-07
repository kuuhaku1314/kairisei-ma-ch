package accountstore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Measures session lookup only, using an isolated database, not player data.
func BenchmarkResolveSession(b *testing.B) {
	storage := &Database{databasePath: filepath.Join(b.TempDir(), "session.sqlite3")}
	b.Cleanup(func() {
		if err := storage.Close(); err != nil {
			b.Error(err)
		}
	})
	db, err := storage.Open()
	if err != nil {
		b.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE cn_local_account (user_id INTEGER PRIMARY KEY, session_key TEXT NOT NULL UNIQUE)`); err != nil {
		b.Fatal(err)
	}
	key := "local-cn-" + strings.Repeat("a", 64)
	if _, err := db.Exec(`INSERT INTO cn_local_account VALUES (?, ?)`, PrimaryUserID, key); err != nil {
		b.Fatal(err)
	}
	accounts := &Accounts{storage: storage}
	b.ResetTimer()
	for b.Loop() {
		if id, err := accounts.ResolveSession(key); err != nil || id != PrimaryUserID {
			b.Fatalf("session lookup: id=%d err=%v", id, err)
		}
	}
}

// A write transaction must wait before reading, not take a stale WAL snapshot
// and then fail on its first write. Readers must remain independent of it.
func TestSQLiteWriterWaitsBeforeSnapshot(t *testing.T) {
	storage := &Database{databasePath: filepath.Join(t.TempDir(), "存档 #1.sqlite3")}
	t.Cleanup(func() {
		if err := storage.Close(); err != nil {
			t.Error(err)
		}
	})
	first, err := storage.Open()
	if err != nil {
		t.Fatal(err)
	}
	// A separate owner models another process, so BEGIN IMMEDIATE is tested
	// independently of our in-process single-writer connection limit.
	other := &Database{databasePath: storage.databasePath}
	t.Cleanup(func() {
		if err := other.Close(); err != nil {
			t.Error(err)
		}
	})
	second, err := other.Open()
	if err != nil {
		t.Fatal(err)
	}
	reader, err := storage.OpenRead()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = first.Exec("CREATE TABLE counter (n INTEGER); INSERT INTO counter VALUES (0)"); err != nil {
		t.Fatal(err)
	}
	tx, err := first.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE counter SET n=1"); err != nil {
		t.Fatal(err)
	}
	stats := storage.Statistics()
	if stats.Writer.InUse != 1 || stats.Writer.MaxOpenConnections != 1 || !stats.Reader.Initialized ||
		stats.Files.DatabaseBytes == nil || *stats.Files.DatabaseBytes <= 0 || stats.Files.WALBytes == nil || *stats.Files.WALBytes <= 0 {
		t.Fatal("database metrics could not observe an active writer without querying it", stats)
	}
	ro, err := reader.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err = ro.QueryRow("SELECT n FROM counter").Scan(&n); err != nil || n != 0 {
		t.Fatalf("concurrent reader: n=%d err=%v", n, err)
	}
	ro.Rollback()
	type begun struct {
		tx  *sql.Tx
		err error
	}
	ready := make(chan begun, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() { next, e := second.BeginTx(ctx, nil); ready <- begun{next, e} }()
	select {
	case result := <-ready:
		if result.tx != nil {
			result.tx.Rollback()
		}
		t.Fatalf("second writer began before first commit: %v", result.err)
	case <-time.After(30 * time.Millisecond):
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	result := <-ready
	if result.err != nil {
		t.Fatal(result.err)
	}
	defer result.tx.Rollback()
	if err = result.tx.QueryRow("SELECT n FROM counter").Scan(&n); err != nil || n != 1 {
		t.Fatalf("writer read stale snapshot: n=%d err=%v", n, err)
	}
	if _, err = result.tx.Exec("UPDATE counter SET n=n+1"); err != nil {
		t.Fatal(err)
	}
	if err = result.tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestSQLitePoolLifecycleAndSessionInvalidation(t *testing.T) {
	t.Setenv("KAIRI_SQLITE_READ_CONNS", "2")
	storage, err := OpenDatabase(filepath.Join(t.TempDir(), "存档 #1.json"), "unused-seed.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	before := storage.Statistics()
	if before.Reader.Initialized || before.Writer.Initialized || before.Reader.MaxOpenConnections != 2 ||
		before.Files.DatabaseBytes == nil || *before.Files.DatabaseBytes != 0 {
		t.Fatal("metrics opened a connection or created a missing database", before)
	}
	t.Cleanup(func() {
		if err := storage.Close(); err != nil {
			t.Error(err)
		}
	})
	writers, err := storage.Open()
	if err != nil {
		t.Fatal(err)
	}
	readers, err := storage.OpenRead()
	if err != nil {
		t.Fatal(err)
	}
	if writers.Stats().MaxOpenConnections != 1 || readers.Stats().MaxOpenConnections != 2 {
		t.Fatal("pool configuration was not applied")
	}
	for i := 0; i < 3; i++ {
		got, err := storage.OpenRead()
		if err != nil || got != readers {
			t.Fatalf("pool was recreated: %v", err)
		}
	}
	if _, err := writers.Exec(`CREATE TABLE cn_local_account (user_id INTEGER PRIMARY KEY, session_key TEXT NOT NULL UNIQUE)`); err != nil {
		t.Fatal(err)
	}
	key := "local-cn-" + strings.Repeat("a", 64)
	if _, err := writers.Exec(`INSERT INTO cn_local_account VALUES (?, ?)`, PrimaryUserID, key); err != nil {
		t.Fatal(err)
	}
	accounts := &Accounts{storage: storage}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// Pin every reader to check connection-local pragmas, not just the first.
	var pinned []*sql.Conn
	for i := 0; i < 2; i++ {
		conn, err := readers.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		pinned = append(pinned, conn)
		for pragma, want := range map[string]int{"foreign_keys": 1, "busy_timeout": 5000, "synchronous": 2, "query_only": 1} {
			var got int
			if err := conn.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&got); err != nil || got != want {
				t.Fatalf("%s=%d: %v", pragma, got, err)
			}
		}
		var mode string
		if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
			t.Fatalf("journal_mode=%s: %v", mode, err)
		}
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := accounts.ResolveSessionContext(canceled, key); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled pool wait: %v", err)
	}
	for _, conn := range pinned {
		// Evict physical connections; replacements must get the same settings.
		if err := conn.Raw(func(any) error { return driver.ErrBadConn }); !errors.Is(err, driver.ErrBadConn) {
			t.Fatal(err)
		}
		_ = conn.Close()
	}
	if id, err := accounts.ResolveSessionContext(ctx, key); err != nil || id != PrimaryUserID {
		t.Fatalf("session: %d %v", id, err)
	}
	var foreignKeys int
	if err := readers.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatalf("replacement foreign_keys=%d: %v", foreignKeys, err)
	}
	if _, err := readers.Exec(`DELETE FROM cn_local_account`); err == nil {
		t.Fatal("reader permitted a write")
	}
	tx, err := writers.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	nextKey := "local-cn-" + strings.Repeat("b", 64)
	if _, err := tx.Exec(`UPDATE cn_local_account SET session_key=?`, nextKey); err != nil {
		t.Fatal(err)
	}
	// Reader observes the last committed identity without waiting for a writer.
	if id, err := accounts.ResolveSessionContext(ctx, key); err != nil || id != PrimaryUserID {
		t.Fatalf("session blocked by writer: %d %v", id, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.ResolveSessionContext(ctx, key); err == nil {
		t.Fatal("old session remained valid after commit")
	}
	if id, err := accounts.ResolveSessionContext(ctx, nextKey); err != nil || id != PrimaryUserID {
		t.Fatalf("new session: %d %v", id, err)
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Open(); err == nil {
		t.Fatal("closed writer reopened")
	}
	if _, err := storage.OpenRead(); err == nil {
		t.Fatal("closed reader reopened")
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
}

// Exactly 500 workers with distinct sessions. This exercises DB admission and
// lookup throughput only, not complete gameplay or a 500-player service SLA.
func BenchmarkResolveSession500(b *testing.B) {
	storage := &Database{databasePath: filepath.Join(b.TempDir(), "sessions.sqlite3")}
	b.Cleanup(func() {
		if err := storage.Close(); err != nil {
			b.Error(err)
		}
	})
	db, err := storage.Open()
	if err != nil {
		b.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE cn_local_account (user_id INTEGER PRIMARY KEY, session_key TEXT NOT NULL UNIQUE)`); err != nil {
		b.Fatal(err)
	}
	keys := make([]string, 500)
	tx, err := db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	defer tx.Rollback()
	for i := range keys {
		keys[i] = fmt.Sprintf("local-cn-%064d", i)
		if _, err := tx.Exec(`INSERT INTO cn_local_account VALUES (?, ?)`, PrimaryUserID+i, keys[i]); err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	accounts := &Accounts{storage: storage}
	readers, err := storage.OpenRead()
	if err != nil {
		b.Fatal(err)
	}
	var ready, done sync.WaitGroup
	ready.Add(500)
	done.Add(500)
	start := make(chan struct{})
	for i := range keys {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			for n := i; n < b.N; n += 500 {
				if id, err := accounts.ResolveSession(keys[i]); err != nil || id != PrimaryUserID+i {
					b.Errorf("session %d: id=%d err=%v", i, id, err)
					return
				}
			}
		}()
	}
	ready.Wait()
	b.ResetTimer()
	close(start)
	done.Wait()
	b.StopTimer()
	stats := readers.Stats()
	if stats.OpenConnections > defaultSQLiteReadConns {
		b.Fatal("reader pool exceeded its limit")
	}
	b.ReportMetric(float64(stats.OpenConnections), "read-conns")
}

// A bounded burst, not a gameplay capacity claim: 500 independent sessions each
// do 20 lookups and one committed counter update while sharing the two pools.
func TestSQLite500SessionReadWriteBurst(t *testing.T) {
	storage := &Database{databasePath: filepath.Join(t.TempDir(), "burst.sqlite3")}
	t.Cleanup(func() {
		if err := storage.Close(); err != nil {
			t.Error(err)
		}
	})
	db, err := storage.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE cn_local_account (user_id INTEGER PRIMARY KEY, session_key TEXT NOT NULL UNIQUE);
		CREATE TABLE counter (n INTEGER NOT NULL); INSERT INTO counter VALUES (0)`); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 500)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := range keys {
		keys[i] = fmt.Sprintf("local-cn-%064d", i)
		if _, err := tx.Exec(`INSERT INTO cn_local_account VALUES (?, ?)`, PrimaryUserID+i, keys[i]); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	accounts := &Accounts{storage: storage}
	readers, err := storage.OpenRead()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	start := make(chan struct{})
	var ready, done sync.WaitGroup
	ready.Add(500)
	done.Add(500)
	readLatency := make([]time.Duration, 500*20)
	writeLatency := make([]time.Duration, 500)
	for i := range keys {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			for n := 0; n < 20; n++ {
				began := time.Now()
				if id, err := accounts.ResolveSessionContext(ctx, keys[i]); err != nil || id != PrimaryUserID+i {
					t.Errorf("session %d: id=%d err=%v", i, id, err)
					return
				}
				readLatency[i*20+n] = time.Since(began)
				if n == 5 {
					began := time.Now()
					tx, err := db.BeginTx(ctx, nil)
					if err != nil {
						t.Error(err)
						return
					}
					if _, err = tx.ExecContext(ctx, `UPDATE counter SET n=n+1`); err != nil {
						_ = tx.Rollback()
						t.Error(err)
						return
					}
					if err = tx.Commit(); err != nil {
						t.Error(err)
						return
					}
					writeLatency[i] = time.Since(began)
				}
			}
		}()
	}
	ready.Wait()
	began := time.Now()
	close(start)
	done.Wait()
	elapsed := time.Since(began)
	var count int
	if err := readers.QueryRowContext(ctx, `SELECT n FROM counter`).Scan(&count); err != nil || count != 500 {
		t.Fatalf("committed writes=%d: %v", count, err)
	}
	if readers.Stats().OpenConnections > defaultSQLiteReadConns || db.Stats().OpenConnections != 1 {
		t.Fatal("connection bound exceeded")
	}
	slices.Sort(readLatency)
	slices.Sort(writeLatency)
	t.Logf("500 workers, 10000 auth reads, 500 small FULL-sync commits: total=%s read p50=%s p95=%s max=%s; write incl. queue p50=%s p95=%s max=%s; read connections=%d, writer=%d, writer waits=%d",
		elapsed, readLatency[5000], readLatency[9500], readLatency[9999], writeLatency[250], writeLatency[475], writeLatency[499], readers.Stats().OpenConnections, db.Stats().OpenConnections, db.Stats().WaitCount)
}
