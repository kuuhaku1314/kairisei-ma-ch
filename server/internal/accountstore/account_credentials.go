package accountstore

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const passwordIterations = 600000

var errAccountCredentials = errors.New("账号或密码不正确")

func NormalizeUsername(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if len(name) < 3 || len(name) > 32 {
		return "", errors.New("账号须为 3–32 位字母、数字或下划线")
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return "", errors.New("账号须为 3–32 位字母、数字或下划线")
		}
	}
	return name, nil
}

func PasswordHash(password string) ([]byte, []byte, error) {
	if len(password) < 8 || len(password) > 128 {
		return nil, nil, errors.New("密码须为 8–128 字节")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, nil, err
	}
	hash, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, 32)
	return salt, hash, err
}

type AccountBinding struct {
	UserID    int    `json:"user_id"`
	Username  string `json:"username"`
	LoginUUID string `json:"uuid,omitempty"`
}

func (accounts *Accounts) AccountBinding(uuid string) (AccountBinding, error) {
	uuid = strings.ToLower(strings.TrimSpace(uuid))
	if !validLoginUUID(uuid) {
		return AccountBinding{}, errors.New("本机登录凭据无效")
	}
	db, err := accounts.storage.OpenRead()
	if err != nil {
		return AccountBinding{}, err
	}
	var result AccountBinding
	err = db.QueryRow(`SELECT a.user_id, COALESCE(c.username, '') FROM cn_local_account a
		LEFT JOIN cn_account_credentials c ON c.user_id = a.user_id WHERE a.login_uuid = ? AND a.user_id < ?`, uuid, SystemPartnerUserIDBase).Scan(&result.UserID, &result.Username)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	return result, err
}

// BindInternalAccount is trusted provisioning, never a client route entry.
func (accounts *Accounts) BindInternalAccount(uuid, name, password string) (AccountBinding, error) {
	return accounts.bindAccount(context.Background(), uuid, name, password, nil)
}

