package httpapi

import (
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"testing"
	"time"
)

func TestCommittedPVPPolicyResultCannotBeRejudged(t *testing.T) {
	var decks []gamestate.PVPDeckSelection
	s := testAccount(t, func(state *gamestate.State) {
		state.User.PVPPoint = 100
		state.PVP = gamestate.PVPPlayerState{NextBattleID: 1, Challenge: 5, DefenseDecks: []gamestate.PVPDeckSelection{}, History: []gamestate.PVPMatch{}}
		state.MissionDefinitions = map[int]gamestate.MissionDefinition{1: {MissionID: 1, Published: true, Title: "竞技场对局", Rule: &gamestate.MissionRule{Event: gamestate.MissionPVPPlayed, Target: 3}}}
		decks = activePVPDeckSelections(state.Decks)
	})
	config := game.PVPConfig{ResultSource: game.PVPResultFixedPolicy, EngineMode: game.PvpEngineClientNativeLocal, ReplayResult: 1, RankWinPoint: 20, RankLosePoint: -10}
	first, _, err := s.BeginPVP(1, 1002, decks, config, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	point, _ := s.PvpStatus()
	if point != 120 || first.CompletedAtUnix == 0 || s.Snapshot(gamestate.State{}).PVP.ActiveMatch != nil || s.MissionInfos(false)[0].ProgressNow != 1 {
		t.Fatal("PVP begin observation changed")
	}
	second, _, err := s.BeginPVP(1, 1003, decks, config, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = s.FinishPVP(first.BattleID, false, false, config, time.Now())
	if err == nil {
		t.Fatal("conflicting old result accepted")
	}
	point, _ = s.PvpStatus()
	if point != 140 || s.MissionInfos(false)[0].ProgressNow != 2 {
		t.Fatalf("late first loss changed points: %d", point)
	}
	if _, _, _, err := s.FinishPVP(second.BattleID, false, false, config, time.Now()); err == nil {
		t.Fatal("latest committed result was rejudged")
	}
	if _, _, _, err := s.FinishPVP(first.BattleID, true, false, config, time.Now()); err != nil {
		t.Fatal("identical late acknowledgement rejected", err)
	}
	point, _ = s.PvpStatus()
	if point != 140 || s.MissionInfos(false)[0].ProgressNow != 2 {
		t.Fatal("acknowledgement changed ranking")
	}
}
