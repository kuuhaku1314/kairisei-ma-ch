package accountstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// Preview and backup must visit the same complete rows. Counts alone cannot
// detect an updated score or a replaced delivery receipt between requests.
func visitHistoryMail(ctx context.Context, tx *sql.Tx, batches []mailBatchSize, write func(any) error) error {
	for _, batch := range batches {
		var doc struct {
			Key        string          `json:"operation_key"`
			Revision   int             `json:"revision"`
			UpdatedUTC string          `json:"updated_utc"`
			Payload    json.RawMessage `json:"payload"`
			SHA256     string          `json:"payload_sha256"`
		}
		var payload []byte
		if err := tx.QueryRowContext(ctx, `SELECT operation_key,revision,updated_utc,payload_json,payload_sha256 FROM cn_global_operation WHERE operation_key=?`, batch.Key).
			Scan(&doc.Key, &doc.Revision, &doc.UpdatedUTC, &payload, &doc.SHA256); err != nil {
			return err
		}
		sum := sha256.Sum256(payload)
		if hex.EncodeToString(sum[:]) != doc.SHA256 {
			return errors.New("礼物批次内容哈希不一致，已拒绝清理")
		}
		var recipients struct {
			IDs []int `json:"user_ids"`
		}
		if err := json.Unmarshal(payload, &recipients); err != nil {
			return err
		}
		pending := make(map[int]bool, len(recipients.IDs))
		for _, id := range recipients.IDs {
			if id <= 0 || pending[id] {
				return errors.New("礼物批次收件人无效，已拒绝清理")
			}
			pending[id] = true
		}
		doc.Payload = payload
		if err := write(map[string]any{"mail_batch": doc}); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT user_id,request_sha256,result_json,result_sha256,created_utc FROM cn_admin_action_receipt WHERE operation_key=? ORDER BY user_id`, batch.Key)
		if err != nil {
			return err
		}
		for rows.Next() {
			var receipt struct {
				Key        string          `json:"operation_key"`
				UserID     int             `json:"user_id"`
				RequestSHA string          `json:"request_sha256"`
				Result     json.RawMessage `json:"result"`
				ResultSHA  string          `json:"result_sha256"`
				CreatedUTC string          `json:"created_utc"`
			}
			var result []byte
			if err := rows.Scan(&receipt.UserID, &receipt.RequestSHA, &result, &receipt.ResultSHA, &receipt.CreatedUTC); err != nil {
				rows.Close()
				return err
			}
			sum := sha256.Sum256(result)
			if hex.EncodeToString(sum[:]) != receipt.ResultSHA || !pending[receipt.UserID] {
				rows.Close()
				return errors.New("礼物发放回执哈希或收件人不一致，已拒绝清理")
			}
			delete(pending, receipt.UserID)
			receipt.Key, receipt.Result = batch.Key, result
			if err := write(map[string]any{"mail_receipt": receipt}); err != nil {
				rows.Close()
				return err
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(pending) != 0 {
			return errors.New("礼物批次尚有收件人没有发放回执，已拒绝清理")
		}
	}
	return nil
}

func visitHistoryRanks(ctx context.Context, tx *sql.Tx, bosses []HistoryRankCount, write func(any) error) error {
	for _, boss := range bosses {
		rows, err := tx.QueryContext(ctx, `SELECT boss_id,mode,user_id,best_score,best_grade,best_at_unix FROM cn_boss_score_rank WHERE boss_id=? ORDER BY mode,user_id`, boss.BossID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var row struct {
				BossID int   `json:"boss_id"`
				Mode   int   `json:"mode"`
				UserID int   `json:"user_id"`
				Score  int64 `json:"best_score"`
				Grade  int   `json:"best_grade"`
				AtUnix int64 `json:"best_at_unix"`
			}
			if err := rows.Scan(&row.BossID, &row.Mode, &row.UserID, &row.Score, &row.Grade, &row.AtUnix); err != nil {
				rows.Close()
				return err
			}
			if err := write(map[string]any{"boss_score_rank": row}); err != nil {
				rows.Close()
				return err
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
