package game

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/battlemodel"
	"kairisei.local/server/internal/gamestate"
)

func TestBattleCatalogEntryQueriesPreserveWireRules(t *testing.T) {
	raw := json.RawMessage(`{"9":[],"10":[{"0":300101,"1":101,"9":0,"future":9007199254740993,"10":[{"0":30010102,"1":1,"5":30,"6":15,"7":1,"10":0,"14":1,"16":0,"18":{"0":0,"1":1,"2":0,"3":0,"4":1,"5":0},"24":0},{"0":30010103,"5":20,"10":2,"14":1,"16":100}]}],"11":[],"12":[]}`)
	catalog, err := decodeTeamBattleCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	id, group, found := catalog.GroupForBoss(30010102)
	if !found || id != 300101 || group.(map[string]any)["future"] != json.Number("9007199254740993") {
		t.Fatal("group query lost identity or unmodeled protocol precision")
	}
	want := TeamBattleEntryRules{BossID: 30010102, OnlyMyDeck: 1, BPUse: 30, BPUseHalf: 15, Continue: 1, StageType: gamestate.BattleStageCNWorldBoss,
		AwakeTakeover: battlemodel.WaveTakeover{KeepCost: true, KeepDeckTrash: true}}
	got, ok := catalog.EntryRules(30010102)
	if !ok || !reflect.DeepEqual(got, want) || got.AllowsMultiplayer() || !got.AllowsSolo() {
		t.Fatal("typed entry query differs from the wire rules or own-deck-only restriction")
	}
	if !catalog.BossAccessible(30010102, 100) || catalog.BossAccessible(30010103, 100) || !catalog.BossAccessible(30010103, 99) {
		t.Fatal("NEW/CLEAR or exact expiry boundary changed")
	}
	if catalog.HasBoss(999) || catalog.BossAccessible(999, 100) {
		t.Fatal("unknown boss became accessible")
	}
	// Stage identity must not bypass the own-deck-only restriction, including
	// the reserved identity of the retired CN World BOSS feature.
	// The raw catalog and the typed query keep the same canonical identity.
	for _, stage := range []gamestate.BattleStageType{gamestate.BattleStageAlchemist, gamestate.BattleStageAlchemistSP, gamestate.BattleStageMainQuest, gamestate.BattleStageCNRecommendCard, gamestate.BattleStageCNWorldBoss} {
		value, _ := json.Marshal(stage)
		input := strings.Replace(string(raw), `"1":101`, `"1":`+string(value), 1)
		view, err := decodeTeamBattleCatalog(json.RawMessage(input))
		if err != nil {
			t.Fatal(err)
		}
		_, group, _ := view.GroupForBoss(30010102)
		rules, ok := view.EntryRules(30010102)
		want.StageType = stage
		if !ok || !reflect.DeepEqual(rules, want) || rules.StageType != stage || group.(map[string]any)["1"] != json.Number(value) || rules.AllowsMultiplayer() {
			t.Fatalf("stage identity %d was reinterpreted or bypassed the own-deck-only restriction", stage)
		}
	}
}

