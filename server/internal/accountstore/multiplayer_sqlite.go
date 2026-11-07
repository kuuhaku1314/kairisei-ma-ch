package accountstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/multiplayer"
)

const maxMultiplayerCompletionBytes = 512 * 1024

func (accounts *Accounts) NextRoomID(minimum int64) (int64, error) {
	if minimum <= 0 {
		return 0, errors.New("minimum multiplayer room ID is invalid")
	}
	// The single-writer pool/IMMEDIATE transaction serializes this durable
	// sequence. It does not depend on mutable account or login state.
	transaction, err := accounts.beginMultiplayerTransaction()
	if err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
	}()
	if _, err := transaction.ExecContext(
		context.Background(),
		`INSERT OR IGNORE INTO cn_multiplayer_runtime (singleton, next_room_id) VALUES (1, ?)`,
		minimum,
	); err != nil {
		return 0, fmt.Errorf("initialize multiplayer room sequence: %w", err)
	}
	if _, err := transaction.ExecContext(
		context.Background(),
		`UPDATE cn_multiplayer_runtime SET next_room_id = MAX(next_room_id, ?) WHERE singleton = 1`,
		minimum,
	); err != nil {
		return 0, fmt.Errorf("normalize multiplayer room sequence: %w", err)
	}
	if err := accounts.pruneMultiplayerCompletions(transaction, time.Now()); err != nil {
		return 0, err
	}
	var nextRoomID int64
	if err := transaction.QueryRowContext(
		context.Background(),
		`SELECT next_room_id FROM cn_multiplayer_runtime WHERE singleton = 1`,
	).Scan(&nextRoomID); err != nil {
		return 0, fmt.Errorf("read multiplayer room sequence: %w", err)
	}
	// Reserve immediately, not only when a room eventually wins. Otherwise a
	// restart after defeat/retirement reuses a paid room's start receipt.
	if _, err := transaction.ExecContext(context.Background(),
		`UPDATE cn_multiplayer_runtime SET next_room_id = ? WHERE singleton = 1`, nextRoomID+1); err != nil {
		return 0, fmt.Errorf("reserve multiplayer room identity: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return 0, fmt.Errorf("commit multiplayer room sequence: %w", err)
	}
	committed = true
	return nextRoomID, nil
}

func (accounts *Accounts) SaveCompleted(completed multiplayer.CompletedBattle, expiresAt time.Time) error {
	return accounts.commitAccountStates(nil, func(transaction *sql.Tx) error { return accounts.writeCompleted(transaction, completed, expiresAt) })
}

func (accounts *Accounts) writeCompleted(transaction *sql.Tx, completed multiplayer.CompletedBattle, expiresAt time.Time) error {
	if err := validateCompletedBattle(completed, expiresAt); err != nil {
		return err
	}
	content, err := json.Marshal(completed)
	if err != nil {
		return fmt.Errorf("encode multiplayer completion: %w", err)
	}
	if len(content) == 0 || len(content) > maxMultiplayerCompletionBytes {
		return errors.New("multiplayer completion exceeds the storage boundary")
	}
	digest := sha256.Sum256(content)
	digestText := hex.EncodeToString(digest[:])

	if err := accounts.pruneMultiplayerCompletions(transaction, time.Now()); err != nil {
		return err
	}
	var existingDigest string
	err = transaction.QueryRowContext(
		context.Background(),
		`SELECT payload_sha256 FROM cn_multiplayer_completion WHERE room_id = ?`,
		completed.RoomID,
	).Scan(&existingDigest)
	switch {
	case err == nil && existingDigest != digestText:
		return errors.New("multiplayer room completion is immutable")
	case err == nil:
		// A repeated terminal BattleSv frame may only confirm the exact same
		// immutable projection; it must not allocate another outcome.
	case errors.Is(err, sql.ErrNoRows):
		if _, err := transaction.ExecContext(
			context.Background(),
			`INSERT INTO cn_multiplayer_completion
			 (room_id, expires_unix, payload_json, payload_sha256)
			 VALUES (?, ?, ?, ?)`,
			completed.RoomID,
			expiresAt.Unix(),
			content,
			digestText,
		); err != nil {
			return fmt.Errorf("write multiplayer completion: %w", err)
		}
		if err := writeMultiplayerClear(transaction, completed); err != nil {
			return err
		}
		if err := writeBossScoreRanking(transaction, completed); err != nil {
			return err
		}
	default:
		return fmt.Errorf("read multiplayer completion identity: %w", err)
	}
	if _, err := transaction.ExecContext(
		context.Background(),
		`INSERT OR IGNORE INTO cn_multiplayer_runtime (singleton, next_room_id) VALUES (1, ?)`,
		completed.RoomID+1,
	); err != nil {
		return fmt.Errorf("initialize multiplayer room sequence: %w", err)
	}
	if _, err := transaction.ExecContext(
		context.Background(),
		`UPDATE cn_multiplayer_runtime SET next_room_id = MAX(next_room_id, ?) WHERE singleton = 1`,
		completed.RoomID+1,
	); err != nil {
		return fmt.Errorf("advance multiplayer room sequence: %w", err)
	}
	return nil
}