// Binding requires possession of the random UUID used by guest login. Hashing
// takes place after the creation precheck, outside account and database locks.
func (accounts *Accounts) bindAccount(ctx context.Context, uuid, name, password string, admission *creationAdmission) (AccountBinding, error) {
	if err := ctx.Err(); err != nil {
		return AccountBinding{}, err
	}
	name, err := NormalizeUsername(name)
	if err != nil {
		return AccountBinding{}, err
	}
	uuid = strings.ToLower(strings.TrimSpace(uuid))
	if !validLoginUUID(uuid) {
		return AccountBinding{}, errors.New("本机登录凭据无效")
	}
	if len(password) < 8 || len(password) > 128 {
		return AccountBinding{}, errors.New("密码须为 8–128 字节")
	}
	if admission != nil {
		if err := accounts.precheckCreation(ctx, uuid, admission); err != nil {
			return AccountBinding{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return AccountBinding{}, err
	}
	salt, hash, err := PasswordHash(password)
	if err != nil {
		return AccountBinding{}, err
	}
	if err := ctx.Err(); err != nil {
		return AccountBinding{}, err
	}
	accounts.mu.Lock()
	defer accounts.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return AccountBinding{}, err
	}
	db, err := accounts.storage.Open()
	if err != nil {
		return AccountBinding{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return AccountBinding{}, err
	}
	defer tx.Rollback()
	identity, err := accounts.resolveLoginTransaction(ctx, tx, uuid, false, admission)
	if err != nil {
		return AccountBinding{}, err
	}
	if err := writeCredentialsTransaction(tx, identity.UserID, name, salt, hash, false); err != nil {
		return AccountBinding{}, err
	}
	if err := ctx.Err(); err != nil {
		return AccountBinding{}, err
	}
	if err := tx.Commit(); err != nil {
		return AccountBinding{}, err
	}
	return AccountBinding{UserID: identity.UserID, Username: name, LoginUUID: identity.LoginUUID}, nil
}

func (accounts *Accounts) WriteCredentials(userID int, name string, salt, hash []byte, admin bool) error {
	if userID < PrimaryUserID || userID >= SystemPartnerUserIDBase {
		return errors.New("不能绑定系统伙伴")
	}
	accounts.mu.Lock()
	defer accounts.mu.Unlock()
	db, err := accounts.storage.Open()
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := writeCredentialsTransaction(tx, userID, name, salt, hash, admin); err != nil {
		return err
	}
	return tx.Commit()
}

// Only the loopback Admin exposes unbinding. Keep the character, UUID and save;
// match the displayed username so a stale page cannot remove a newer binding.
func (accounts *Accounts) UnbindAccount(userID int, name string) error {
	if userID < PrimaryUserID || userID >= SystemPartnerUserIDBase {
		return errors.New("请选择玩家账号")
	}
	accounts.mu.Lock()
	defer accounts.mu.Unlock()
	db, err := accounts.storage.Open()
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`DELETE FROM cn_account_credentials WHERE user_id = ? AND username = ?`, userID, name)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return errors.New("绑定已变化或已解除，请刷新账号列表")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := (&AdminAudit{Operation: "account-unbind", Target: fmt.Sprint(userID), Payload: map[string]any{"username": name}}).appendTo(tx, now); err != nil {
		return err
	}
	return tx.Commit()
}

func writeCredentialsTransaction(tx *sql.Tx, userID int, name string, salt, hash []byte, admin bool) error {
	if userID < PrimaryUserID || userID >= SystemPartnerUserIDBase {
		return errors.New("不能绑定系统伙伴")
	}
	var existingID int
	err := tx.QueryRow(`SELECT user_id FROM cn_account_credentials WHERE username = ?`, name).Scan(&existingID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && existingID != userID {
		return errors.New("该账号名已使用")
	}
	var existingName string
	err = tx.QueryRow(`SELECT username FROM cn_account_credentials WHERE user_id = ?`, userID).Scan(&existingName)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && !admin {
		return errors.New("当前角色已经绑定账号，请使用账号登录或联系管理员重置密码")
	}
	if err == nil && existingName != name {
		return errors.New("已绑定的账号名不可修改")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.Exec(`INSERT INTO cn_account_credentials(user_id, username, password_salt, password_hash, iterations, updated_utc)
		VALUES(?, ?, ?, ?, ?, ?) ON CONFLICT(user_id) DO UPDATE SET
		password_salt = excluded.password_salt, password_hash = excluded.password_hash, updated_utc = excluded.updated_utc`, userID, name, salt, hash, passwordIterations, now)
	if err != nil {
		return fmt.Errorf("保存账号绑定失败: %w", err)
	}
	op := "account-bind"
	if admin {
		op = "account-credentials-admin"
	}
	if err := (&AdminAudit{Operation: op, Target: fmt.Sprint(userID), Payload: map[string]any{"username": name}}).appendTo(tx, now); err != nil {
		return err
	}
	return nil
}

func (accounts *Accounts) LoginAccount(name, password string) (AccountBinding, error) {
	identity, _, err := accounts.authenticateAccount(name, password)
	if err != nil {
		return AccountBinding{}, err
	}
	db, err := accounts.storage.OpenRead()
	if err != nil {
		return AccountBinding{}, err
	}
	var until sql.NullInt64
	err = db.QueryRow(`SELECT login_block_until_unix FROM cn_local_account WHERE user_id = ?`, identity.UserID).Scan(&until)
	if err != nil {
		return AccountBinding{}, err
	}
	if loginBlockActive(until, time.Now().Unix()) {
		return AccountBinding{}, &LoginBlockedError{UntilUnix: until.Int64}
	}
	if guard := accounts.creation.Load(); guard != nil && !guard.policy().LoginWhitelist.Allows(identity.UserID) {
		return AccountBinding{}, ErrLoginWhitelist
	}
	return identity, nil
}

func (accounts *Accounts) authenticateAccount(name, password string) (AccountBinding, []byte, error) {
	name, err := NormalizeUsername(name)
	if err != nil || len(password) < 8 || len(password) > 128 {
		return AccountBinding{}, nil, errAccountCredentials
	}
	db, err := accounts.storage.OpenRead()
	if err != nil {
		return AccountBinding{}, nil, err
	}
	var result AccountBinding
	var salt, expected []byte
	var iterations int
	err = db.QueryRow(`SELECT a.user_id, c.username, a.login_uuid, c.password_salt, c.password_hash, c.iterations
		FROM cn_account_credentials c JOIN cn_local_account a ON a.user_id = c.user_id WHERE c.username = ?`, name).
		Scan(&result.UserID, &result.Username, &result.LoginUUID, &salt, &expected, &iterations)
	missing := errors.Is(err, sql.ErrNoRows)
	if err != nil && !missing {
		return AccountBinding{}, nil, err
	}
	if missing {
		salt, expected, iterations = make([]byte, 16), make([]byte, 32), passwordIterations
	}
	if iterations != passwordIterations || len(salt) != 16 || len(expected) != 32 {
		return AccountBinding{}, nil, errAccountCredentials
	}
	actual, err := pbkdf2.Key(sha256.New, password, salt, iterations, 32)
	if err != nil {
		return AccountBinding{}, nil, err
	}
	if subtle.ConstantTimeCompare(actual, expected) != 1 || missing {
		return AccountBinding{}, nil, errAccountCredentials
	}
	return result, expected, nil
}
