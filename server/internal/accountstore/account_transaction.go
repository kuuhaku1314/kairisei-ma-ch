package accountstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
)

// PersistStates commits a room's start fees and receipts together. Callers hold
// all participating account locks; the SQLite writer serializes this transaction
// with unrelated saves without any room or registry lock.
func (accounts *Accounts) PersistStates(states []gamestate.State) error {
	if len(states) == 0 || len(states) > 4 {
		return errors.New("account transaction requires one to four participants")
	}
	return accounts.commitAccountStates(states, nil)
}

// PersistCompletedBattle commits room identity, clear statistics, inventories
// and result snapshots together. The caller holds every eligible account lock.
func (accounts *Accounts) PersistCompletedBattle(completed multiplayer.CompletedBattle, expiresAt time.Time, states []gamestate.State) error {
	if len(states) > len(completed.OnlineUserIDs) {
		return errors.New("completion contains extra account updates")
	}
	for _, state := range states {
		eligible := false
		for _, id := range completed.OnlineUserIDs {
			eligible = eligible || state.User.UserID == id
		}
		found := false
		for _, receipt := range state.TeamBattleResultReceipts {
			found = found || receipt.RoomID == completed.RoomID
		}
		if !eligible || !found {
			return errors.New("completion account has no matching result snapshot")
		}
	}
	return accounts.commitAccountStates(states, func(tx *sql.Tx) error {
		return accounts.writeCompleted(tx, completed, expiresAt)
	})
}

func (accounts *Accounts) commitAccountStates(states []gamestate.State, extra func(*sql.Tx) error) error {
	contents := make([][]byte, len(states))
	seen := make(map[int]bool, len(states))
	for i, state := range states {
		userID := state.User.UserID
		if userID < PrimaryUserID || userID >= SystemPartnerUserIDBase || seen[userID] {
			return errors.New("account transaction has an invalid or duplicate user")
		}
		seen[userID] = true
		content, err := EncodeAccountMetadata(state)
		if err != nil {
			return err
		}
		contents[i] = content
	}
	database, err := accounts.storage.Open()
	if err != nil {
		return err
	}
	transaction, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin account transaction: %w", err)
	}
	defer transaction.Rollback()
	if extra != nil {
		if err := extra(transaction); err != nil {
			return err
		}
	}
	updatedUTC := time.Now().UTC().Format(time.RFC3339Nano)
	for i, state := range states {
		digest := sha256.Sum256(contents[i])
		digestText := hex.EncodeToString(digest[:])
		if state.User.UserID == PrimaryUserID {
			err = writePrimaryAccountSnapshot(transaction, state, contents[i], digestText, updatedUTC, nil)
		} else {
			err = writeAccountSnapshot(transaction, state.User.UserID, state, contents[i], digestText, updatedUTC, nil)
		}
		if err != nil {
			return err
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit account transaction: %w", err)
	}
	for _, state := range states {
		accounts.RememberBattle(state.User.UserID, state.ActiveTeamBattle, state.StoryTeamBattleSession)
	}
	return nil
}
