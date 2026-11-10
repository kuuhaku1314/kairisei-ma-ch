package game

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestMissingCardSourcesFollowInventoryWithoutChangingDetailQueries(t *testing.T) {
	s := &Account{onboarding: completedTestOnboarding(),
		cardDefinitions: map[int]gamestate.Card{1: {CardID: 1}, 2: {CardID: 2}, 3: {CardID: 3}},
		cards:           []CardInfo{{UniqueID: 101, CardID: 1}},
		containerCards:  []CardInfo{{UniqueID: 102, CardID: 2}},
	}
	ids := []int{3, 1, 2}
	missing, err := s.HowToGetMissingCards(ids, nil, CardSourcePolicy{})
	if err != nil || len(missing) != 2 || missing[0].CardID != 3 || missing[1].CardID != 2 {
		t.Fatalf("missing list should exclude equipped inventory and retain warehouse-only cards: %+v (%v)", missing, err)
	}
	all, err := s.HowToGetCards(ids, nil, CardSourcePolicy{})
	if err != nil || len(all) != 3 || all[1].CardID != 1 || !slices.Equal(ids, []int{3, 1, 2}) {
		t.Fatalf("detail query or caller-owned IDs changed: %+v (%v)", all, err)
	}
	s.cards = append(s.cards, CardInfo{UniqueID: 103, CardID: 2}, CardInfo{UniqueID: 104, CardID: 3})
	missing, err = s.HowToGetMissingCards(ids, nil, CardSourcePolicy{})
	if err != nil || missing == nil || len(missing) != 0 {
		t.Fatalf("new inventory should clear the missing-card response: %+v (%v)", missing, err)
	}
}

func TestCardSourcesRespectGachaPublicationAndRandomGiftEligibility(t *testing.T) {
	card := func(id int) gamestate.Reward { return gamestate.Reward{Type: 6, RewardTypeID: id, Num: 1} }
	s := &Account{onboarding: completedTestOnboarding(), cardDefinitions: map[int]gamestate.Card{}, gachas: []gamestate.GachaProfile{{
		GachaID: 101, GroupID: 100, Name: "附赠池", PlayCount: 2, PlayCountMax: 3,
		RewardPool: []gamestate.WeightedReward{{Reward: card(1), Weight: 1}},
		Gifts:      []gamestate.GachaGiftRule{{FromPlay: 3, ToPlay: 3, Rewards: []gamestate.Reward{card(2)}}},
		RandomGifts: []gamestate.GachaRandomGiftRule{
			{FromPlay: 3, ToPlay: 3, Pool: []gamestate.WeightedReward{{Reward: card(3), Weight: 1}}},
			{FromPlay: 1, ToPlay: 2, Pool: []gamestate.WeightedReward{{Reward: card(4), Weight: 1}}},
			{FromPlay: 4, Pool: []gamestate.WeightedReward{{Reward: card(5), Weight: 1}}},
			{FromPlay: 3, Pool: []gamestate.WeightedReward{{Reward: card(6), Weight: 0}}},
		},
	}}}
	ids := []int{1, 2, 3, 4, 5, 6}
	for _, id := range ids {
		s.cardDefinitions[id] = gamestate.Card{CardID: id}
	}
	open := false
	policy := CardSourcePolicy{GachaPublished: func(id int) bool { return open && id == 101 }}
	for _, query := range []func([]int, []gamestate.TeamBattleRewardProfile, CardSourcePolicy) ([]howToGetCardList, error){s.HowToGetCards, s.HowToGetMissingCards} {
		for _, published := range []bool{false, true, false, true} {
			open = published
			rows, err := query(ids, nil, policy)
			if err != nil || len(rows) != len(ids) {
				t.Fatal("source query failed", rows, err)
			}
			for i, row := range rows {
				want := 0
				if open && i < 3 {
					want = 7
				}
				if len(row.GetCards) != 1 || row.GetCards[0].Type != want {
					t.Fatalf("published=%t card=%d sources=%+v", open, row.CardID, row.GetCards)
				}
				if open && row.CardID == 3 && !strings.Contains(row.GetCards[0].Text, "随机附赠") {
					t.Fatal("random gift was not distinguished from the main pool")
				}
			}
		}
	}
	for _, window := range [][2]int64{{time.Now().Add(time.Hour).Unix(), 0}, {0, time.Now().Add(-time.Hour).Unix()}, {0, 1}} {
		s.gachaWindows = map[int][2]int64{101: window}
		rows, err := s.HowToGetCards(ids, nil, policy)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if len(row.GetCards) != 1 || row.GetCards[0].Type != 0 {
				t.Fatal("scheduled or disabled pool remained obtainable", window, rows)
			}
		}
	}
}

