package accountstore_test

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/testfixture"
)

func TestBossScoreRankingFirstBestAndAtomicCompletion(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	_ = testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x866)
	other := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x867)
	policy := &gamestate.TeamBattleScorePolicy{Rate: 1000,
		Grades: make([]gamestate.TeamBattleScoreGrade, 9)}
	for i := range policy.Grades {
		policy.Grades[i].Score = int64(i+1) * 100
	}
	completed := func(roomID int64, userIDs []int, damage, when int64) multiplayer.CompletedBattle {
		return multiplayer.CompletedBattle{RoomID: roomID, BossID: 37010101, OwnerMemberType: 1,
			Members: make([]multiplayer.Member, 4), OnlineUserIDs: userIDs, CompletedAtUnix: when,
			EndType: 2, ScoreDamage: damage, ScorePolicy: policy}
	}
	expires := time.Now().Add(time.Hour)
	first := completed(1, []int{accountstore.PrimaryUserID, other}, 50, 1000)
	retries := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { retries <- accounts.SaveCompleted(first, expires) }()
	}
	for i := 0; i < 2; i++ {
		if err := <-retries; err != nil {
			t.Fatal(err)
		}
	}
	for _, run := range []multiplayer.CompletedBattle{
		completed(2, []int{accountstore.PrimaryUserID}, 70, 2000),
		completed(3, []int{accountstore.PrimaryUserID}, 70, 2100),
		completed(4, []int{accountstore.PrimaryUserID}, 70, 1500), // Earlier accepted finish persisted later.
		completed(5, []int{other}, 70, 1400),
	} {
		if err := accounts.SaveCompleted(run, expires); err != nil {
			t.Fatal(err)
		}
	}
	page, err := accounts.BossScoreRanking(37010101, gamestate.BattleScoreMultiplayer, accountstore.PrimaryUserID, 0, 1)
	if err != nil || page.Total != 2 || len(page.Entries) != 1 || page.Entries[0].UserID != other ||
		page.Entries[0].Score != 700 || page.Entries[0].Grade != 6 || page.Entries[0].AchievedAt != 1400 ||
		page.Self == nil || page.Self.Rank != 2 || page.Self.AchievedAt != 1500 || page.Self.Name == "" {
		t.Fatalf("ranking order, grade, earliest best or name: %+v %v", page, err)
	}
	db, err := accounts.Database().Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_rank BEFORE INSERT ON cn_boss_score_rank
	 WHEN NEW.boss_id=37010102 BEGIN SELECT RAISE(ABORT,'forced rank failure'); END`); err != nil {
		t.Fatal(err)
	}
	failed := completed(6, []int{accountstore.PrimaryUserID}, 90, 3000)
	failed.BossID = 37010102
	if err := accounts.SaveCompleted(failed, expires); err == nil {
		t.Fatal("ranking failure committed immutable completion")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM cn_multiplayer_completion WHERE room_id=6`).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial completion persisted", count, err)
	}
	large := completed(7, []int{accountstore.PrimaryUserID}, 1000000000070, 4000)
	if err := accounts.SaveCompleted(large, expires); err != nil {
		t.Fatal("valid trillion-HP body and part score was rejected", err)
	}
	page, err = accounts.BossScoreRanking(37010101, gamestate.BattleScoreMultiplayer, accountstore.PrimaryUserID, 0, 1)
	if err != nil || page.Self == nil || page.Self.Score != 10000000000700 || page.Self.Grade != 8 {
		t.Fatal("large score was truncated or lost", page, err)
	}
	if err := accounts.SaveCompleted(completed(8, []int{accountstore.PrimaryUserID}, math.MaxInt64, 5000), expires); err == nil {
		t.Fatal("overflowing score multiplication was accepted")
	}
	// Solo ranking, score progress, reward balance and receipt must commit
	// together. Its highest score is independent from the room score above.
	for _, userID := range []int{accountstore.PrimaryUserID, other} {
		state, err := accounts.LoadState(userID)
		if err != nil {
			t.Fatal(err)
		}
		before := state.User.Gold
		state.User.Gold += 7
		state.TeamBattleSoloScores = map[int]gamestate.TeamBattleScoreProgress{37010101: {HighScore: 800, Claimed: 255}}
		hash := strings.Repeat("c", 64)
		state.TeamBattleSoloResultReceipts = []gamestate.TeamBattleSoloResultReceipt{{BossID: 37010101, RequestSHA256: hash, ClaimedAtUnix: 6000, Snapshot: json.RawMessage(`{"is_clear":0,"member_type":1}`)}}
		commit := game.SoloBattleCommit{BossID: 37010101, ReceiptSHA: hash, Score: &gamestate.BossScoreResult{BossID: 37010101, Score: 800, Grade: 7, AchievedAt: 6000}}
		if _, err := db.Exec(`CREATE TRIGGER fail_solo_rank BEFORE INSERT ON cn_boss_score_rank WHEN NEW.mode=0 BEGIN SELECT RAISE(ABORT,'forced'); END`); err != nil {
			t.Fatal(err)
		}
		if err := accounts.PersistSoloBattle(state, commit); err == nil {
			t.Fatal("failed solo rank committed account")
		}
		rolled, err := accounts.LoadState(userID)
		if err != nil || rolled.User.Gold != before || len(rolled.TeamBattleSoloScores) != 0 {
			t.Fatal("partial solo score settlement", err)
		}
		if _, err := db.Exec(`DROP TRIGGER fail_solo_rank`); err != nil {
			t.Fatal(err)
		}
		for retry := 0; retry < 2; retry++ {
			if err := accounts.PersistSoloBattle(state, commit); err != nil {
				t.Fatal(err)
			}
		}
		reloaded, err := accounts.LoadState(userID)
		if err != nil || reloaded.User.Gold != before+7 || reloaded.TeamBattleSoloScores[37010101].Claimed != 255 {
			t.Fatal("solo progress lost or repeated reward", err)
		}
	}
	personal, err := accounts.BossScoreRanking(37010101, gamestate.BattleScoreSolo, accountstore.PrimaryUserID, 0, 100)
	team, teamErr := accounts.BossScoreRanking(37010101, gamestate.BattleScoreMultiplayer, accountstore.PrimaryUserID, 0, 100)
	if err != nil || teamErr != nil || personal.Total != 2 || personal.Self == nil || personal.Self.Score != 800 || team.Self == nil || team.Self.Score != 10000000000700 {
		t.Fatal("solo and multiplayer rankings were mixed", personal, team, err, teamErr)
	}
}
