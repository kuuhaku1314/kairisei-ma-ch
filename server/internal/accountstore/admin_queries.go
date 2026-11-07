package accountstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Document struct {
	Revision   int             `json:"revision"`
	UpdatedUTC string          `json:"updated_utc,omitempty"`
	SHA256     string          `json:"sha256"`
	Payload    json.RawMessage `json:"payload"`
}

var ErrDocumentConflict = errors.New("配置已被其他页面修改，请重新载入后预览")

func (storage *Database) ReadDocument(key string) (Document, error) {
	db, err := storage.OpenRead()
	if err != nil {
		return Document{}, err
	}
	var doc Document
	err = db.QueryRow(`SELECT revision, updated_utc, payload_json, payload_sha256 FROM cn_global_operation WHERE operation_key = ?`, key).Scan(&doc.Revision, &doc.UpdatedUTC, &doc.Payload, &doc.SHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return doc, nil
	}
	if err != nil {
		return doc, err
	}
	digest := sha256.Sum256(doc.Payload)
	if hex.EncodeToString(digest[:]) != doc.SHA256 {
		return doc, errors.New("operations document digest mismatch")
	}
	return doc, nil
}

// The revision guard and audit entry commit together. A stale browser cannot
// overwrite an operator's newer draft or publication.
func (storage *Database) WriteDocument(key string, expected int, value any, operation string) (Document, error) {
	docs, err := storage.WriteDocuments([]DocumentWrite{{Key: key, Expected: expected, Value: value, Operation: operation}})
	if err != nil {
		return Document{}, err
	}
	return docs[0], nil
}

type DocumentWrite struct {
	Key       string
	Expected  int
	Value     any
	Operation string
}

// WriteDocuments commits a related configuration change and every audit entry
// in one transaction. Any stale revision or write failure rolls back the batch.
func (storage *Database) WriteDocuments(writes []DocumentWrite) ([]Document, error) {
	if len(writes) == 0 {
		return nil, errors.New("empty document batch")
	}
	docs := make([]Document, len(writes))
	seen := make(map[string]bool, len(writes))
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	for i, write := range writes {
		if write.Expected < 0 {
			return nil, ErrDocumentConflict
		}
		if write.Key == "" || seen[write.Key] {
			return nil, errors.New("empty or duplicate document key")
		}
		seen[write.Key] = true
		body, err := json.Marshal(write.Value)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(body)
		docs[i] = Document{Revision: write.Expected + 1, UpdatedUTC: stamp, SHA256: hex.EncodeToString(digest[:]), Payload: body}
	}
	db, err := storage.Open()
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for i, write := range writes {
		if err := writeDocumentTx(tx, write, docs[i]); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return docs, nil
}

func writeDocumentTx(tx *sql.Tx, write DocumentWrite, doc Document) error {
	key, expected, body, operation := write.Key, write.Expected, doc.Payload, write.Operation
	var result sql.Result
	var err error
	if expected == 0 {
		result, err = tx.Exec(`INSERT INTO cn_global_operation (operation_key, revision, updated_utc, payload_json, payload_sha256) VALUES (?, 1, ?, ?, ?) ON CONFLICT(operation_key) DO NOTHING`, key, doc.UpdatedUTC, body, doc.SHA256)
	} else {
		result, err = tx.Exec(`UPDATE cn_global_operation SET revision=revision+1, updated_utc=?, payload_json=?, payload_sha256=? WHERE operation_key=? AND revision=?`, doc.UpdatedUTC, body, doc.SHA256, key, expected)
	}
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrDocumentConflict
	}
	if _, err = tx.Exec(`INSERT INTO cn_admin_audit (created_utc, operation, target, payload_json, payload_sha256) VALUES (?, ?, ?, ?, ?)`, doc.UpdatedUTC, operation, key, body, doc.SHA256); err != nil {
		return err
	}
	return nil
}

func (storage *Database) ActionReceipt(key string, userID int, requestSHA string) (json.RawMessage, error) {
	db, err := storage.OpenRead()
	if err != nil {
		return nil, err
	}
	var content []byte
	var recordedRequest, recordedResult string
	err = db.QueryRow(`SELECT request_sha256,result_json,result_sha256 FROM cn_admin_action_receipt WHERE operation_key=? AND user_id=?`, key, userID).Scan(&recordedRequest, &content, &recordedResult)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(content)
	if recordedRequest != requestSHA || hex.EncodeToString(digest[:]) != recordedResult {
		return nil, errors.New("操作编号已对应其他内容，或发放收据不完整")
	}
	return json.RawMessage(content), nil
}