func TestCardSourcesFollowPublishedRewardsAndRemainingEligibility(t *testing.T) {
	card := func(id int) gamestate.Reward { return gamestate.Reward{Type: 6, RewardTypeID: id, Num: 1} }
	pool := func(id int) []gamestate.WeightedReward {
		return []gamestate.WeightedReward{{Reward: card(id), Weight: 1}}
	}
	zero := 0
	s := &Account{onboarding: completedTestOnboarding(),
		cardDefinitions:    map[int]gamestate.Card{},
		stackCardTemplates: map[int]gamestate.CardStack{},
		teamBattleSolo:     battleCatalogFixture(t, json.RawMessage(`{"9":[],"10":[{"0":100,"4":"活动","10":[{"0":101,"4":"超级","10":0,"26":0}]}],"11":[],"12":[]}`)),
		gachas: []gamestate.GachaProfile{{GachaID: 201, GroupID: 200, Name: "阶段池", PlayCount: 1, PlayCountMax: 3,
			Steps: []gamestate.GachaStep{{RewardPool: pool(1)}, {RewardPool: pool(2)}, {RewardPool: pool(3)}, {RewardPool: pool(4)}},
			Gifts: []gamestate.GachaGiftRule{{FromPlay: 1, ToPlay: 1, Rewards: []gamestate.Reward{card(1)}}, {FromPlay: 3, Rewards: []gamestate.Reward{card(5)}}}}},
		tradeShopProfiles: map[int]gamestate.TradeShopProfile{301: {TradeShopID: 301, Name: "兑换所", Lineups: []gamestate.TradeShopLineupProfile{
			{LineupID: 311, StockNum: 1, Rewards: []gamestate.Reward{card(6)}}, {LineupID: 312, Disabled: true, Rewards: []gamestate.Reward{card(1)}},
		}}},
		tradeShopPurchases: map[int]int{},
		missionCatalog:     missionCatalogFixture(t, map[int]gamestate.MissionDefinition{401: {MissionID: 401, Published: true, Title: "任务", Rewards: []gamestate.Reward{card(7)}}}),
		missions:           map[int]gamestate.MissionProgress{},
		cardActions:        gamestate.CardActionState{EvolutionTransitions: []gamestate.EvolutionTransition{{FromCardID: 1, ToCardID: 8}}},
		towerAdmission:     TowerAdmission{TowerIDs: map[int]struct{}{501: {}}},
		towerQuestProfiles: map[int]gamestate.TowerQuestProfile{501: {TowerID: 501, Name: "塔",
			Floors: []gamestate.TowerQuestFloorProfile{{Floor: 1, Boss: json.RawMessage(`{"0":511}`)}}}},
		towerQuestProgress: map[int]gamestate.TowerQuestProgress{501: {TowerID: 501}},
	}
	ids := []int{}
	for id := 1; id <= 13; id++ {
		s.cardDefinitions[id] = gamestate.Card{CardID: id}
		ids = append(ids, id)
	}
	delete(s.cardDefinitions, 10)
	s.stackCardTemplates[10] = gamestate.CardStack{CardID: 10}
	material := gamestate.Reward{Type: 13, RewardTypeID: 10, Num: 1}
	profiles := []gamestate.TeamBattleRewardProfile{
		{BossID: 101, EnemyDrops: []gamestate.TeamBattleEnemyDrop{{Reward: material}, {Reward: card(1), ChancePerMillion: &zero}},
			FirstClearRewards: []gamestate.Reward{card(9)}, ScorePolicy: &gamestate.TeamBattleScorePolicy{Grades: []gamestate.TeamBattleScoreGrade{{Rewards: []gamestate.Reward{card(11)}}}}},
		{BossID: 511, TowerID: 501, TowerFloor: 1, FirstClearRewards: []gamestate.Reward{card(12)}},
	}
	read := func(groups map[int]struct{}) map[int][]howToGetCardEntry {
		t.Helper()
		lists, err := s.HowToGetCards(ids, profiles, CardSourcePolicy{BattleGroups: groups})
		if err != nil {
			t.Fatal(err)
		}
		rows := map[int][]howToGetCardEntry{}
		for _, list := range lists {
			rows[list.CardID] = list.GetCards
		}
		return rows
	}
	check := func(rows map[int][]howToGetCardEntry, id, kind, target, subtype int) {
		t.Helper()
		if len(rows[id]) != 1 || rows[id][0].Type != kind || rows[id][0].ContentID != target || rows[id][0].Subtype != subtype {
			t.Fatalf("card %d sources: %+v; want type=%d target=%d subtype=%d", id, rows[id], kind, target, subtype)
		}
	}
	rows := read(nil)
	for _, id := range []int{1, 4, 13} {
		check(rows, id, 0, 0, 0)
	}
	for _, id := range []int{2, 3, 5} {
		check(rows, id, 7, 201, 0)
	}
	check(rows, 6, 8, 301, 0)
	check(rows, 7, 9, 401, 0)
	check(rows, 8, 4, 1, 0)
	for _, id := range []int{9, 10, 11} {
		check(rows, id, 10, 101, 0)
	}
	check(rows, 12, 10, 501, 1)
	// Permanent ownership and successful play counters gate only their own sources.
	s.tradeShopPurchases[311] = 1
	s.missions[401] = gamestate.MissionProgress{MissionID: 401, State: 2}
	s.ApplyEvolutionRestrictions(NewEvolutionRestrictions([]EvolutionPath{{1, 8}}))
	s.teamBattleScores = map[int]gamestate.TeamBattleScoreProgress{101: {Claimed: 1}}
	s.towerQuestProgress[501] = gamestate.TowerQuestProgress{TowerID: 501, ClearedFloors: []int{1}}
	s.teamBattleSolo = battleCatalogFixture(t, json.RawMessage(strings.Replace(string(s.teamBattleSolo.saved()), `"10":0`, `"10":2`, 1)))
	s.gachas[0].PlayCountMax = 2
	rows = read(nil)
	for _, id := range []int{3, 5, 6, 7, 8, 9, 11, 12} {
		check(rows, id, 0, 0, 0)
	}
	check(rows, 10, 10, 101, 0)
	check(read(map[int]struct{}{}), 10, 0, 0, 0)
	s.disabledTeamBattleBossIDs = map[int]bool{101: true}
	check(read(nil), 10, 0, 0, 0)
	s.tradeShopPurchases[311] = 0
	shop := s.tradeShopProfiles[301]
	shop.StartTime = int(time.Now().Add(time.Hour).Unix())
	s.tradeShopProfiles[301] = shop
	check(read(nil), 6, 0, 0, 0)
	shop.StartTime, shop.EndTime = 0, int(time.Now().Add(-time.Hour).Unix())
	s.tradeShopProfiles[301] = shop
	check(read(nil), 6, 0, 0, 0)
	shop.EndTime, shop.Disabled = 0, true
	s.tradeShopProfiles[301] = shop
	check(read(nil), 6, 0, 0, 0)
	shop.Disabled, shop.Lineups[0].Disabled = false, true
	s.tradeShopProfiles[301] = shop
	check(read(nil), 6, 0, 0, 0)
	shop.Lineups[0].Disabled = false
	s.tradeShopProfiles[301] = shop
	check(read(nil), 6, 8, 301, 0)
	// A temporarily unaffordable source remains useful; only its own
	// publication, schedule, prerequisite and reward state determine visibility.
	s.missions[401] = gamestate.MissionProgress{MissionID: 401}
	for _, definition := range []gamestate.MissionDefinition{
		{MissionID: 401, Rewards: []gamestate.Reward{card(7)}},
		{MissionID: 401, Published: true, StartsAt: time.Now().Add(time.Hour).Unix(), Rewards: []gamestate.Reward{card(7)}},
		{MissionID: 401, Published: true, EndsAt: time.Now().Add(-time.Hour).Unix(), Rewards: []gamestate.Reward{card(7)}},
		{MissionID: 401, Published: true, PrerequisiteID: 402, Rewards: []gamestate.Reward{card(7)}},
	} {
		definition.Title = "任务"
		s.missionCatalog = missionCatalogFixture(t, map[int]gamestate.MissionDefinition{
			401: definition, 402: {MissionID: 402, Title: "前置任务", Rewards: []gamestate.Reward{card(13)}},
		})
		check(read(nil), 7, 0, 0, 0)
	}
}

