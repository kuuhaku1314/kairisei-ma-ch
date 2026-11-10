package game

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestTowerChallengeFailuresAccumulateWithinRank(t *testing.T) {
	const towerID = 7
	profile := gamestate.TowerQuestProfile{TowerID: towerID, LoseCountMax: 4,
		Floors: []gamestate.TowerQuestFloorProfile{{Floor: 1, Rank: 1}, {Floor: 2, Rank: 2},
			{Floor: 3, Rank: 2}, {Floor: 4, Rank: 2}, {Floor: 5, Rank: 3}}}
	s := &Account{onboarding: completedTestOnboarding(), towerQuestProfiles: map[int]gamestate.TowerQuestProfile{towerID: profile},
		towerQuestProgress: map[int]gamestate.TowerQuestProgress{towerID: {TowerID: towerID, Floor: 2, ClearedFloors: []int{1}}}}
	settle := func(floor int, clear bool) gamestate.TowerQuestProgress {
		t.Helper()
		if err := s.settleTowerQuestLocked(TeamBattleContext{TowerID: towerID, TowerFloor: floor}, clear); err != nil {
			t.Fatal(err)
		}
		return s.towerQuestProgress[towerID]
	}
	if p := settle(2, false); p.LoseCount != 1 || p.Floor != 2 {
		t.Fatal("first failure did not stay in the challenge", p)
	}
	if p := settle(2, true); p.LoseCount != 1 || p.Floor != 3 || p.LastRankUp || p.LastResultLoseCount != 1 {
		t.Fatal("same-rank victory erased accumulated failures", p)
	}
	settle(3, false)
	settle(3, true)
	if p := settle(4, false); p.LoseCount != 3 || p.Floor != 4 {
		t.Fatal("failures across floors did not accumulate in one rank", p)
	}
	if p := settle(4, false); p.LoseCount != 0 || p.Floor != 2 || p.LastResultLoseCount != 4 ||
		!slices.Equal(p.ClearedFloors, []int{1, 2, 3}) {
		t.Fatal("fourth failure did not restart this rank without erasing clears", p)
	}
	if p := settle(3, true); p.Floor != 2 || p.LoseCount != 0 || p.LastResult != "" {
		t.Fatal("replaying an already cleared later floor skipped the rank restart", p)
	}
	settle(2, false)
	settle(2, true)
	settle(3, true)
	if p := settle(4, true); p.Floor != 5 || p.LoseCount != 0 || !p.LastRankUp || p.LastResultLoseCount != 1 {
		t.Fatal("rank advancement did not reset the previous rank failures", p)
	}
	settle(5, false)
	if p := settle(5, true); p.Floor != 0 || p.LoseCount != 0 || p.LastRankUp {
		t.Fatal("completing the tower left an active challenge", p)
	}
}

