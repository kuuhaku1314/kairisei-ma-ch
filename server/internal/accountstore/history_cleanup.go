package accountstore

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"kairisei.local/server/internal/gamestate"
)

// MailBatchPrefix keys admin mail batch documents; their per-player delivery
// receipts use the same operation key.
const MailBatchPrefix = "mail-batch:"

// RetiredMailBatchPrefix marks each cleaned batch independently. A stale
// browser retry can therefore never re-create and resend that batch.
const RetiredMailBatchPrefix = "retired-mail-batch:"

// HistoryCleanupRequest selects display-only history. Every part is optional;
// account inventory, progress, claims and gift boxes are never part of it.
type HistoryCleanupRequest struct {
	// Finished admin mail batches created before this instant, with their receipts.
	MailBeforeUTC string `json:"mail_before_utc,omitempty"`
	// Clear decks and daily clear counts keep today and the previous days.
	ClearKeepDays int `json:"clear_keep_days,omitempty"`
	// Bosses whose score leaderboard restarts empty.
	RankBossIDs []int `json:"rank_boss_ids,omitempty"`
	// Rebuild the database file afterwards so freed pages return to the disk.
	Vacuum bool `json:"vacuum,omitempty"`
}

type HistoryRankCount struct {
	BossID int `json:"boss_id"`
	Rows   int `json:"rows"`
}

type HistoryCleanupReport struct {
	Request        HistoryCleanupRequest `json:"request"`
	MailBatches    int                   `json:"mail_batches"`
	MailReceipts   int                   `json:"mail_receipts"`
	MailBytes      int64                 `json:"mail_bytes"`
	ClearBeforeDay int64                 `json:"clear_before_day,omitempty"`
	ClearRows      int                   `json:"clear_rows"`
	ClearBytes     int64                 `json:"clear_bytes"`
	RankRows       []HistoryRankCount    `json:"rank_rows"`
	FileBytes      int64                 `json:"file_bytes"`
	FreeBytes      int64                 `json:"free_bytes"`
	FileBytesAfter int64                 `json:"file_bytes_after,omitempty"`
	Digest         string                `json:"digest"`
	Backup         string                `json:"backup,omitempty"`
	Applied        bool                  `json:"applied"`
}

// HistoryOverview is the read-only size summary shown before choosing a cleanup.
type HistoryOverview struct {
	MailBatches           int                `json:"mail_batches"`
	MailFinished          int                `json:"mail_finished"`
	MailReceipts          int                `json:"mail_receipts"`
	MailBytes             int64              `json:"mail_bytes"`
	MailOldestFinishedUTC string             `json:"mail_oldest_finished_utc,omitempty"`
	ClearRows             int                `json:"clear_rows"`
	ClearBytes            int64              `json:"clear_bytes"`
	ClearOldestDay        int64              `json:"clear_oldest_day,omitempty"`
	ClearToday            int64              `json:"clear_today"`
	RankRows              []HistoryRankCount `json:"rank_rows"`
	FileBytes             int64              `json:"file_bytes"`
	FreeBytes             int64              `json:"free_bytes"`
}

const (
	minClearKeepDays = 2 // The yesterday ranking reads the previous day.
	maxClearKeepDays = 29
)

// Normalize validates the request and puts it in its canonical form.
func (request *HistoryCleanupRequest) Normalize(now time.Time) error {
	if request.MailBeforeUTC != "" {
		before, err := time.Parse(time.RFC3339Nano, request.MailBeforeUTC)
		if err != nil || before.After(now) {
			return errors.New("礼物批次的截止时间须不晚于当前时间")
		}
		request.MailBeforeUTC = before.UTC().Format(time.RFC3339Nano)
	}
	if request.ClearKeepDays != 0 && (request.ClearKeepDays < minClearKeepDays || request.ClearKeepDays > maxClearKeepDays) {
		return fmt.Errorf("通关卡组须保留 %d–%d 天（昨日排行需要昨天的数据）", minClearKeepDays, maxClearKeepDays)
	}
	if len(request.RankBossIDs) > 1000 {
		return errors.New("一次最多重置 1000 个 Boss 的排行榜")
	}
	slices.Sort(request.RankBossIDs)
	request.RankBossIDs = slices.Compact(request.RankBossIDs)
	if len(request.RankBossIDs) > 0 && request.RankBossIDs[0] <= 0 {
		return errors.New("Boss ID 无效")
	}
	if request.MailBeforeUTC == "" && request.ClearKeepDays == 0 && len(request.RankBossIDs) == 0 && !request.Vacuum {
		return errors.New("请至少选择一项清理内容")
	}
	return nil
}

