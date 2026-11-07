package accountstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

const creationPrimaryUUID = "00000000-0000-4000-8000-000000000001"

func creationTestAccounts(t *testing.T) *Accounts {
	t.Helper()
	seed, err := os.ReadFile(filepath.Join("..", "..", "config", "cn602-save-template.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	seedPath, savePath := filepath.Join(dir, "seed.json"), filepath.Join(dir, "save.json")
	for _, path := range []string{seedPath, savePath} {
		if err := os.WriteFile(path, seed, 0600); err != nil {
			t.Fatal(err)
		}
	}
	storage, err := OpenDatabase(savePath, seedPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	if _, err := storage.LoadOrImport(); err != nil {
		t.Fatal(err)
	}
	accounts, err := NewAccounts(storage)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.ResolveInternalLogin(creationPrimaryUUID); err != nil {
		t.Fatal(err)
	}
	return accounts
}

func requireCreationRejection(t *testing.T, err error, code string) *CreationRejection {
	t.Helper()
	var rejection *CreationRejection
	if !errors.As(err, &rejection) || rejection.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
	return rejection
}

func creationCount(t *testing.T, accounts *Accounts) int {
	t.Helper()
	db, err := accounts.storage.OpenRead()
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COALESCE(SUM(created_count),0) FROM cn_account_creation_daily`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestCreationFinalSlotSharedAcrossLoginAndBinding(t *testing.T) {
	accounts := creationTestAccounts(t)
	if err := accounts.AttachCreationPolicy(func() CreationPolicy { return CreationPolicy{DailyLimit: 1, VerificationBits: 0} }); err != nil {
		t.Fatal(err)
	}
	source := netip.MustParseAddr("192.0.2.1")
	accounts.mu.Lock()
	results := make(chan error, 4)
	for index := 0; index < 4; index++ {
		go func() {
			uuid := fmt.Sprintf("00000000-0000-4000-8000-%012d", index+2)
			var err error
			if index%2 == 0 {
				_, err = accounts.ResolveClientLogin(context.Background(), uuid, source, CreationProof{})
			} else {
				_, err = accounts.BindClientAccount(context.Background(), uuid, fmt.Sprintf("player_%d", index), "valid-password", source, CreationProof{})
			}
			results <- err
		}()
	}
	guard := accounts.creation.Load()
	deadline := time.Now().Add(3 * time.Second)
	admitted := 0
	for time.Now().Before(deadline) {
		guard.mu.Lock()
		admitted = len(guard.attempts[source])
		guard.mu.Unlock()
		if admitted == 4 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	accounts.mu.Unlock()
	if admitted != 4 {
		t.Fatal("requests did not all pass the read precheck")
	}
	success := 0
	for range 4 {
		err := <-results
		if err == nil {
			success++
		} else {
			requireCreationRejection(t, err, CreationDailyLimit)
		}
	}
	if success != 1 || creationCount(t, accounts) != 1 {
		t.Fatal("last quota slot was not exclusive", success)
	}
	// Another source can create via binding, and its login retry consumes no slot.
	other := netip.MustParseAddr("192.0.2.2")
	const uuid = "00000000-0000-4000-8000-000000000099"
	binding, err := accounts.BindClientAccount(context.Background(), uuid, "bound_player", "valid-password", other, CreationProof{})
	if err != nil {
		t.Fatal(err)
	}
	if err := accounts.storage.Close(); err != nil {
		t.Fatal(err)
	}
	storage, err := OpenDatabase(accounts.storage.jsonPath, accounts.storage.seedPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	restarted, err := NewAccounts(storage)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.AttachCreationPolicy(func() CreationPolicy { return CreationPolicy{DailyLimit: 1, VerificationBits: 0} }); err != nil {
		t.Fatal(err)
	}
	identity, err := restarted.ResolveClientLogin(context.Background(), uuid, other, CreationProof{})
	if err != nil || identity.UserID != binding.UserID || creationCount(t, restarted) != 2 {
		t.Fatal("restart or UUID retry changed creation facts", identity, err)
	}
	_, err = restarted.ResolveClientLogin(context.Background(), "00000000-0000-4000-8000-000000000100", other, CreationProof{})
	requireCreationRejection(t, err, CreationDailyLimit)
}

func TestCreationRollbackForCredentialsSeedAndCancellation(t *testing.T) {
	accounts := creationTestAccounts(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var reads int
	cancelFinal := false
	if err := accounts.AttachCreationPolicy(func() CreationPolicy {
		reads++
		if cancelFinal && reads == 2 {
			cancel()
		}
		return CreationPolicy{DailyLimit: 10, VerificationBits: 0}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.BindInternalAccount(creationPrimaryUUID, "taken_name", "valid-password"); err != nil {
		t.Fatal(err)
	}
	source := netip.MustParseAddr("192.0.2.10")
	const uuid = "00000000-0000-4000-8000-000000000002"
	_, err := accounts.BindClientAccount(context.Background(), uuid, "taken_name", "valid-password", source, CreationProof{})
	if err == nil || creationCount(t, accounts) != 0 {
		t.Fatal("credential conflict consumed quota", err)
	}
	if binding, err := accounts.AccountBinding(uuid); err != nil || binding.UserID != 0 {
		t.Fatal("credential conflict retained identity", binding, err)
	}
	seed, err := os.ReadFile(accounts.storage.seedPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(accounts.storage.seedPath, []byte("invalid seed"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = accounts.ResolveClientLogin(context.Background(), uuid, source, CreationProof{})
	if err == nil || creationCount(t, accounts) != 0 {
		t.Fatal("seed failure consumed quota", err)
	}
	if err := os.WriteFile(accounts.storage.seedPath, seed, 0600); err != nil {
		t.Fatal(err)
	}
	reads, cancelFinal = 0, true
	_, err = accounts.ResolveClientLogin(ctx, uuid, source, CreationProof{})
	if !errors.Is(err, context.Canceled) || creationCount(t, accounts) != 0 {
		t.Fatal("cancelled transaction consumed quota", err)
	}
	if binding, err := accounts.AccountBinding(uuid); err != nil || binding.UserID != 0 {
		t.Fatal("cancelled transaction retained identity", binding, err)
	}
}

func TestCreationReadRejectionAvoidsWriterAndSeed(t *testing.T) {
	accounts := creationTestAccounts(t)
	var limit atomic.Int64
	limit.Store(1)
	if err := accounts.AttachCreationPolicy(func() CreationPolicy { return CreationPolicy{DailyLimit: int(limit.Load()), VerificationBits: 0} }); err != nil {
		t.Fatal(err)
	}
	source := netip.MustParseAddr("192.0.2.20")
	if _, err := accounts.ResolveClientLogin(context.Background(), "00000000-0000-4000-8000-000000000002", source, CreationProof{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(accounts.storage.seedPath, []byte("unreadable seed"), 0600); err != nil {
		t.Fatal(err)
	}
	db, _ := accounts.storage.Open()
	blocker, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	waits := db.Stats().WaitCount
	for _, daily := range []int64{1, 0} {
		limit.Store(daily)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err := accounts.BindClientAccount(ctx, "00000000-0000-4000-8000-000000000003", "new_player", "valid-password", source, CreationProof{})
		cancel()
		code := CreationDailyLimit
		if daily == 0 {
			code = CreationDisabled
		}
		rejection := requireCreationRejection(t, err, code)
		if (daily == 0) != (rejection.RetryAfterSeconds == 0) {
			t.Fatal("incorrect retry hint", rejection)
		}
	}
	if db.Stats().WaitCount != waits || creationCount(t, accounts) != 1 {
		t.Fatal("read refusal waited for writer or changed count")
	}
}

func TestCreationAttemptBoundsAndKnownIdentityBypass(t *testing.T) {
	accounts := creationTestAccounts(t)
	if err := accounts.AttachCreationPolicy(func() CreationPolicy { return CreationPolicy{DailyLimit: 0, VerificationBits: 0} }); err != nil {
		t.Fatal(err)
	}
	source := netip.MustParseAddr("192.0.2.30")
	for index := range 13 {
		uuid := fmt.Sprintf("00000000-0000-4000-8000-%012d", index+2)
		var err error
		if index%2 == 0 {
			_, err = accounts.ResolveClientLogin(context.Background(), uuid, source, CreationProof{})
		} else {
			_, err = accounts.BindClientAccount(context.Background(), uuid, "new_player", "valid-password", source, CreationProof{})
		}
		code := CreationDisabled
		if index == 12 {
			code = CreationRateLimited
		}
		requireCreationRejection(t, err, code)
	}
	if _, err := accounts.ResolveClientLogin(context.Background(), creationPrimaryUUID, source, CreationProof{}); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.BindClientAccount(context.Background(), creationPrimaryUUID, "known_player", "valid-password", source, CreationProof{}); err != nil {
		t.Fatal(err)
	}
	guard := accounts.creation.Load()
	now := time.Now()
	guard.now = func() time.Time { return now }
	guard.mu.Lock()
	guard.attempts = make(map[netip.Addr][]time.Time, creationAddressLimit)
	for index := range creationAddressLimit {
		guard.attempts[netip.AddrFrom4([4]byte{10, 0, byte(index / 256), byte(index)})] = []time.Time{now}
	}
	guard.lastSweep = now
	guard.mu.Unlock()
	requireCreationRejection(t, guard.allowAttempt(source), CreationRateLimited)
	if len(guard.attempts) != creationAddressLimit {
		t.Fatal("active buckets evicted")
	}
	if _, err := accounts.ResolveClientLogin(context.Background(), creationPrimaryUUID, source, CreationProof{}); err != nil {
		t.Fatal("known identity hit bucket capacity", err)
	}
	now = now.Add(time.Minute)
	if err := guard.allowAttempt(source); err != nil || len(guard.attempts) != 1 {
		t.Fatal("expired buckets did not recycle", err)
	}
	stats := accounts.CreationRejections()
	if stats[CreationDisabled] != 12 || stats[CreationRateLimited] != 1 || creationCount(t, accounts) != 0 {
		t.Fatal("rejections were not aggregated", stats)
	}
}

func TestCreationUTC8BoundaryPolicyAndRetention(t *testing.T) {
	accounts := creationTestAccounts(t)
	var limit atomic.Int64
	limit.Store(1)
	if err := accounts.AttachCreationPolicy(func() CreationPolicy { return CreationPolicy{DailyLimit: int(limit.Load()), VerificationBits: 0} }); err != nil {
		t.Fatal(err)
	}
	guard := accounts.creation.Load()
	now := time.Date(2026, 11, 4, 15, 59, 59, 0, time.UTC)
	guard.now = func() time.Time { return now }
	source := netip.MustParseAddr("192.0.2.40")
	newLogin := func(number int) error {
		_, err := accounts.ResolveClientLogin(context.Background(), fmt.Sprintf("00000000-0000-4000-8000-%012d", number), source, CreationProof{})
		return err
	}
	if err := newLogin(2); err != nil {
		t.Fatal(err)
	}
	if got := requireCreationRejection(t, newLogin(3), CreationDailyLimit).RetryAfterSeconds; got != 1 {
		t.Fatal("UTC+8 midnight hint", got)
	}
	limit.Store(2)
	if err := newLogin(3); err != nil {
		t.Fatal("raised limit did not take effect", err)
	}
	limit.Store(0)
	requireCreationRejection(t, newLogin(4), CreationDisabled)
	limit.Store(1)
	requireCreationRejection(t, newLogin(4), CreationDailyLimit)
	// A refusal crossing midnight repeats the snapshot once, not an endless loop.
	clockReads := 0
	attemptsBefore := len(guard.attempts[source])
	guard.now = func() time.Time {
		clockReads++
		if clockReads == 1 {
			return now
		}
		return now.Add(time.Second)
	}
	if err := newLogin(4); err != nil {
		t.Fatal("cross-day refusal was not rechecked", err)
	}
	if len(guard.attempts[source]) != attemptsBefore+1 {
		t.Fatal("cross-day recheck charged multiple attempts")
	}
	now = now.Add(time.Second)
	guard.now = func() time.Time { return now }
	db, _ := accounts.storage.Open()
	for index := range 130 {
		if _, err := db.Exec(`INSERT INTO cn_account_creation_daily VALUES('2026-10-01', ?, 1)`, fmt.Sprint(index)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO cn_account_creation_daily VALUES('2026-10-30', 'retained', 1)`); err != nil {
		t.Fatal(err)
	}
	limit.Store(2)
	if err := newLogin(5); err != nil {
		t.Fatal(err)
	}
	var old, retained int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM cn_account_creation_daily WHERE day='2026-10-01'), (SELECT COUNT(*) FROM cn_account_creation_daily WHERE day='2026-10-30')`).Scan(&old, &retained); err != nil || old != 2 || retained != 1 {
		t.Fatal("bounded composite-key retention failed", old, retained, err)
	}
}

func TestCreationPolicyAndSourceRequired(t *testing.T) {
	accounts := creationTestAccounts(t)
	ctx := context.Background()
	source := netip.MustParseAddr("127.0.0.1")
	if _, err := accounts.ResolveClientLogin(ctx, creationPrimaryUUID, source, CreationProof{}); err == nil {
		t.Fatal("missing policy accepted")
	}
	if err := accounts.AttachCreationPolicy(nil); err == nil {
		t.Fatal("nil policy accepted")
	}
	if err := accounts.AttachCreationPolicy(func() CreationPolicy { return CreationPolicy{DailyLimit: 10, VerificationBits: 0} }); err != nil {
		t.Fatal(err)
	}
	if err := accounts.AttachCreationPolicy(func() CreationPolicy { return CreationPolicy{DailyLimit: 10, VerificationBits: 0} }); err == nil {
		t.Fatal("duplicate policy accepted")
	}
	if _, err := accounts.ResolveClientLogin(ctx, creationPrimaryUUID, netip.Addr{}, CreationProof{}); err == nil {
		t.Fatal("invalid source accepted")
	}
	if _, err := accounts.BindClientAccount(ctx, creationPrimaryUUID, "new_player", "valid-password", netip.Addr{}, CreationProof{}); err == nil {
		t.Fatal("invalid binding source accepted")
	}
}

func TestCreationUsesDayAfterWriterPermission(t *testing.T) {
	accounts := creationTestAccounts(t)
	if err := accounts.AttachCreationPolicy(func() CreationPolicy { return CreationPolicy{DailyLimit: 1, VerificationBits: 0} }); err != nil {
		t.Fatal(err)
	}
	before := time.Date(2026, 11, 4, 15, 59, 59, 0, time.UTC)
	var seconds atomic.Int64
	seconds.Store(before.Unix())
	accounts.creation.Load().now = func() time.Time { return time.Unix(seconds.Load(), 0) }
	db, _ := accounts.storage.Open()
	blocker, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	waits := db.Stats().WaitCount
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := accounts.ResolveClientLogin(ctx, "00000000-0000-4000-8000-000000000002", netip.MustParseAddr("192.0.2.50"), CreationProof{})
		done <- err
	}()
	deadline := time.Now().Add(time.Second)
	for db.Stats().WaitCount == waits && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if db.Stats().WaitCount == waits {
		t.Fatal("request did not wait for writer")
	}
	seconds.Store(before.Add(time.Second).Unix())
	if err := blocker.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var day string
	if err := db.QueryRow(`SELECT day FROM cn_account_creation_daily`).Scan(&day); err != nil || day != "2026-11-05" {
		t.Fatal("day was frozen before acquiring writer", day, err)
	}
}