func TestTowerReplaysPreserveChallengeAndSettleBestClear(t *testing.T) {
	const bossID, towerID = 123, 7
	for _, current := range []int{2, 3, 0} {
		t.Run(fmt.Sprintf("challenge_%d", current), func(t *testing.T) {
			losses := 2
			cleared := []int{1}
			if current == 3 {
				cleared = []int{1, 2}
			} else if current == 0 {
				losses = 0
				cleared = []int{1, 2, 3}
			}
			s := &Account{onboarding: completedTestOnboarding(),
				towerQuestProfiles: map[int]gamestate.TowerQuestProfile{towerID: {TowerID: towerID, LoseCountMax: 4,
					Floors: []gamestate.TowerQuestFloorProfile{{Floor: 1, Rank: 1}, {Floor: 2, Rank: 2}, {Floor: 3, Rank: 3}}}},
				towerQuestProgress: map[int]gamestate.TowerQuestProgress{towerID: {TowerID: towerID, Floor: current,
					LoseCount: losses, ClearedFloors: cleared, ClearRatings: map[int]int{1: 1}, BestClearTurns: map[int]int{1: 20},
					LastResult: "lose", LastRankUp: true, LastResultLoseCount: 2}},
			}
			profiles := []gamestate.TeamBattleRewardProfile{{BossID: bossID, TowerID: towerID, TowerFloor: 1,
				ResultRewards: []gamestate.Reward{{Type: 4, Num: 5}}, FirstClearRewards: []gamestate.Reward{{Type: 10, Num: 50}}}}
			for _, clear := range []bool{false, true} {
				s.activeBattle = &TeamBattleContext{FameSeed: "tower", FameSources: []TeamBattleFameSource{{MemberType: 1}}, BossID: bossID, TowerID: towerID, TowerFloor: 1}
				result, err := s.CompleteTeamBattle(bossID, clear, profiles, TeamBattleDropReport{Turns: 10})
				if err != nil {
					t.Fatal(err)
				}
				p := s.towerQuestProgress[towerID]
				if p.Floor != current || p.LoseCount != losses || p.LastBattleFloor != 1 || p.LastResult != "" ||
					p.LastRankUp || p.LastResultLoseCount != 0 || !slices.Equal(p.ClearedFloors, cleared) {
					t.Fatal("historical replay changed challenge progress or left challenge effects", p)
				}
				if clear && (result.WasFirstClear || s.gold != 5 || s.coinFree != 0 || p.ClearRatings[1] != 7 || p.BestClearTurns[1] != 10) {
					t.Fatal("replay lost ordinary rewards/best rating or repeated first-clear rewards", p, s.gold, s.coinFree)
				}
				if _, err := s.CompleteTeamBattle(bossID, clear, profiles, TeamBattleDropReport{Turns: 10}); err == nil {
					t.Fatal("replay settled twice")
				}
			}
			// A saved result round trip retains both the current challenge and
			// improved old-floor record; it does not depend on transient context.
			saved := s.Snapshot(gamestate.State{})
			body, err := json.Marshal(saved.TowerQuestProgress)
			if err != nil {
				t.Fatal(err)
			}
			var restored []gamestate.TowerQuestProgress
			if err := json.Unmarshal(body, &restored); err != nil || len(restored) != 1 || restored[0].Floor != current ||
				restored[0].LoseCount != losses || restored[0].ClearRatings[1] != 7 || restored[0].BestClearTurns[1] != 10 {
				t.Fatal("saved replay changed progress", err)
			}
		})
	}
}

func TestTowerSettlementReturnFreezesCommittedFloor(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		floor, losses         int
		clear                 bool
		progress, destination int
	}{
		{"advance", 2, 0, true, 3, 3},
		{"finish", 3, 0, true, 0, 3},
		{"failure", 3, 0, false, 3, 3},
		{"rank rewind", 3, 3, false, 2, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const towerID, bossID = 7, 123
			context := TeamBattleContext{FameSeed: "tower", FameSources: []TeamBattleFameSource{{MemberType: 1}}, BossID: bossID, TowerID: towerID, TowerFloor: tc.floor,
				StageType: gamestate.BattleStageAlchemist, EntryRole: gamestate.BattleEntrySolo}
			cleared := []int{1}
			if tc.floor == 3 {
				cleared = append(cleared, 2)
			}
			s := &Account{onboarding: completedTestOnboarding(), activeBattle: &context,
				towerAdmission: TowerAdmission{TowerIDs: map[int]struct{}{}}, // Closed after the accepted start.
				towerQuestProfiles: map[int]gamestate.TowerQuestProfile{towerID: {TowerID: towerID, LoseCountMax: 4,
					Floors: []gamestate.TowerQuestFloorProfile{{Floor: 1, Rank: 1}, {Floor: 2, Rank: 2}, {Floor: 3, Rank: 2}}}},
				towerQuestProgress: map[int]gamestate.TowerQuestProgress{towerID: {TowerID: towerID, Floor: tc.floor, LoseCount: tc.losses, ClearedFloors: cleared}},
			}
			profiles := []gamestate.TeamBattleRewardProfile{{BossID: bossID, TowerID: towerID, TowerFloor: tc.floor}}
			result, err := s.CompleteTeamBattle(bossID, tc.clear, profiles, TeamBattleDropReport{Turns: 10})
			if err != nil {
				t.Fatal(err)
			}
			if s.towerQuestProgress[towerID].Floor != tc.progress {
				t.Fatal("wrong committed progress")
			}
			snapshot := result.ResultSnapshot(BoolInt(tc.clear), 1, nil)
			if snapshot.Return == nil || snapshot.Return.BossID != bossID || snapshot.Return.TowerID != towerID ||
				snapshot.Return.StageType != context.StageType || snapshot.Return.TowerFloor != tc.destination {
				t.Fatalf("return differs from this settlement: %+v", snapshot.Return)
			}
			raw, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			// Later progress is not an input when the receipt is replayed.
			s.towerQuestProgress[towerID] = gamestate.TowerQuestProgress{TowerID: towerID, Floor: 999}
			var replay TeamBattleResultSnapshot
			if json.Unmarshal(raw, &replay) != nil || replay.Return == nil || replay.Return.TowerFloor != tc.destination {
				t.Fatal("cached destination drifted with current progress")
			}
		})
	}
}