func databaseFileSize(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (file, free int64, err error) {
	var pages, size, freelist int64
	if err = q.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pages); err != nil {
		return 0, 0, err
	}
	if err = q.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&size); err != nil {
		return 0, 0, err
	}
	if err = q.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&freelist); err != nil {
		return 0, 0, err
	}
	return pages * size, freelist * size, nil
}

// HistoryOverview reads one consistent snapshot of the cleanable history sizes.
func (d *Database) HistoryOverview(ctx context.Context) (overview HistoryOverview, err error) {
	db, err := d.OpenRead()
	if err != nil {
		return overview, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return overview, err
	}
	defer tx.Rollback()
	batches, err := mailBatchSizes(ctx, tx, "")
	if err != nil {
		return overview, err
	}
	for _, batch := range batches {
		overview.MailBatches++
		overview.MailReceipts += batch.Receipts
		overview.MailBytes += batch.Bytes
		if batch.finished() {
			overview.MailFinished++
			if overview.MailOldestFinishedUTC == "" || batch.UpdatedUTC < overview.MailOldestFinishedUTC {
				overview.MailOldestFinishedUTC = batch.UpdatedUTC
			}
		}
	}
	var oldest sql.NullInt64
	if err = tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(length(decks_json)),0),min(day) FROM cn_battle_clear_daily`).
		Scan(&overview.ClearRows, &overview.ClearBytes, &oldest); err != nil {
		return overview, err
	}
	overview.ClearOldestDay = oldest.Int64
	overview.ClearToday = gamestate.BattleClearDay(time.Now().Unix())
	if overview.RankRows, err = rankCounts(ctx, tx, nil); err != nil {
		return overview, err
	}
	overview.FileBytes, overview.FreeBytes, err = databaseFileSize(ctx, tx)
	return overview, err
}

type mailBatchSize struct {
	Key, UpdatedUTC, SHA256 string
	Recipients, Receipts    int
	Bytes                   int64
}

// A batch is finished once every recipient holds a delivery receipt.
func (batch mailBatchSize) finished() bool {
	return batch.Recipients > 0 && batch.Receipts >= batch.Recipients
}

func mailBatchSizes(ctx context.Context, tx *sql.Tx, before string) ([]mailBatchSize, error) {
	rows, err := tx.QueryContext(ctx, `SELECT op.operation_key,op.updated_utc,op.payload_sha256,
 COALESCE(json_array_length(CAST(op.payload_json AS TEXT),'$.user_ids'),0),
 count(r.user_id),length(op.payload_json)+COALESCE(sum(length(r.result_json)),0)
 FROM cn_global_operation op LEFT JOIN cn_admin_action_receipt r ON r.operation_key=op.operation_key
 WHERE op.operation_key GLOB ? AND (?='' OR julianday(op.updated_utc)<julianday(?))
 GROUP BY op.operation_key ORDER BY op.operation_key`, MailBatchPrefix+"*", before, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []mailBatchSize{}
	for rows.Next() {
		var batch mailBatchSize
		if err := rows.Scan(&batch.Key, &batch.UpdatedUTC, &batch.SHA256, &batch.Recipients, &batch.Receipts, &batch.Bytes); err != nil {
			return nil, err
		}
		result = append(result, batch)
	}
	return result, rows.Err()
}

func rankCounts(ctx context.Context, tx *sql.Tx, bosses []int) ([]HistoryRankCount, error) {
	if bosses != nil {
		result := make([]HistoryRankCount, 0, len(bosses))
		for _, bossID := range bosses {
			var count int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM cn_boss_score_rank WHERE boss_id=?`, bossID).Scan(&count); err != nil {
				return nil, err
			}
			if count > 0 {
				result = append(result, HistoryRankCount{BossID: bossID, Rows: count})
			}
		}
		return result, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT boss_id,count(*) FROM cn_boss_score_rank GROUP BY boss_id ORDER BY boss_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []HistoryRankCount{}
	for rows.Next() {
		var count HistoryRankCount
		if err := rows.Scan(&count.BossID, &count.Rows); err != nil {
			return nil, err
		}
		result = append(result, count)
	}
	return result, rows.Err()
}