// List summaries read the compact projection, not full inventory snapshots.
type AccountListItem struct {
	LoginBlock   LoginBlock `json:"login_block"`
	Level        int        `json:"level"`
	ArthurType   int        `json:"arthur_type"`
	Gold         int        `json:"gold"`
	Crystals     int        `json:"crystals"`
	Name         string     `json:"name"`
	Username     string     `json:"username"`
	UserID       int        `json:"user_id"`
	LoginUUID    string     `json:"login_uuid"`
	CreatedUTC   string     `json:"created_utc"`
	LastLoginUTC string     `json:"last_login_utc"`
	Revision     int        `json:"revision"`
	UpdatedUTC   string     `json:"updated_utc"`
}

func (accounts *Accounts) QueryAccounts(search string, includeSystem bool, limit, offset int, ids ...int) ([]AccountListItem, int, error) {
	return accounts.QueryFilteredAccounts(AccountFilter{Search: search, IncludeSystem: includeSystem}, limit, offset, ids...)
}

type AccountFilter struct {
	Search        string
	IncludeSystem bool
	Binding       string
	CreatedAfter  string
	CreatedBefore string
	LoginAfter    string
	LoginBefore   string
	Sort          string
	ActiveOnly    bool
	ActiveIDs     []int
	LoginBlock    string
	// Optional projection filters; zero means unrestricted.
	LevelMin   int
	LevelMax   int
	ArthurType int
}

func (accounts *Accounts) QueryFilteredAccounts(filter AccountFilter, limit, offset int, ids ...int) ([]AccountListItem, int, error) {
	db, err := accounts.storage.OpenRead()
	if err != nil {
		return nil, 0, err
	}
	// The compact public projection has the display name; never read inventory
	// snapshots to render a list. SQLite parameters keep searches literal.
	from := ` FROM cn_local_account a
		LEFT JOIN cn_account_credentials c ON c.user_id=a.user_id
		LEFT JOIN cn_account_projection p ON p.user_id=a.user_id
		LEFT JOIN cn_save_snapshot s ON s.singleton=1 AND a.user_id=?
		LEFT JOIN cn_account_snapshot x ON x.user_id=a.user_id`
	name := `COALESCE(json_extract(CAST(p.payload_json AS TEXT),'$.user.name'),'')`
	level := `COALESCE(json_extract(CAST(p.payload_json AS TEXT),'$.user.level'),0)`
	job := `COALESCE(json_extract(CAST(p.payload_json AS TEXT),'$.user.active_arthur_type'),0)`
	where := ` WHERE (? OR a.user_id<?)`
	args := []any{PrimaryUserID, filter.IncludeSystem, SystemPartnerUserIDBase}
	now := time.Now().Unix()
	switch filter.LoginBlock {
	case "blocked":
		where += ` AND (a.login_block_until_unix = 0 OR a.login_block_until_unix > ?)`
		args = append(args, now)
	case "unblocked":
		where += ` AND (a.login_block_until_unix IS NULL OR a.login_block_until_unix BETWEEN 1 AND ?)`
		args = append(args, now)
	}
	for _, q := range strings.Fields(strings.ToLower(filter.Search)) {
		where += ` AND instr(lower(CAST(a.user_id AS TEXT)||' '||a.login_uuid||' '||COALESCE(c.username,'')||' '||` + name + `),?)>0`
		args = append(args, q)
	}
	switch filter.Binding {
	case "bound":
		where += ` AND c.user_id IS NOT NULL AND a.user_id<?`
		args = append(args, SystemPartnerUserIDBase)
	case "guest":
		where += ` AND c.user_id IS NULL AND a.user_id<?`
		args = append(args, SystemPartnerUserIDBase)
	case "system":
		where += ` AND a.user_id>=?`
		args = append(args, SystemPartnerUserIDBase)
	}
	for _, condition := range []struct{ column, op, value string }{
		{"a.created_utc", ">=", filter.CreatedAfter}, {"a.created_utc", "<", filter.CreatedBefore},
		{"a.last_login_utc", ">=", filter.LoginAfter}, {"a.last_login_utc", "<", filter.LoginBefore},
	} {
		if condition.value != "" {
			where += ` AND julianday(` + condition.column + `)` + condition.op + `julianday(?)`
			args = append(args, condition.value)
		}
	}
	if filter.LevelMin > 0 {
		where += ` AND ` + level + `>=?`
		args = append(args, filter.LevelMin)
	}
	if filter.LevelMax > 0 {
		where += ` AND ` + level + `<=?`
		args = append(args, filter.LevelMax)
	}
	if filter.ArthurType > 0 {
		where += ` AND ` + job + `=?`
		args = append(args, filter.ArthurType)
	}
	if filter.ActiveOnly {
		if len(filter.ActiveIDs) == 0 {
			where += ` AND 0`
		} else {
			where += ` AND a.user_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(filter.ActiveIDs)), ",") + `)`
			for _, id := range filter.ActiveIDs {
				args = append(args, id)
			}
		}
	}
	if len(ids) > 0 {
		where += ` AND a.user_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + `)`
		for _, id := range ids {
			args = append(args, id)
		}
	}
	var total int
	if err := db.QueryRow(`SELECT count(*)`+from+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := "a.user_id"
	switch filter.Sort {
	case "created":
		order = "a.created_utc DESC,a.user_id DESC"
	case "login":
		order = "a.last_login_utc DESC,a.user_id DESC"
	case "level":
		order = level + " DESC,a.user_id"
	}
	query := `SELECT a.user_id,a.login_uuid,COALESCE(c.username,''),` + name + `,a.created_utc,a.last_login_utc,COALESCE(s.revision,x.revision,0),COALESCE(s.updated_utc,x.updated_utc,''),
		` + level + `,
		` + job + `,
		COALESCE(json_extract(CAST(p.payload_json AS TEXT),'$.user.gold'),0),
		COALESCE(json_extract(CAST(p.payload_json AS TEXT),'$.user.coin'),0)+COALESCE(json_extract(CAST(p.payload_json AS TEXT),'$.user.coin_free'),0),
		a.login_block_until_unix` + from + where + ` ORDER BY ` + order
	if limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, limit, offset)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := []AccountListItem{}
	for rows.Next() {
		var a AccountListItem
		var until sql.NullInt64
		if err := rows.Scan(&a.UserID, &a.LoginUUID, &a.Username, &a.Name, &a.CreatedUTC, &a.LastLoginUTC, &a.Revision, &a.UpdatedUTC, &a.Level, &a.ArthurType, &a.Gold, &a.Crystals, &until); err != nil {
			return nil, 0, err
		}
		a.LoginBlock = loginBlockAt(until, now)
		result = append(result, a)
	}
	return result, total, rows.Err()
}

