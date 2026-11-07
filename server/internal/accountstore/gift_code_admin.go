package accountstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"kairisei.local/server/internal/game"
)

func (s *Database) GiftBatches() (GiftBatchRegistry, int, error) {
	db, err := s.OpenRead()
	if err != nil {
		return GiftBatchRegistry{}, 0, err
	}
	return readGiftRegistry(db)
}

func giftMeaning(b game.GiftCodeBatch) any {
	return struct {
		Mode, Scope, Group string
		Rewards            any
	}{b.Mode, b.ClaimScope, b.Group, b.Rewards}
}

func (s *Database) SaveGiftBatch(expected int, batch game.GiftCodeBatch) (GiftBatchRegistry, int, error) {
	db, err := s.Open()
	if err != nil {
		return GiftBatchRegistry{}, 0, err
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return GiftBatchRegistry{}, 0, err
	}
	defer tx.Rollback()
	registry, revision, err := readGiftRegistry(tx)
	if err != nil {
		return registry, revision, err
	}
	if expected != revision {
		return registry, revision, ErrDocumentConflict
	}
	index := -1
	for i, b := range registry.Batches {
		if b.ID == batch.ID {
			index = i
			break
		}
	}
	if batch.ID == 0 {
		batch.ID = registry.NextID
		registry.NextID++
	} else if index < 0 {
		return registry, revision, errors.New("兑换批次不存在")
	}
	var issued, used int
	if err = tx.QueryRow(`SELECT count(*) FROM cn_gift_code WHERE batch_id=?`, batch.ID).Scan(&issued); err != nil {
		return registry, revision, err
	}
	if err = tx.QueryRow(`SELECT count(*) FROM cn_gift_redemption WHERE batch_id=?`, batch.ID).Scan(&used); err != nil {
		return registry, revision, err
	}
	if batch.Limit > 0 && batch.Limit < used {
		return registry, revision, errors.New("总额度不能小于已领取次数")
	}
	batch.Revision = 1
	if index >= 0 {
		previous := registry.Batches[index]
		batch.Revision = previous.Revision + 1
		if issued > 0 && (!reflect.DeepEqual(giftMeaning(previous), giftMeaning(batch)) || !batch.Published) {
			return registry, revision, errors.New("已发码批次不能更改奖励、模式、限领组或退回草稿；请新建批次")
		}
		registry.Batches[index] = batch
	} else {
		registry.Batches = append(registry.Batches, batch)
	}
	raw, err := json.Marshal(registry)
	if err != nil {
		return registry, revision, err
	}
	digest := sha256.Sum256(raw)
	doc := Document{Revision: revision + 1, UpdatedUTC: time.Now().UTC().Format(time.RFC3339Nano), Payload: raw, SHA256: hex.EncodeToString(digest[:])}
	if err = writeDocumentTx(tx, DocumentWrite{Key: GiftBatchesKey, Expected: revision, Operation: "gift-batch"}, doc); err != nil {
		return registry, revision, err
	}
	return registry, doc.Revision, tx.Commit()
}

type GiftCodeGeneration struct {
	BatchID          int      `json:"batch_id"`
	ExpectedRevision int      `json:"expected_revision"`
	RequestKey       string   `json:"request_key"`
	Count            int      `json:"count"`
	Prefix           string   `json:"prefix"`
	Codes            []string `json:"codes"`
}

