package accountstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
)

var (
	ErrLoginBlockAccountNotFound = errors.New("玩家账号不存在")
	ErrLoginBlockExpiry          = errors.New("封禁到期时间须为未来的 Unix 秒数，0 表示永久")
)

// LoginBlock is computed from identity metadata at the time of an Admin read.
type LoginBlock struct {
	Active           bool   `json:"active"`
	UntilUnix        *int64 `json:"until_unix"`
	RemainingSeconds int64  `json:"remaining_seconds"`
}

type LoginBlockedError struct{ UntilUnix int64 }

func (e *LoginBlockedError) Error() string {
	if e.UntilUnix == 0 {
		return "该账号已被永久封禁"
	}
	return "该账号已被封禁，解封时间：" +
		time.Unix(e.UntilUnix, 0).In(time.FixedZone("UTC+8", 8*60*60)).Format("2006-01-02 15:04:05 UTC+8")
}

func loginBlockActive(until sql.NullInt64, now int64) bool {
	return until.Valid && (until.Int64 == 0 || until.Int64 > now)
}

func loginBlockAt(until sql.NullInt64, now int64) LoginBlock {
	if !until.Valid {
		return LoginBlock{}
	}
	block := LoginBlock{Active: loginBlockActive(until, now), UntilUnix: &until.Int64}
	if until.Int64 == 0 {
		block.RemainingSeconds = -1
	} else if block.Active {
		block.RemainingSeconds = until.Int64 - now
	}
	return block
}

// SetLoginBlock changes identity metadata and revokes the current session in
// the same transaction as the audit. A nil deadline clears the block.
func (accounts *Accounts) SetLoginBlock(ctx context.Context, userID int, until *int64) (LoginBlock, error) {
	if userID < PrimaryUserID || userID >= SystemPartnerUserIDBase {
		return LoginBlock{}, ErrLoginBlockAccountNotFound
	}
	db, err := accounts.storage.Open()
	if err != nil {
		return LoginBlock{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return LoginBlock{}, err
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	operation := "account-login-recover"
	var result sql.Result
	var value sql.NullInt64
	if until != nil {
		if *until != 0 && (*until <= now.Unix() || *until > 253402300799) {
			return LoginBlock{}, ErrLoginBlockExpiry
		}
		value = sql.NullInt64{Int64: *until, Valid: true}
		operation = "account-login-block"
		result, err = tx.ExecContext(ctx, `UPDATE cn_local_account
			SET login_block_until_unix = ?, session_key = 'expired:' || user_id WHERE user_id = ?`, value, userID)
	} else {
		result, err = tx.ExecContext(ctx, `UPDATE cn_local_account SET login_block_until_unix = NULL WHERE user_id = ?`, userID)
	}
	if err != nil {
		return LoginBlock{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return LoginBlock{}, err
	}
	if count == 0 {
		return LoginBlock{}, ErrLoginBlockAccountNotFound
	}
	block := loginBlockAt(value, now.Unix())
	audit := AdminAudit{Operation: operation, Target: strconv.Itoa(userID), Payload: block}
	if err := audit.appendTo(tx, now.Format(time.RFC3339Nano)); err != nil {
		return LoginBlock{}, fmt.Errorf("write login block audit: %w", err)
	}
	return block, tx.Commit()
}