func (accounts *Accounts) Count() (int, error) {
	database, err := accounts.storage.OpenRead()
	if err != nil {
		return 0, err
	}
	var count int
	if err := database.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM cn_local_account`,
	).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (accounts *Accounts) SnapshotMetadata(userID int) (int, string, error) {
	database, err := accounts.storage.OpenRead()
	if err != nil {
		return 0, "", err
	}
	query := `SELECT revision, updated_utc FROM cn_account_snapshot WHERE user_id = ?`
	arguments := []any{userID}
	if userID == PrimaryUserID {
		query = `SELECT revision, updated_utc FROM cn_save_snapshot WHERE singleton = 1`
		arguments = nil
	}
	var revision int
	var updated string
	if err := database.QueryRow(query, arguments...).Scan(&revision, &updated); err != nil {
		return 0, "", err
	}
	return revision, updated, nil
}

type LoginMetadata struct{ LoginUUID, CreatedUTC, LastLoginUTC string }

func (accounts *Accounts) LoginMetadata(userID int) (LoginMetadata, error) {
	db, err := accounts.storage.OpenRead()
	if err != nil {
		return LoginMetadata{}, err
	}
	var result LoginMetadata
	err = db.QueryRowContext(context.Background(), `SELECT login_uuid, created_utc, last_login_utc FROM cn_local_account WHERE user_id = ?`, userID).Scan(&result.LoginUUID, &result.CreatedUTC, &result.LastLoginUTC)
	return result, err
}

// PlayerIDs reads actual players within an inclusive range, without decoding saves.
func (accounts *Accounts) PlayerIDs(ctx context.Context, startUID, endUID int) ([]int, error) {
	if startUID < PrimaryUserID || endUID >= SystemPartnerUserIDBase || startUID > endUID {
		return nil, errors.New("player ID range is outside the real-player namespace")
	}
	db, err := accounts.storage.OpenRead()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT user_id FROM cn_local_account WHERE user_id >= ? AND user_id <= ? ORDER BY user_id`, startUID, endUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (accounts *Accounts) CountIDs(ids []int) (int, error) {
	db, err := accounts.storage.OpenRead()
	if err != nil {
		return 0, err
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	var count int
	err = db.QueryRow(`SELECT count(*) FROM cn_local_account WHERE user_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`)`, args...).Scan(&count)
	return count, err
}

type AuditRecord struct {
	AuditID    int             `json:"audit_id"`
	CreatedUTC string          `json:"created_utc"`
	Operation  string          `json:"operation"`
	Target     string          `json:"target"`
	Payload    json.RawMessage `json:"payload"`
	SHA256     string          `json:"sha256"`
}

// AuditFilter narrows the admin audit list; empty fields are unrestricted.
// After/Before are RFC3339 UTC bounds on created_utc (inclusive / exclusive).
type AuditFilter struct {
	Search    string
	Operation string
	After     string
	Before    string
}

func (accounts *Accounts) AuditRecords(q, op string, limit, offset int) ([]AuditRecord, int, error) {
	return accounts.QueryAuditRecords(AuditFilter{Search: q, Operation: op}, limit, offset)
}

func (accounts *Accounts) QueryAuditRecords(filter AuditFilter, limit, offset int) ([]AuditRecord, int, error) {
	database, err := accounts.storage.OpenRead()
	if err != nil {
		return nil, 0, err
	}
	q, op := filter.Search, filter.Operation
	where := ` WHERE (?='' OR operation=?) AND (?='' OR instr(lower(target||' '||operation||' '||CAST(payload_json AS TEXT)),lower(?))>0)` +
		` AND (?='' OR julianday(created_utc)>=julianday(?)) AND (?='' OR julianday(created_utc)<julianday(?))`
	args := []any{op, op, q, q, filter.After, filter.After, filter.Before, filter.Before}
	var total int
	if err := database.QueryRow(`SELECT count(*) FROM cn_admin_audit`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := database.Query(
		`SELECT audit_id, created_utc, operation, target, payload_json, payload_sha256
		 FROM cn_admin_audit`+where+` ORDER BY audit_id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var records []AuditRecord
	for rows.Next() {
		var item AuditRecord
		var content []byte
		if err := rows.Scan(&item.AuditID, &item.CreatedUTC, &item.Operation, &item.Target, &content, &item.SHA256); err != nil {
			return nil, 0, err
		}
		digest := sha256.Sum256(content)
		if hex.EncodeToString(digest[:]) != item.SHA256 {
			return nil, 0, errors.New("admin audit digest mismatch")
		}
		item.Payload = append(json.RawMessage(nil), content...)
		records = append(records, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if records == nil {
		records = []AuditRecord{}
	}
	return records, total, nil
}

func (storage *Database) DeliveredUsers(operationKey, requestSHA string) (map[int]bool, error) {
	db, err := storage.OpenRead()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT user_id,request_sha256 FROM cn_admin_action_receipt WHERE operation_key=?`, operationKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[int]bool{}
	for rows.Next() {
		var id int
		var sha string
		if err := rows.Scan(&id, &sha); err != nil {
			return nil, err
		}
		if sha != requestSHA {
			return nil, errors.New("批次收据与请求不符")
		}
		seen[id] = true
	}
	return seen, rows.Err()
}

type DocumentSummary struct {
	Key, UpdatedUTC string
	Payload         json.RawMessage
	ReceiptCount    int
}

// ListBatchDocuments lists batch documents whose payload carries "title" and "user_ids".
// search matches the key or title literally; unfinished keeps batches with fewer receipts than recipients.
func (storage *Database) ListBatchDocuments(prefix, search string, unfinished bool, limit, offset int) ([]DocumentSummary, int, error) {
	db, err := storage.OpenRead()
	if err != nil {
		return nil, 0, err
	}
	receipts := `(SELECT count(*) FROM cn_admin_action_receipt WHERE operation_key=op.operation_key)`
	where := ` WHERE operation_key LIKE ?`
	args := []any{prefix + "%"}
	if search = strings.TrimSpace(search); search != "" {
		where += ` AND instr(lower(operation_key||' '||COALESCE(json_extract(CAST(payload_json AS TEXT),'$.title'),'')),lower(?))>0`
		args = append(args, search)
	}
	if unfinished {
		where += ` AND ` + receipts + `<COALESCE(json_array_length(CAST(payload_json AS TEXT),'$.user_ids'),0)`
	}
	var total int
	if err := db.QueryRow(`SELECT count(*) FROM cn_global_operation op`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := db.Query(`SELECT operation_key,updated_utc,payload_json,`+receipts+`
  FROM cn_global_operation op`+where+` ORDER BY updated_utc DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := []DocumentSummary{}
	for rows.Next() {
		var doc DocumentSummary
		if err := rows.Scan(&doc.Key, &doc.UpdatedUTC, &doc.Payload, &doc.ReceiptCount); err != nil {
			return nil, 0, err
		}
		result = append(result, doc)
	}
	return result, total, rows.Err()
}