// CleanupHistory previews (expected == "") or applies the selected history
// cleanup in one transaction. Mail batches and leaderboard rows are backed up
// first; clear decks are rolling display data and expire exactly like the
// automatic 30-day pruning. The caller holds the maintenance gate.
func (d *Database) CleanupHistory(ctx context.Context, request HistoryCleanupRequest, expected string) (report HistoryCleanupReport, err error) {
	now := time.Now()
	if err = request.Normalize(now); err != nil {
		return report, err
	}
	report.Request = request
	report.RankRows = []HistoryRankCount{}
	db, err := d.Open()
	if err != nil {
		return report, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return report, err
	}
	defer tx.Rollback()
	if report.FileBytes, report.FreeBytes, err = databaseFileSize(ctx, tx); err != nil {
		return report, err
	}
	hash := sha256.New()
	identity, _ := json.Marshal(request)
	hash.Write(identity)

	batches := []mailBatchSize{}
	if request.MailBeforeUTC != "" {
		all, err := mailBatchSizes(ctx, tx, request.MailBeforeUTC)
		if err != nil {
			return report, err
		}
		for _, batch := range all {
			if !batch.finished() {
				continue // Unfinished batches stay resumable.
			}
			batches = append(batches, batch)
			report.MailBatches++
			report.MailReceipts += batch.Receipts
			report.MailBytes += batch.Bytes
		}
		if err := visitHistoryMail(ctx, tx, batches, json.NewEncoder(hash).Encode); err != nil {
			return report, err
		}
	}
	if request.ClearKeepDays > 0 {
		report.ClearBeforeDay = gamestate.BattleClearDay(now.Unix()) - int64(request.ClearKeepDays) + 1
		rows, err := tx.QueryContext(ctx, `SELECT day,boss_id,mode,user_id,arthur_type,clear_count,completed_at,event_key,decks_sha256,decks_json
 FROM cn_battle_clear_daily WHERE day<? ORDER BY day,boss_id,mode,user_id,arthur_type`, report.ClearBeforeDay)
		if err != nil {
			return report, err
		}
		for rows.Next() {
			var day, completed, clears int64
			var boss, mode, user, job int
			var sum, event string
			var decks []byte
			if err := rows.Scan(&day, &boss, &mode, &user, &job, &clears, &completed, &event, &sum, &decks); err != nil {
				rows.Close()
				return report, err
			}
			report.ClearRows++
			report.ClearBytes += int64(len(decks))
			actual := sha256.Sum256(decks)
			identity := []any{"clear", day, boss, mode, user, job, clears, completed, event, sum, hex.EncodeToString(actual[:])}
			if err := json.NewEncoder(hash).Encode(identity); err != nil {
				rows.Close()
				return report, err
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return report, err
		}
	}
	if len(request.RankBossIDs) > 0 {
		if report.RankRows, err = rankCounts(ctx, tx, request.RankBossIDs); err != nil {
			return report, err
		}
		if err := visitHistoryRanks(ctx, tx, report.RankRows, json.NewEncoder(hash).Encode); err != nil {
			return report, err
		}
	}
	report.Digest = hex.EncodeToString(hash.Sum(nil))
	if expected == "" {
		return report, nil
	}
	if expected != report.Digest {
		return report, errors.New("历史数据已变化，请重新预览")
	}

	stamp := now.UTC().Format(time.RFC3339Nano)
	backup, err := d.backupHistory(ctx, tx, report, batches)
	if err != nil {
		return report, err
	}
	keep := false
	defer func() {
		if !keep && backup != "" {
			_ = os.Remove(backup)
			report.Backup = ""
		}
	}()
	report.Backup = backup
	if len(batches) > 0 {
		if err = retireMailBatches(tx, batches, stamp); err != nil {
			return report, err
		}
	}
	if request.ClearKeepDays > 0 {
		if _, err = tx.ExecContext(ctx, `DELETE FROM cn_battle_clear_daily WHERE day<?`, report.ClearBeforeDay); err != nil {
			return report, err
		}
	}
	for _, count := range report.RankRows {
		if _, err = tx.ExecContext(ctx, `DELETE FROM cn_boss_score_rank WHERE boss_id=?`, count.BossID); err != nil {
			return report, err
		}
	}
	report.Applied = true
	audit := AdminAudit{Operation: "history-cleanup", Target: "history", Payload: report}
	if err = audit.appendTo(tx, stamp); err != nil {
		report.Applied = false
		return report, err
	}
	// A commit error can leave its durable outcome uncertain. Keep the already
	// synced recovery copy, just like operational and audit cleanup do.
	keep = true
	if err = tx.Commit(); err != nil {
		report.Applied = false
		return report, err
	}
	if request.Vacuum {
		// VACUUM cannot run inside a transaction. The cleanup above is already
		// committed, so a failure here leaves a consistent, just larger, file.
		if _, err = db.ExecContext(ctx, `VACUUM`); err != nil {
			return report, fmt.Errorf("历史数据已清理，但整理数据库文件失败：%w", err)
		}
		if _, err = db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
			return report, fmt.Errorf("历史数据已清理，但整理数据库文件失败：%w", err)
		}
		if report.FileBytesAfter, _, err = databaseFileSize(ctx, db); err != nil {
			return report, err
		}
	}
	return report, nil
}