func TestTowerPublicationClosesEntryWithoutErasingProgressOrInFlightStory(t *testing.T) {
	const towerID = 7
	profile := gamestate.TowerQuestProfile{TowerID: towerID, LoseCountMax: 3, Floors: []gamestate.TowerQuestFloorProfile{{
		Floor: 1, Rank: 1, BPUse: 30, Story: gamestate.TowerQuestStoryProfile{TalkID: 77},
	}}}
	s := &Account{onboarding: completedTestOnboarding(), bp: 100, bpMax: 100, bpRecoveryInterval: time.Minute,
		towerQuestProfiles: map[int]gamestate.TowerQuestProfile{towerID: profile},
		towerQuestProgress: map[int]gamestate.TowerQuestProgress{towerID: {TowerID: towerID, Floor: 1, ClearRatings: map[int]int{}}},
	}
	if len(s.TowerQuestIDs()) != 1 {
		t.Fatal("tower is not open by default")
	}
	s.ApplyTowerAdmission(TowerAdmission{TowerIDs: map[int]struct{}{}})
	var business *BusinessError
	if _, _, _, err := s.TowerQuestShowState(towerID); !errors.As(err, &business) {
		t.Fatal("closed tower detail was accessible")
	}
	if _, _, err := s.TowerQuestRankingState(towerID); !errors.As(err, &business) {
		t.Fatal("closed tower ranking was accessible")
	}
	if started, err := s.BeginTowerStory(77, towerID, 1); started || !errors.As(err, &business) || s.bp != 100 || s.activeSubStoryID != 0 {
		t.Fatal("closed tower story charged entry or started", err)
	}
	s.ApplyTowerAdmission(TowerAdmission{TowerIDs: map[int]struct{}{towerID: {}}})
	if ids := s.TowerQuestIDs(); len(ids) != 1 || ids[0] != towerID {
		t.Fatal("opening tower did not publish its directory entry", ids)
	}
	if started, err := s.BeginTowerStory(77, towerID, 1); err != nil || !started || s.bp != 70 {
		t.Fatal("open tower story did not charge once", started, err)
	}
	s.ApplyTowerAdmission(TowerAdmission{TowerIDs: map[int]struct{}{}})
	if started, err := s.BeginTowerStory(77, towerID, 1); err != nil || !started || s.bp != 70 {
		t.Fatal("closure interrupted an idempotent in-flight story", started, err)
	}
	if _, err := s.EndSubStory(true); err != nil {
		t.Fatal("closing tower interrupted an accepted story", err)
	}
	progress := s.towerQuestProgress[towerID]
	if progress.Floor != 0 || len(progress.ClearedFloors) != 1 || progress.ClearedFloors[0] != 1 || s.bp != 70 {
		t.Fatal("tower closure erased completed progress or changed entry cost", progress)
	}
	s.ApplyTowerAdmission(TowerAdmission{TowerIDs: map[int]struct{}{towerID: {}}})
	_, reopened, _, err := s.TowerQuestShowState(towerID)
	if err != nil || reopened.LastResult != "win" || reopened.Floor != 0 {
		t.Fatal("reopening tower lost its result and progress", reopened, err)
	}
}

