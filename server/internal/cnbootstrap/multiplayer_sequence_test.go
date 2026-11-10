package cnbootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/testfixture"
)

func TestRoomSequenceReservesUnfinishedRooms(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	first, err := accounts.NextRoomID(1000000)
	if err != nil {
		t.Fatal(err)
	}
	// Each call opens a new SQLite transaction; no SaveCompleted is needed.
	second, err := accounts.NextRoomID(1000000)
	if err != nil {
		t.Fatal(err)
	}
	if second != first+1 {
		t.Fatalf("unfinished paid room ID reused: %d -> %d", first, second)
	}
	// Different rooms reserve concurrently; uniqueness belongs to the durable
	// transaction, independently of the Hub and account/login mutexes.
	type reservation struct {
		id  int64
		err error
	}
	results := make(chan reservation, 32)
	for i := 0; i < cap(results); i++ {
		go func() { id, err := accounts.NextRoomID(1000000); results <- reservation{id, err} }()
	}
	seen := map[int64]bool{}
	for i := 0; i < cap(results); i++ {
		select {
		case result := <-results:
			if result.err != nil || result.id <= second || seen[result.id] {
				t.Fatalf("concurrent reservation: %+v", result)
			}
			seen[result.id] = true
		case <-time.After(3 * time.Second):
			t.Fatal("room ID reservation deadlocked")
		}
	}
}

func TestMultiWaveCompletionPersistsAcrossDatabaseConnections(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	now := time.Now()
	completed := multiplayer.CompletedBattle{RoomID: 123, BossID: 1, OwnerMemberType: 1, Members: make([]multiplayer.Member, 4), OnlineUserIDs: []int{1001}, CompletedAtUnix: now.Unix(), BattleIndex: 5, Progress: 5, HostCostPaid: true}
	for i := range completed.Members {
		completed.Members[i] = multiplayer.Member{MemberType: i + 1, UserID: 1001 + i}
	}
	for i := 0; i <= completed.BattleIndex; i++ {
		completed.ReleasedDrops = append(completed.ReleasedDrops, gamestate.TeamBattleEnemyDrop{BattleIndex: i, Reward: gamestate.Reward{Type: 4, Num: i + 1, CardSkillLevels: []int16{}}})
	}
	// Racing saves may confirm an identical immutable outcome, never create
	// another reward context or overwrite one with a different result.
	saved := make(chan error, 12)
	for i := 0; i < cap(saved); i++ {
		go func() { saved <- accounts.SaveCompleted(completed, now.Add(time.Hour)) }()
	}
	for i := 0; i < cap(saved); i++ {
		select {
		case err := <-saved:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("completion save deadlocked")
		}
	}
	conflicting := completed
	conflicting.Turns++
	if err := accounts.SaveCompleted(conflicting, now.Add(time.Hour)); err == nil {
		t.Fatal("conflicting completion overwrote the existing outcome")
	}
	loaded, _, err := accounts.LoadCompleted(123, now)
	if err != nil || loaded.BattleIndex != 5 || loaded.Progress != 5 || len(loaded.ReleasedDrops) != 6 || loaded.ReleasedDrops[5].Reward.Num != 6 {
		t.Fatalf("wave ledger did not survive SQLite reload: %+v %v", loaded, err)
	}
	// A pending writer must not prevent an immutable result from being read.
	db, err := accounts.Database().Open()
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	read := make(chan error, 1)
	go func() {
		_, _, err := accounts.LoadCompleted(123, now)
		read <- err
	}()
	select {
	case err := <-read:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("completion read waited for the SQLite writer")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := accounts.LoadCompleted(123, now.Add(time.Hour)); !errors.Is(err, multiplayer.ErrCompletedBattleUnavailable) {
		t.Fatalf("expired completion remained available: %v", err)
	}
	completed.ReleasedDrops[5].BattleIndex = 6
	if err := accounts.SaveCompleted(completed, now.Add(time.Hour)); err == nil {
		t.Fatal("accepted a reward from a wave not completed")
	}
}
