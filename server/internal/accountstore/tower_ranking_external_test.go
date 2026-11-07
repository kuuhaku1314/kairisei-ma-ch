package accountstore_test

import (
	"testing"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func TestTowerRankingCommittedHistoryOrderPagingAndRollback(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	_ = testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x9880)
	slow := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x9881)
	fast := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x9882)
	complete := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x9883)
	noRecord := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x9884)
	page, err := accounts.TowerRanking(7, 4, noRecord, 0, 10)
	if err != nil || page.Total != 0 || page.Self != nil || len(page.Entries) != 0 {
		t.Fatal("unplayed accounts invented records", page, err)
	}
	put := func(userID, challenge int, cleared []int, turns map[int]int) gamestate.State {
		t.Helper()
		state, err := accounts.LoadState(userID)
		if err != nil {
			t.Fatal(err)
		}
		state.TowerQuestProgress = []gamestate.TowerQuestProgress{{TowerID: 7, Floor: challenge, ClearedFloors: cleared, BestClearTurns: turns}}
		if err := accounts.PersistState(userID, state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	put(accountstore.PrimaryUserID, 2, []int{1, 2, 3}, map[int]int{3: 10})
	put(slow, 2, []int{1, 2, 3}, map[int]int{3: 10})
	put(fast, 4, []int{1, 2, 3}, map[int]int{3: 1})
	put(complete, 0, []int{1, 2, 3, 4}, nil)
	page, err = accounts.TowerRanking(7, 4, accountstore.PrimaryUserID, 0, 2)
	if err != nil || page.Total != 4 || len(page.Entries) != 2 || page.Entries[0].UserID != complete ||
		page.Entries[0].Floor != 4 || page.Entries[1].UserID != fast || page.Self == nil || page.Self.RankNumber != 3 ||
		page.Self.Floor != 3 || page.Self.Score != 5500 {
		t.Fatal("highest clears, score ordering or primary account projection failed", page, err)
	}
	next, err := accounts.TowerRanking(7, 4, noRecord, 2, 2)
	if err != nil || next.Self != nil || len(next.Entries) != 2 || next.Entries[0].UserID != accountstore.PrimaryUserID ||
		next.Entries[1].UserID != slow || next.Entries[1].RankNumber != 4 {
		t.Fatal("paging or deterministic equal-score ordering failed", next, err)
	}
	empty, err := accounts.TowerRanking(7, 4, fast, 99, 10)
	if err != nil || len(empty.Entries) != 0 || empty.Total != 4 || empty.Self == nil || empty.Self.RankNumber != 2 {
		t.Fatal("empty page lost global rank", empty, err)
	}
	// Rewinding the challenge and persisting the same best history twice does
	// not lower or double the record. Profile metadata remains current.
	state := put(fast, 2, []int{1, 2, 3}, map[int]int{3: 1})
	state.User.Name = "Updated tower player"
	if err := accounts.PersistState(fast, state); err != nil {
		t.Fatal(err)
	}
	fresh := testfixture.TestAccountRepository(t, accounts.Database())
	page, err = fresh.TowerRanking(7, 4, fast, 0, 10)
	if err != nil || page.Self == nil || page.Self.Score != 10000 || page.Self.Floor != 3 || page.Self.Name != state.User.Name {
		t.Fatal("restart, repeat save or current public name changed the record", page, err)
	}
	db, err := accounts.Database().Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TRIGGER reject_tower_projection BEFORE UPDATE ON cn_account_projection
 BEGIN SELECT RAISE(ABORT,'test rollback'); END;`); err != nil {
		t.Fatal(err)
	}
	state.TowerQuestProgress[0].ClearedFloors = []int{1, 2, 3, 4}
	state.User.Name = "Must roll back"
	if err = accounts.PersistState(fast, state); err == nil {
		t.Fatal("save unexpectedly passed the rollback trigger")
	}
	if _, err = db.Exec(`DROP TRIGGER reject_tower_projection`); err != nil {
		t.Fatal(err)
	}
	page, err = accounts.TowerRanking(7, 4, fast, 0, 10)
	if err != nil || page.Self == nil || page.Self.Floor != 3 || page.Self.Name != "Updated tower player" {
		t.Fatal("failed save leaked a public record", page, err)
	}
}