// Writes the rows that cannot be rebuilt (mail batches, receipts, leaderboard)
// next to the database. Returns "" when nothing of that kind is selected.
func (d *Database) backupHistory(ctx context.Context, tx *sql.Tx, report HistoryCleanupReport, batches []mailBatchSize) (path string, err error) {
	if len(batches) == 0 && len(report.RankRows) == 0 {
		return "", nil
	}
	dir := filepath.Join(filepath.Dir(d.databasePath), "maintenance-backups")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(dir, "history-*.jsonl.gz")
	if err != nil {
		return "", err
	}
	path = file.Name()
	defer func() {
		file.Close()
		if err != nil {
			_ = os.Remove(path)
			path = ""
		}
	}()
	zipped := gzip.NewWriter(file)
	encoder := json.NewEncoder(zipped)
	if err = encoder.Encode(map[string]any{"format": "history-cleanup-v1", "created_utc": time.Now().UTC(), "report": report}); err != nil {
		return path, err
	}
	if err = visitHistoryMail(ctx, tx, batches, encoder.Encode); err != nil {
		return path, err
	}
	if err = visitHistoryRanks(ctx, tx, report.RankRows, encoder.Encode); err != nil {
		return path, err
	}
	if err = zipped.Close(); err != nil {
		return path, err
	}
	if err = file.Sync(); err != nil {
		return path, err
	}
	return path, file.Close()
}

// Deletes finished batch documents and receipts, and records their IDs so the
// same batch can never be created again.
func retireMailBatches(tx *sql.Tx, batches []mailBatchSize, stamp string) error {
	payload, err := json.Marshal(map[string]string{"retired_utc": stamp})
	if err != nil {
		return err
	}
	digest := sha256.Sum256(payload)
	for _, batch := range batches {
		if _, err := tx.Exec(`DELETE FROM cn_admin_action_receipt WHERE operation_key=?`, batch.Key); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM cn_global_operation WHERE operation_key=?`, batch.Key); err != nil {
			return err
		}
		key := RetiredMailBatchPrefix + batch.Key[len(MailBatchPrefix):]
		if _, err := tx.Exec(`INSERT INTO cn_global_operation (operation_key,revision,updated_utc,payload_json,payload_sha256) VALUES (?,1,?,?,?)`,
			key, stamp, payload, hex.EncodeToString(digest[:])); err != nil {
			return err
		}
	}
	return nil
}

// MailBatchRetired reports whether a batch ID was removed by a history cleanup.
func (d *Database) MailBatchRetired(id string) (bool, error) {
	doc, err := d.ReadDocument(RetiredMailBatchPrefix + id)
	return doc.Revision != 0, err
}
