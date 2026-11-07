package accountstore

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Authenticate the existing binding and replace only its password credential.
// The authenticated hash is the write precondition, so a concurrent password
// change, admin reset or unbind cannot be overwritten using stale authority.
func (accounts *Accounts) ChangeAccountPassword(ctx context.Context, uuid, name, password, newPassword string) (AccountBinding, error) {
	uuid = strings.ToLower(strings.TrimSpace(uuid))
	if !validLoginUUID(uuid) {
		return AccountBinding{}, errAccountCredentials
	}
	identity, expected, err := accounts.authenticateAccount(name, password)
	if err != nil {
		return AccountBinding{}, err
	}
	if identity.LoginUUID != uuid {
		return AccountBinding{}, errAccountCredentials
	}
	salt, hash, err := PasswordHash(newPassword)
	if err != nil {
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
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := tx.ExecContext(ctx, `UPDATE cn_account_credentials
		SET password_salt = ?, password_hash = ?, iterations = ?, updated_utc = ?
		WHERE user_id = ? AND username = ? AND password_hash = ?`,
		salt, hash, passwordIterations, now, identity.UserID, identity.Username, expected)
	if err != nil {
		return AccountBinding{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return AccountBinding{}, err
	}
	if changed != 1 {
		return AccountBinding{}, errAccountCredentials
	}
	if err := (&AdminAudit{Operation: "account-password-change", Target: fmt.Sprint(identity.UserID),
		Payload: map[string]any{"username": identity.Username}}).appendTo(tx, now); err != nil {
		return AccountBinding{}, err
	}
	if err := tx.Commit(); err != nil {
		return AccountBinding{}, err
	}
	return identity, nil
}