func TestModernCardSourcesUsePublishedDestinationKinds(t *testing.T) {
	card := func(id int) gamestate.Reward { return gamestate.Reward{Type: 6, RewardTypeID: id, Num: 1} }
	s := &Account{onboarding: completedTestOnboarding(),
		cardDefinitions: map[int]gamestate.Card{},
		cards:           []CardInfo{{UniqueID: 1, CardID: 200}},
		teamBattleSolo: battleCatalogFixture(t, json.RawMessage(`{
			"9":[{"0":100001,"1":0,"9":100001,"4":"主线","10":[{"0":10000101,"4":"一","10":0,"26":0}]}],
			"10":[
				{"0":200001,"1":11,"9":200001,"4":"地图","10":[{"0":20000101,"4":"二","10":0,"26":0}]},
				{"0":3005602,"1":19,"9":0,"4":"炼金","10":[{"0":300560201,"4":"三","10":0,"26":0}]},
				{"0":3005902,"1":20,"9":0,"4":"炼金SP","10":[{"0":300590201,"4":"四","10":0,"26":0}]}
			],"11":[],"12":[]
		}`)),
		stageQuests:    map[int]json.RawMessage{100001: publicationArea(100001, 0)},
		towerAdmission: TowerAdmission{TowerIDs: map[int]struct{}{501: {}, 1056: {}}},
		towerQuestProfiles: map[int]gamestate.TowerQuestProfile{
			501:  {TowerID: 501, Name: "通常塔", Floors: []gamestate.TowerQuestFloorProfile{{Floor: 1, Boss: json.RawMessage(`{"0":50101}`)}}},
			1056: {TowerID: 1056, StageType: gamestate.BattleStageAlchemist, Name: "炼金塔", Floors: []gamestate.TowerQuestFloorProfile{{Floor: 1, Boss: json.RawMessage(`{"0":105601}`)}}},
		},
		towerQuestProgress: map[int]gamestate.TowerQuestProgress{501: {TowerID: 501}, 1056: {TowerID: 1056}},
		cardActions:        gamestate.CardActionState{EvolutionTransitions: []gamestate.EvolutionTransition{{FromCardID: 200, ToCardID: 7, Type: 4}}},
	}
	ids := []int{2, 3, 4, 5, 6, 7, 8}
	for _, id := range ids {
		s.cardDefinitions[id] = gamestate.Card{CardID: id}
	}
	profiles := []gamestate.TeamBattleRewardProfile{
		{BossID: 10000101, StageQuestAreaID: 100001, ResultRewards: []gamestate.Reward{card(2)}},
		{BossID: 20000101, StageQuestAreaID: 200001, ResultRewards: []gamestate.Reward{card(3)}},
		{BossID: 300560201, ResultRewards: []gamestate.Reward{card(4)}},
		{BossID: 50101, TowerID: 501, ResultRewards: []gamestate.Reward{card(5)}},
		{BossID: 105601, TowerID: 1056, ResultRewards: []gamestate.Reward{card(6)}},
		{BossID: 300590201, ResultRewards: []gamestate.Reward{card(8)}},
	}
	result, err := s.HowToGetCards(ids, profiles, CardSourcePolicy{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]howToGetCardEntry{
		2: {Type: 10, Subtype: 5, ContentID: 100001},
		3: {Type: 10, Subtype: 3, ContentID: 200001},
		4: {Type: 10, Subtype: 4, ContentID: 3005602},
		5: {Type: 10, Subtype: 1, ContentID: 501},
		6: {Type: 10, Subtype: 4, ContentID: 1056},
		7: {Type: 15, ContentID: 200},
		8: {Type: 10, Subtype: 4, ContentID: 3005902},
	}
	for _, row := range result {
		expected := want[row.CardID]
		if len(row.GetCards) == 0 {
			t.Fatalf("card %d has no published destination", row.CardID)
		}
		for _, source := range row.GetCards {
			if source.Type != expected.Type || source.Subtype != expected.Subtype || source.ContentID != expected.ContentID {
				t.Fatalf("card %d sources = %+v; want %+v", row.CardID, row.GetCards, expected)
			}
		}
	}
}
