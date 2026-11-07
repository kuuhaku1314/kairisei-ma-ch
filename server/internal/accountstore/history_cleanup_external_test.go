package accountstore_test

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func TestHistoryCleanupKeepsUnfinishedBatchesRecentDaysAndOtherBosses(t *testing.T) {
	a := testfixture.NewFriendCapacityTestAccounts(t)
	first := testfixture.CreateNamedFriendCapacityAccount(t, a, 1)
	second := testfixture.CreateNamedFriendCapacityAccount(t, a, 2)
	d := a.Database()
	db, err := d.Open()
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := db.QueryRow(query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	old := time.Now().UTC().AddDate(0, 0, -40).Format(time.RFC3339Nano)
	batch := func(id, created string, delivered ...int) {
		t.Helper()
		doc, err := d.WriteDocument(accountstore.MailBatchPrefix+id, 0, map[string]any{"batch_id": id, "title": id, "user_ids": []int{first, second}}, "mail-batch")
		if err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE cn_global_operation SET updated_utc=? WHERE operation_key=?`, created, accountstore.MailBatchPrefix+id)
		for _, user := range delivered {
			result := `{"ok":true}`
			sum := sha256.Sum256([]byte(result))
			exec(`INSERT INTO cn_admin_action_receipt VALUES(?,?,?,?,?,?)`, accountstore.MailBatchPrefix+id, user, doc.SHA256, result, hex.EncodeToString(sum[:]), old)
		}
	}
	batch("finished-old", old, first, second)
	batch("unfinished-old", old, first)
	batch("finished-new", time.Now().UTC().Format(time.RFC3339Nano), first, second)
	today := gamestate.BattleClearDay(time.Now().Unix())
	for _, day := range []int64{today, today - 1, today - 5, today - 10} {
		exec(`INSERT INTO cn_battle_clear_daily VALUES(?,?,0,?,0,1,?,?,?,?)`, day, 9001, first, day*86400, "clear", []byte(`[]`), strings.Repeat("c", 64))
	}
	for _, row := range [][2]int{{100, first}, {100, second}, {200, first}} {
		exec(`INSERT INTO cn_boss_score_rank VALUES(?,1,?,10,1,1)`, row[0], row[1])
	}
	exec(`INSERT INTO cn_boss_score_rank VALUES(100,0,?,20,2,2)`, first)

	request := accountstore.HistoryCleanupRequest{
		MailBeforeUTC: time.Now().UTC().AddDate(0, 0, -5).Format(time.RFC3339Nano),
		ClearKeepDays: 3,
		RankBossIDs:   []int{100},
	}
	preview, err := d.CleanupHistory(context.Background(), request, "")
	if err != nil || preview.MailBatches != 1 || preview.MailReceipts != 2 || preview.ClearRows != 2 ||
		len(preview.RankRows) != 1 || preview.RankRows[0].Rows != 3 || preview.Applied {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if _, err = d.CleanupHistory(context.Background(), request, strings.Repeat("0", 64)); err == nil {
		t.Fatal("stale digest accepted")
	}
	result, err := d.CleanupHistory(context.Background(), request, preview.Digest)
	if err != nil || !result.Applied || result.Backup == "" {
		t.Fatalf("apply %+v %v", result, err)
	}
	if _, err := os.Stat(result.Backup); err != nil {
		t.Fatal("backup missing", err)
	}
	backup, err := os.Open(result.Backup)
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	zipped, err := gzip.NewReader(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer zipped.Close()
	decoder := json.NewDecoder(zipped)
	modes := [2]int{}
	for {
		var row struct {
			Rank *struct {
				Mode int `json:"mode"`
			} `json:"boss_score_rank"`
		}
		if err = decoder.Decode(&row); err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if row.Rank != nil {
			modes[row.Rank.Mode]++
		}
	}
	if modes != [2]int{1, 2} {
		t.Fatal("backup lost score participation modes", modes)
	}
	if count(`SELECT count(*) FROM cn_global_operation WHERE operation_key GLOB 'mail-batch:*'`) != 2 ||
		count(`SELECT count(*) FROM cn_admin_action_receipt WHERE operation_key=?`, accountstore.MailBatchPrefix+"finished-old") != 0 ||
		count(`SELECT count(*) FROM cn_admin_action_receipt WHERE operation_key=?`, accountstore.MailBatchPrefix+"unfinished-old") != 1 {
		t.Fatal("mail batch selection changed")
	}
	if count(`SELECT count(*) FROM cn_battle_clear_daily`) != 2 || count(`SELECT count(*) FROM cn_battle_clear_daily WHERE day<?`, today-1) != 0 {
		t.Fatal("clear history window changed")
	}
	if count(`SELECT count(*) FROM cn_boss_score_rank WHERE boss_id=100`) != 0 || count(`SELECT count(*) FROM cn_boss_score_rank WHERE boss_id=200`) != 1 {
		t.Fatal("leaderboard selection changed")
	}
	for id, want := range map[string]bool{"finished-old": true, "finished-new": false, "unfinished-old": false} {
		if retired, err := d.MailBatchRetired(id); err != nil || retired != want {
			t.Fatalf("retired %s = %v %v", id, retired, err)
		}
	}

	compact := accountstore.HistoryCleanupRequest{Vacuum: true}
	preview, err = d.CleanupHistory(context.Background(), compact, "")
	if err != nil {
		t.Fatal(err)
	}
	result, err = d.CleanupHistory(context.Background(), compact, preview.Digest)
	if err != nil || !result.Applied || result.FileBytesAfter <= 0 || result.Backup != "" {
		t.Fatalf("vacuum %+v %v", result, err)
	}
	if err := (&accountstore.HistoryCleanupRequest{ClearKeepDays: 1}).Normalize(time.Now()); err == nil {
		t.Fatal("yesterday ranking window accepted")
	}
}

func TestHistoryCleanupRejectsChangedRowsWithoutCountChanges(t *testing.T) {
	a := testfixture.NewFriendCapacityTestAccounts(t)
	user := testfixture.CreateNamedFriendCapacityAccount(t, a, 1)
	d := a.Database()
	db, err := d.Open()
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	oldDay := gamestate.BattleClearDay(time.Now().Unix()) - 10
	exec(`INSERT INTO cn_boss_score_rank VALUES(100,1,?,10,1,1)`, user)
	exec(`INSERT INTO cn_battle_clear_daily VALUES(?,100,0,?,0,1,1,'event',?,?)`, oldDay, user, []byte(`[]`), strings.Repeat("c", 64))
	request := accountstore.HistoryCleanupRequest{RankBossIDs: []int{100}, ClearKeepDays: 2}
	preview, err := d.CleanupHistory(ctx, request, "")
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE cn_boss_score_rank SET best_score=20 WHERE boss_id=100`)
	if _, err = d.CleanupHistory(ctx, request, preview.Digest); err == nil {
		t.Fatal("changed score accepted with stale preview")
	}
	preview, err = d.CleanupHistory(ctx, request, "")
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE cn_battle_clear_daily SET clear_count=2 WHERE boss_id=100`)
	if _, err = d.CleanupHistory(ctx, request, preview.Digest); err == nil {
		t.Fatal("changed clear count accepted with stale preview")
	}
	preview, err = d.CleanupHistory(ctx, request, "")
	if err != nil {
		t.Fatal(err)
	}
	exec(`CREATE TRIGGER fail_history BEFORE DELETE ON cn_boss_score_rank BEGIN SELECT RAISE(ABORT,'injected failure'); END`)
	failed, err := d.CleanupHistory(ctx, request, preview.Digest)
	if err == nil || failed.Applied || failed.Backup != "" {
		t.Fatal("failed cleanup reported a commit or missing backup", failed, err)
	}
	var n int
	if err = db.QueryRow(`SELECT count(*) FROM cn_battle_clear_daily`).Scan(&n); err != nil || n != 1 {
		t.Fatal("partial history delete committed", n, err)
	}
}

func TestHistoryCleanupFingerprintsAndVerifiesMailReceipts(t *testing.T) {
	a := testfixture.NewFriendCapacityTestAccounts(t)
	d := a.Database()
	db, err := d.Open()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	key := accountstore.MailBatchPrefix + "fingerprint"
	doc, err := d.WriteDocument(key, 0, map[string]any{"user_ids": []int{accountstore.PrimaryUserID}}, "mail-batch")
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	if _, err = db.Exec(`UPDATE cn_global_operation SET updated_utc=? WHERE operation_key=?`, old, key); err != nil {
		t.Fatal(err)
	}
	result := []byte(`{"ok":true}`)
	sum := sha256.Sum256(result)
	if _, err = db.Exec(`INSERT INTO cn_admin_action_receipt VALUES(?,?,?,?,?,?)`, key, accountstore.PrimaryUserID, doc.SHA256, result, hex.EncodeToString(sum[:]), old); err != nil {
		t.Fatal(err)
	}
	request := accountstore.HistoryCleanupRequest{MailBeforeUTC: time.Now().UTC().Format(time.RFC3339Nano)}
	preview, err := d.CleanupHistory(ctx, request, "")
	if err != nil {
		t.Fatal(err)
	}
	result = []byte(`{"ok":true,"changed":1}`)
	sum = sha256.Sum256(result)
	if _, err = db.Exec(`UPDATE cn_admin_action_receipt SET result_json=?,result_sha256=? WHERE operation_key=?`, result, hex.EncodeToString(sum[:]), key); err != nil {
		t.Fatal(err)
	}
	if _, err = d.CleanupHistory(ctx, request, preview.Digest); err == nil {
		t.Fatal("changed same-count receipt accepted")
	}
	if _, err = db.Exec(`UPDATE cn_admin_action_receipt SET result_sha256=? WHERE operation_key=?`, strings.Repeat("0", 64), key); err != nil {
		t.Fatal(err)
	}
	if _, err = d.CleanupHistory(ctx, request, ""); err == nil {
		t.Fatal("corrupt receipt accepted for cleanup")
	}
}

func TestHistoryCleanupKeepsSyncedBackupWhenCommitFails(t *testing.T) {
	a := testfixture.NewFriendCapacityTestAccounts(t)
	user := testfixture.CreateNamedFriendCapacityAccount(t, a, 1)
	d := a.Database()
	db, err := d.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO cn_boss_score_rank VALUES(100,1,?,10,1,1)`, user); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`CREATE TABLE cleanup_parent(id INTEGER PRIMARY KEY)`,
		`CREATE TABLE cleanup_child(id INTEGER REFERENCES cleanup_parent(id) DEFERRABLE INITIALLY DEFERRED)`,
		`CREATE TRIGGER fail_history_commit AFTER DELETE ON cn_boss_score_rank BEGIN INSERT INTO cleanup_child VALUES(1); END`,
	} {
		if _, err = db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	request := accountstore.HistoryCleanupRequest{RankBossIDs: []int{100}}
	preview, err := d.CleanupHistory(context.Background(), request, "")
	if err != nil {
		t.Fatal(err)
	}
	failed, err := d.CleanupHistory(context.Background(), request, preview.Digest)
	if err == nil || failed.Applied || failed.Backup == "" {
		t.Fatal("commit failure lost recovery backup", failed, err)
	}
	if _, err = os.Stat(failed.Backup); err != nil {
		t.Fatal("synced backup removed on commit failure", err)
	}
}