func (accounts *Accounts) LoadCompleted(roomID int64, now time.Time) (multiplayer.CompletedBattle, time.Time, error) {
	if roomID <= 0 || now.IsZero() {
		return multiplayer.CompletedBattle{}, time.Time{}, errors.New("completed room lookup is invalid")
	}
	// Completions are immutable. A single WAL reader query is sufficient; a
	// lookup must not reserve the writer or serialize unrelated account work.
	// NextRoomID and SaveCompleted batch-prune expired rows on their write path.
	database, err := accounts.storage.OpenRead()
	if err != nil {
		return multiplayer.CompletedBattle{}, time.Time{}, err
	}
	var expiresUnix int64
	var content []byte
	var expectedDigest string
	if err := database.QueryRowContext(
		context.Background(),
		`SELECT expires_unix, payload_json, payload_sha256
		 FROM cn_multiplayer_completion WHERE room_id = ? AND expires_unix > ?`,
		roomID,
		now.Unix(),
	).Scan(&expiresUnix, &content, &expectedDigest); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return multiplayer.CompletedBattle{}, time.Time{}, multiplayer.ErrCompletedBattleUnavailable
		}
		return multiplayer.CompletedBattle{}, time.Time{}, fmt.Errorf("read multiplayer completion: %w", err)
	}
	if len(content) == 0 || len(content) > maxMultiplayerCompletionBytes {
		return multiplayer.CompletedBattle{}, time.Time{}, errors.New("persisted multiplayer completion exceeds the storage boundary")
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != expectedDigest {
		return multiplayer.CompletedBattle{}, time.Time{}, errors.New("persisted multiplayer completion digest mismatch")
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var completed multiplayer.CompletedBattle
	if err := decoder.Decode(&completed); err != nil {
		return multiplayer.CompletedBattle{}, time.Time{}, fmt.Errorf("decode multiplayer completion: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return multiplayer.CompletedBattle{}, time.Time{}, errors.New("persisted multiplayer completion has trailing JSON")
	}
	expiresAt := time.Unix(expiresUnix, 0)
	if completed.RoomID != roomID {
		return multiplayer.CompletedBattle{}, time.Time{}, errors.New("persisted multiplayer completion room ID mismatch")
	}
	if err := validateCompletedBattle(completed, expiresAt); err != nil {
		return multiplayer.CompletedBattle{}, time.Time{}, err
	}
	return completed, expiresAt, nil
}

func (accounts *Accounts) beginMultiplayerTransaction() (*sql.Tx, error) {
	database, err := accounts.storage.Open()
	if err != nil {
		return nil, err
	}
	transaction, err := database.BeginTx(context.Background(), &sql.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin multiplayer SQLite transaction: %w", err)
	}
	return transaction, nil
}

func (accounts *Accounts) pruneMultiplayerCompletions(transaction *sql.Tx, now time.Time) error {
	if _, err := transaction.ExecContext(
		context.Background(),
		`DELETE FROM cn_multiplayer_completion WHERE expires_unix <= ?`,
		now.Unix(),
	); err != nil {
		return fmt.Errorf("prune multiplayer completions: %w", err)
	}
	return nil
}

func validateCompletedBattle(completed multiplayer.CompletedBattle, expiresAt time.Time) error {
	if err := gamestate.ValidateTeamBattleScorePolicy(completed.ScorePolicy); err != nil {
		return fmt.Errorf("multiplayer completion score policy: %w", err)
	}
	if err := gamestate.ValidateTeamBattleScoreDamage(completed.ScorePolicy, completed.ScoreDamage); err != nil {
		return err
	}
	if completed.EndType < 0 || (completed.EndType > 2 && completed.EndType != 4) || (completed.EndType == 2 && completed.ScorePolicy == nil) {
		return errors.New("multiplayer completion score or end type is invalid")
	}
	if completed.BattleIndex < 0 || completed.BattleIndex >= multiplayer.MaxBattleWaves || completed.Progress < 0 || completed.Progress > completed.BattleIndex {
		return errors.New("multiplayer completion wave is invalid")
	}
	if completed.DropLedgerVersion < 0 || completed.DropLedgerVersion > 1 || completed.DestroyedEnemyBits < 0 || completed.DestroyedEnemyBits > 15 || len(completed.ReleasedDrops) > 512 {
		return errors.New("multiplayer completion drop ledger is invalid")
	}
	for _, drop := range completed.ReleasedDrops {
		if completed.DropLedgerVersion != 1 || drop.BattleIndex < 0 || drop.BattleIndex > completed.BattleIndex || drop.EnemyIndex < 0 || drop.EnemyIndex >= 4 || drop.ChancePerMillion != nil || !masterdata.ValidPersistedRewardShape(drop.Reward) {
			return errors.New("multiplayer completion drop entry is invalid")
		}
	}
	if completed.RoomID <= 0 || completed.BossID <= 0 || completed.BossGroupID < 0 ||
		completed.OwnerMemberType < 1 || completed.OwnerMemberType > 4 ||
		completed.CompletedAtUnix <= 0 || !expiresAt.After(time.Unix(completed.CompletedAtUnix, 0)) ||
		len(completed.Members) != 4 || len(completed.OnlineUserIDs) == 0 || len(completed.OnlineUserIDs) > 4 {
		return errors.New("multiplayer completion projection is invalid")
	}
	seenUsers := make(map[int]struct{}, len(completed.OnlineUserIDs))
	for _, userID := range completed.OnlineUserIDs {
		if userID <= 0 {
			return errors.New("multiplayer completion claimant is invalid")
		}
		if _, duplicate := seenUsers[userID]; duplicate {
			return errors.New("multiplayer completion claimant is duplicated")
		}
		seenUsers[userID] = struct{}{}
	}
	return nil
}