func TestClosedTowerBattleStartDoesNotChargeEntry(t *testing.T) {
	s := &Account{onboarding: completedTestOnboarding(), bp: 100, bpMax: 100, bpRecoveryInterval: time.Minute,
		towerAdmission: TowerAdmission{TowerIDs: map[int]struct{}{}},
		towerQuestProfiles: map[int]gamestate.TowerQuestProfile{7: {TowerID: 7, Floors: []gamestate.TowerQuestFloorProfile{{
			Floor: 1, BPUse: 30, Boss: json.RawMessage(`{"0":123}`),
		}}}},
		towerQuestProgress: map[int]gamestate.TowerQuestProgress{7: {TowerID: 7, Floor: 1}},
	}
	helper := &s.playerProgression.Friends.HelperReward
	helper.OtherPerPartner, helper.FriendPerPartner = 5, 10
	_, _, started, err := s.BeginTeamBattle(TeamBattleEntry{
		BossID: 123, EnemyTypes: []int8{0}, EntryRole: gamestate.BattleEntrySolo,
		FameSeed: "solo-tower", FameSources: []TeamBattleFameSource{{MemberType: 1, LeaderFame: 1}},
	})
	var business *BusinessError
	if started || !errors.As(err, &business) || s.bp != 100 || s.activeBattle != nil {
		t.Fatal("closed tower start debited BP or created a battle", started, err)
	}
}

func TestTowerQuestRatingProgressIsAccountOwned(t *testing.T) {
	profile := gamestate.TowerQuestProfile{TowerID: 7}
	account := func(flag int) *Account {
		return &Account{onboarding: completedTestOnboarding(),
			towerQuestProfiles: map[int]gamestate.TowerQuestProfile{7: profile},
			towerAdmission:     TowerAdmission{TowerIDs: map[int]struct{}{7: {}}},
			towerQuestProgress: map[int]gamestate.TowerQuestProgress{
				7: {TowerID: 7, Floor: 2, ClearRatings: map[int]int{1: flag}, BestClearTurns: map[int]int{1: 12}},
			},
		}
	}
	first, second := account(3), account(7)
	_, view, _, err := first.TowerQuestShowState(7)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := first.Snapshot(gamestate.State{})
	body, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var saved gamestate.State
	if err := json.Unmarshal(body, &saved); err != nil || len(saved.TowerQuestProgress) != 1 || saved.TowerQuestProgress[0].ClearRatings[1] != 3 || saved.TowerQuestProgress[0].BestClearTurns[1] != 12 {
		t.Fatalf("rating progress did not survive its save payload: %s, %v", body, err)
	}
	view.ClearRatings[1] = 0
	snapshot.TowerQuestProgress[0].ClearRatings[1] = 0
	view.BestClearTurns[1] = 1
	snapshot.TowerQuestProgress[0].BestClearTurns[1] = 1
	_, firstView, _, _ := first.TowerQuestShowState(7)
	_, secondView, _, _ := second.TowerQuestShowState(7)
	if firstView.ClearRatings[1] != 3 || secondView.ClearRatings[1] != 7 || firstView.BestClearTurns[1] != 12 || secondView.BestClearTurns[1] != 12 {
		t.Fatal("a response or another account changed private floor ratings")
	}
}