func (s *Database) GenerateGiftCodes(input GiftCodeGeneration) ([]int64, error) {
	if input.BatchID <= 0 || len(input.RequestKey) < 16 || len(input.RequestKey) > 100 {
		return nil, errors.New("批次或操作编号无效")
	}
	if len(input.Codes) > 0 {
		if input.Count != 0 || input.Prefix != "" || len(input.Codes) > 5000 {
			return nil, errors.New("手工码与随机生成不可混用，每次最多5000个")
		}
	} else if input.Count < 1 || input.Count > 5000 || len(input.Prefix) > 20 {
		return nil, errors.New("每次生成1–5000个，前缀最多20位")
	}
	requestBytes, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	requestSHA := sha256.Sum256(requestBytes)
	hash := hex.EncodeToString(requestSHA[:])
	db, err := s.Open()
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	key := "gift-generate:" + input.RequestKey
	var previous []byte
	var previousSHA string
	err = tx.QueryRow(`SELECT result_json,request_sha256 FROM cn_admin_action_receipt WHERE operation_key=? AND user_id=0`, key).Scan(&previous, &previousSHA)
	if err == nil {
		if hash != previousSHA {
			return nil, errors.New("操作编号已用于其他生成请求")
		}
		var ids []int64
		err = json.Unmarshal(previous, &ids)
		return ids, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	registry, revision, err := readGiftRegistry(tx)
	if err != nil {
		return nil, err
	}
	if revision != input.ExpectedRevision {
		return nil, ErrDocumentConflict
	}
	found := false
	for _, b := range registry.Batches {
		if b.ID == input.BatchID {
			found = b.Published
			break
		}
	}
	if !found {
		return nil, errors.New("请先发布批次，再生成兑换码")
	}
	codes := input.Codes
	if len(codes) == 0 {
		codes = make([]string, input.Count)
		for i := range codes {
			var random [10]byte
			if _, err = rand.Read(random[:]); err != nil {
				return nil, err
			}
			codes[i] = input.Prefix + strings.ToUpper(hex.EncodeToString(random[:]))
		}
	}
	ids := make([]int64, 0, len(codes))
	seen := map[string]bool{}
	for _, raw := range codes {
		code, err := game.NormalizeGiftCode(raw)
		if err != nil {
			return nil, err
		}
		if seen[code] {
			return nil, errors.New("输入中有重复兑换码")
		}
		seen[code] = true
		result, err := tx.Exec(`INSERT INTO cn_gift_code(batch_id,canonical,enabled) VALUES(?,?,1) ON CONFLICT(canonical) DO NOTHING`, input.BatchID, code)
		if err != nil {
			return nil, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if n != 1 {
			return nil, errors.New("兑换码已存在，整批未保存")
		}
		id, err := result.LastInsertId()
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	receipt := AdminActionReceipt{OperationKey: key, UserID: 0, RequestSHA256: hash, Result: ids}
	if err = receipt.appendTo(tx, now); err != nil {
		return nil, err
	}
	audit := &AdminAudit{Operation: "gift-generate", Target: fmt.Sprint(input.BatchID), Payload: map[string]any{"count": len(ids), "request_key": input.RequestKey}}
	if err = audit.appendTo(tx, now); err != nil {
		return nil, err
	}
	return ids, tx.Commit()
}

type GiftCodeRow struct {
	ID      int64  `json:"id"`
	Code    string `json:"code"`
	Enabled bool   `json:"enabled"`
	Used    int    `json:"used"`
}
type GiftRedemptionRow struct {
	CodeID     int64  `json:"code_id"`
	UserID     int    `json:"user_id"`
	ClaimedAt  int64  `json:"claimed_at"`
	PresentIDs string `json:"present_ids"`
	Revision   int    `json:"revision"`
}
type GiftBatchCounts struct {
	Issued int `json:"issued"`
	Used   int `json:"used"`
}

func (s *Database) GiftBatchCounts() (map[int]GiftBatchCounts, error) {
	db, err := s.OpenRead()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT batch_id,count(*),(SELECT count(*) FROM cn_gift_redemption r WHERE r.batch_id=c.batch_id) FROM cn_gift_code c GROUP BY batch_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[int]GiftBatchCounts{}
	for rows.Next() {
		var id int
		var count GiftBatchCounts
		if err = rows.Scan(&id, &count.Issued, &count.Used); err != nil {
			return nil, err
		}
		result[id] = count
	}
	return result, rows.Err()
}

func (s *Database) GiftCodes(batchID, limit, offset int) ([]GiftCodeRow, error) {
	if limit < 1 || limit > 5000 || offset < 0 {
		return nil, errors.New("页码无效")
	}
	db, err := s.OpenRead()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT c.code_id,c.canonical,c.enabled,(SELECT count(*) FROM cn_gift_redemption r WHERE r.code_id=c.code_id) FROM cn_gift_code c WHERE batch_id=? ORDER BY c.code_id LIMIT ? OFFSET ?`, batchID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []GiftCodeRow{}
	for rows.Next() {
		var row GiftCodeRow
		if err = rows.Scan(&row.ID, &row.Code, &row.Enabled, &row.Used); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func (s *Database) GiftRedemptions(batchID, limit, offset int) ([]GiftRedemptionRow, error) {
	if limit < 1 || limit > 500 || offset < 0 {
		return nil, errors.New("页码无效")
	}
	db, err := s.OpenRead()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT code_id,user_id,claimed_at,present_ids,reward_revision FROM cn_gift_redemption WHERE batch_id=? ORDER BY claimed_at DESC,code_id,user_id LIMIT ? OFFSET ?`, batchID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []GiftRedemptionRow{}
	for rows.Next() {
		var row GiftRedemptionRow
		if err = rows.Scan(&row.CodeID, &row.UserID, &row.ClaimedAt, &row.PresentIDs, &row.Revision); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func (s *Database) SetGiftCodeEnabled(id int64, expected, enabled bool) error {
	db, err := s.Open()
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE cn_gift_code SET enabled=? WHERE code_id=? AND enabled=?`, enabled, id, expected)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrDocumentConflict
	}
	audit := &AdminAudit{Operation: "gift-code-state", Target: fmt.Sprint(id), Payload: map[string]bool{"enabled": enabled}}
	if err = audit.appendTo(tx, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}