func TestSoloEntryFreezesContinueRuleWithDebit(t *testing.T) {
	const boss = 30010102
	// A recovery item can leave enough BP for a quest costing more than the
	// natural cap. Entry checks the actual balance, not that cap.
	s := &Account{bp: 30, bpMax: 10, bpRecoveryInterval: 3 * time.Minute, coin: 32, coinFree: 20,
		teamBattleSolo: battleCatalogFixture(t, json.RawMessage(`{"9":[],"10":[{"0":300101,"9":0,"10":[{"0":30010102,"5":15,"7":1,"10":0,"24":3}]}],"11":[],"12":[]}`)),
	}
	helper := &s.playerProgression.Friends.HelperReward
	helper.OtherPerPartner, helper.FriendPerPartner, helper.MaximumPartners = 5, 10, 3
	profiles := []gamestate.TeamBattleRewardProfile{{BossID: boss, ResultRewards: []gamestate.Reward{{Type: 4, Num: 10}}}}
	startID := "solo:offline"
	start := func() error {
		_, _, _, err := s.BeginTeamBattle(TeamBattleEntry{BossID: boss, EnemyTypes: []int8{0}, Profiles: profiles, FameSeed: startID, FameSources: []TeamBattleFameSource{{MemberType: 1, LeaderFame: 1}}})
		return err
	}
	if err := start(); err == nil || s.bp != 30 || s.activeBattle != nil {
		t.Fatal("multiplayer-only entry started or debited solo")
	}
	s.teamBattleSolo = battleCatalogFixture(t, json.RawMessage(strings.Replace(string(s.teamBattleSolo.saved()), `"24":3`, `"24":0`, 1)))
	s.disabledTeamBattleBossIDs = map[int]bool{boss: true}
	if err := start(); err == nil || s.bp != 30 || s.activeBattle != nil {
		t.Fatal("closed difficulty accepted a stale start or charged BP")
	}
	s.disabledTeamBattleBossIDs = nil
	if err := start(); err != nil || s.bp != 15 || !s.activeBattle.ContinueAllowed {
		t.Fatalf("solo entry failed to freeze the accepted rule: %v", err)
	}
	firstSeed := s.activeBattle.Seed
	if firstSeed <= 0 || firstSeed > 2147483647 {
		t.Fatal("solo start did not allocate a native Int32 seed")
	}
	if err := start(); err != nil || s.activeBattle.Seed != firstSeed || s.bp != 15 {
		t.Fatal("start retry changed its shuffle seed or debited twice", err)
	}
	// Simulate a rule change and an in-process account reload while the client is
	// still in the accepted battle. The new rule applies to the next entry.
	s.teamBattleSolo = battleCatalogFixture(t, json.RawMessage(strings.Replace(string(s.teamBattleSolo.saved()), `"7":1`, `"7":0`, 1)))
	s.teamBattleSolo = battleCatalogFixture(t, json.RawMessage(strings.Replace(string(s.teamBattleSolo.saved()), `"5":15`, `"5":99`, 1)))
	if err := s.validateActiveTeamBattleContextLocked(*s.activeBattle, profiles); err != nil || s.activeBattle.BPUse != 15 {
		t.Fatal("a published price change invalidated or repriced the accepted run", err)
	}
	s.teamBattleSolo = battleCatalogFixture(t, json.RawMessage(strings.Replace(string(s.teamBattleSolo.saved()), `"5":99`, `"5":15`, 1)))
	encoded, err := json.Marshal(snapshotTeamBattleContext(s.activeBattle))
	if err != nil {
		t.Fatal(err)
	}
	var active gamestate.TeamBattleActiveState
	if err := json.Unmarshal(encoded, &active); err != nil {
		t.Fatal(err)
	}
	s.activeBattle = teamBattleContextFromRelease(&active)
	if s.activeBattle.Seed != firstSeed {
		t.Fatal("account handler reload lost the active battle seed")
	}
	report := TeamBattleContinueReport{PayType: 3, Progress: 1,
		InputCommands: []string{"0,22" + strings.Repeat(",0", 20)}, EnemyDeadBits: []int{0}}
	if _, _, err := s.ContinueTeamBattle(report, gamestate.State{}, func(gamestate.State) error { return errors.New("disk unavailable") }); err == nil || s.coinFree != 20 || s.coin != 32 || len(s.activeBattle.ContinueReceipts) != 0 {
		t.Fatal("failed persistence did not roll back continuation")
	}
	var saved gamestate.State
	writes := 0
	persist := func(state gamestate.State) error { saved = state; writes++; return nil }
	if _, _, err := s.ContinueTeamBattle(report, gamestate.State{}, persist); err != nil || s.coinFree != 0 || s.coin != 2 {
		t.Fatalf("published rule changed active continuation: %v", err)
	}
	s.activeBattle = teamBattleContextFromRelease(saved.ActiveTeamBattle)
	if _, _, err := s.ContinueTeamBattle(report, gamestate.State{}, persist); err != nil || writes != 1 || s.coin != 2 {
		t.Fatalf("durable continuation retry failed or charged again: %v", err)
	}
	// The player killed the client without sending its result. A valid new
	// start replaces the old run; an invalid request must leave it intact.
	startID = "solo:next"
	s.teamBattleSolo = battleCatalogFixture(t, json.RawMessage(strings.Replace(string(s.teamBattleSolo.saved()), `"24":0`, `"24":3`, 1)))
	if err := start(); err == nil || s.activeBattle.FameSeed != "solo:offline" || s.bp != 15 {
		t.Fatal("invalid new entry discarded the prior battle or charged BP")
	}
	s.teamBattleSolo = battleCatalogFixture(t, json.RawMessage(strings.Replace(string(s.teamBattleSolo.saved()), `"24":3`, `"24":0`, 1)))
	s.teamBattleSoloReceipts = map[string]gamestate.TeamBattleSoloResultReceipt{"old-retreat": {BossID: boss}}
	if err := start(); err != nil || s.activeBattle.ContinueAllowed {
		t.Fatalf("next entry did not accept the new no-continue rule: %v", err)
	}
	if s.bp != 0 || len(s.teamBattleSoloReceipts) != 0 || s.activeBattle.FameSeed != startID {
		t.Fatal("new run did not replace the old result scope and debit exactly once")
	}
	if err := start(); err != nil || s.bp != 0 {
		t.Fatal("repeated start was rejected or charged twice", err)
	}
	if _, _, err := s.ContinueTeamBattle(report, gamestate.State{}, persist); err == nil || writes != 1 || s.coin != 2 {
		t.Fatal("forbidden continuation was charged")
	}
	s.AbandonSoloBattle()
	if s.activeBattle != nil || s.bp != 0 {
		t.Fatal("returning home retained a battle or refunded its entry")
	}
}