func TestTowerClearRatingSettlementKeepsBestAndRejectsInvalidClear(t *testing.T) {
	const bossID, towerID = 123, 7
	s := &Account{onboarding: completedTestOnboarding(),
		towerQuestProfiles: map[int]gamestate.TowerQuestProfile{
			towerID: {TowerID: towerID, LoseCountMax: 3,
				Floors: []gamestate.TowerQuestFloorProfile{{Floor: 1, Rank: 1}}},
		},
		towerQuestProgress: map[int]gamestate.TowerQuestProgress{towerID: {TowerID: towerID, Floor: 1}},
	}
	profiles := []gamestate.TeamBattleRewardProfile{{BossID: bossID, TowerID: towerID, TowerFloor: 1,
		ResultRewards: []gamestate.Reward{{Type: 4, Num: 5}}, FirstClearRewards: []gamestate.Reward{{Type: 10, Num: 50}}}}
	start := func() {
		s.activeBattle = &TeamBattleContext{FameSeed: "tower", FameSources: []TeamBattleFameSource{{MemberType: 1}}, BossID: bossID, TowerID: towerID, TowerFloor: 1}
	}
	for index, step := range []struct{ turns, old, flag, best int }{
		{20, 0, 1, 20}, {16, 1, 1, 16}, {15, 1, 3, 15}, {11, 3, 3, 11},
		{10, 3, 7, 10}, {1, 7, 7, 1}, {20, 7, 7, 1},
	} {
		start()
		result, err := s.CompleteTeamBattle(bossID, true, profiles, TeamBattleDropReport{Turns: step.turns})
		if err != nil {
			t.Fatal(err)
		}
		if result.ClearRating == nil || *result.ClearRating != (TeamBattleClearRating{OldFlag: step.old, NewFlag: step.flag}) ||
			s.towerQuestProgress[towerID].BestClearTurns[1] != step.best || s.coinFree != 50 || s.gold != (index+1)*5 {
			t.Fatalf("turns %d: rating=%+v progress=%+v gold=%d coin=%d", step.turns, result.ClearRating, s.towerQuestProgress[towerID], s.gold, s.coinFree)
		}
		encoded, err := json.Marshal(result.ResultSnapshot(1, 1, nil))
		if err != nil {
			t.Fatal(err)
		}
		var replay TeamBattleResultSnapshot
		if err := json.Unmarshal(encoded, &replay); err != nil || replay.ClearRating == nil || *replay.ClearRating != *result.ClearRating {
			t.Fatal("result replay lost the old/new earned flags", err)
		}
		if _, err := s.CompleteTeamBattle(bossID, true, profiles, TeamBattleDropReport{Turns: step.turns}); err == nil {
			t.Fatal("completed run settled twice")
		}
	}
	start()
	if _, err := s.CompleteTeamBattle(bossID, false, profiles, TeamBattleDropReport{Turns: 20}); err != nil {
		t.Fatal(err)
	}
	if progress := s.towerQuestProgress[towerID]; progress.ClearRatings[1] != 7 || TowerQuestScore(progress) != 10000 || s.gold != 35 {
		t.Fatal("failure erased the best clear or granted rewards")
	}
	for _, turns := range []int{0, 21} {
		start()
		before, _ := json.Marshal(s.towerQuestProgress[towerID])
		if _, err := s.CompleteTeamBattle(bossID, true, profiles, TeamBattleDropReport{Turns: turns}); err == nil {
			t.Fatal("invalid turn count accepted")
		}
		after, _ := json.Marshal(s.towerQuestProgress[towerID])
		if string(before) != string(after) || s.gold != 35 || s.coinFree != 50 || s.activeBattle == nil {
			t.Fatal("rejected clear partially changed progression or rewards")
		}
	}
}

func TestTowerQuestRankingStateRejectsClosedTower(t *testing.T) {
	const towerID = 7
	state := &Account{onboarding: completedTestOnboarding(),
		towerAdmission: TowerAdmission{TowerIDs: map[int]struct{}{}},
		towerQuestProfiles: map[int]gamestate.TowerQuestProfile{
			towerID: {TowerID: towerID},
		},
		towerQuestProgress: map[int]gamestate.TowerQuestProgress{
			towerID: {TowerID: towerID},
		},
	}

	if _, _, err := state.TowerQuestRankingState(towerID); err == nil {
		t.Fatal("closed tower ranking unexpectedly returned a local rank row")
	}
	state.ApplyTowerAdmission(TowerAdmission{TowerIDs: map[int]struct{}{towerID: {}}})
	if _, _, err := state.TowerQuestRankingState(towerID); err != nil {
		t.Fatalf("opened tower ranking was rejected: %v", err)
	}
}
