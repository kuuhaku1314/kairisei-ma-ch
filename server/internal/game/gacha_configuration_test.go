package game

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestGachaGroupLimitCountsRequestsAcrossVariants(t *testing.T) {
	pool := []gamestate.WeightedReward{{Weight: 1, Reward: gamestate.Reward{Type: 8, RewardTypeID: 10, Num: 1, CardSkillLevels: []int16{}}}}
	profiles := []gamestate.GachaProfile{
		{GachaID: 11, GroupID: 7, PayType: 3, Price: 1, CardNum: 1, CardNumMax: 1, PlayCountMax: 2, RewardPool: pool},
		{GachaID: 12, GroupID: 7, PayType: 3, Price: 10, CardNum: 10, CardNumMax: 10, PlayCountMax: 2, RewardPool: pool},
	}
	s := &Account{gachas: gamestate.CloneGachas(profiles), coinFree: 100, items: map[int]gamestate.Item{}, itemDefinitions: map[int]gamestate.ItemDefinition{10: {ItemID: 10, MaxOwned: 1000}}}
	if _, err := s.PlayGacha(12, 4, nil); err == nil || s.gachaGroupPlaysLocked()[7] != 0 {
		t.Fatal("failed request used quota")
	}
	if _, err := s.PlayGacha(12, 3, nil); err != nil {
		t.Fatal(err)
	}
	if s.items[10].Num != 10 || s.gachaGroupPlaysLocked()[7] != 1 || len(s.GachaState()) != 2 {
		t.Fatal("ten-draw must consume exactly one group play")
	}
	// Existing serialized per-ID counters alone reconstruct the shared quota.
	raw, err := json.Marshal(s.gachas)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &s.gachas); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PlayGacha(11, 3, nil); err != nil {
		t.Fatal(err)
	}
	if len(s.GachaState()) != 0 || s.gachaGroupPlaysLocked()[7] != 2 {
		t.Fatal("whole group remains after its shared limit")
	}
	before := s.coinFree
	for _, id := range []int{11, 12} {
		if _, err = s.PlayGacha(id, 3, nil); err == nil || s.coinFree != before {
			t.Fatal("stale variant request bypassed shared limit")
		}
	}
	// Two simultaneous requests against the final group slot cannot both commit.
	s.gachas[0].PlayCount = 0
	s.gachas[1].PlayCount = 1
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []int{11, 12} {
		wg.Add(1)
		go func(id int) { defer wg.Done(); _, e := s.PlayGacha(id, 3, nil); results <- e }(id)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 || s.gachaGroupPlaysLocked()[7] != 2 {
		t.Fatal("concurrent draws overran group quota")
	}
	// The daily quota keeps the row visible and limits all draw methods.
	for i := range s.gachas {
		s.gachas[i].PlayCountMax, s.gachas[i].DailyPlayMax = 0, 1
	}
	if visible := s.GachaState(); len(visible) != 2 || !visible[0].DailyExhausted() || !visible[1].DailyExhausted() {
		t.Fatalf("daily quota was not shared or hid its row: %+v", visible)
	}
	before = s.coinFree
	if _, err := s.PlayGacha(11, 3, nil); err != errGachaDailyLimit || s.coinFree != before {
		t.Fatalf("daily limit did not block a stale variant before charging: %v", err)
	}
	for id, claim := range s.gachaDailyClaims {
		claim.Day = gachaLocalDayKey(time.Now().Add(-24 * time.Hour))
		s.gachaDailyClaims[id] = claim
	}
	if _, err := s.PlayGacha(12, 3, nil); err != nil || s.GachaState()[0].DailyGroupPlays != 1 {
		t.Fatalf("new business day did not reset quota: %v", err)
	}
}

func TestGachaStepPaymentUsesCurrentStage(t *testing.T) {
	pool := []gamestate.WeightedReward{{Weight: 1, Reward: gamestate.Reward{Type: 8, RewardTypeID: 10, Num: 1, CardSkillLevels: []int16{}}}}
	p := gamestate.GachaProfile{GachaID: 11, GroupID: 7, PayType: 3, Price: 1, CardNum: 1, CardNumMax: 1, RewardPool: pool, Steps: []gamestate.GachaStep{{Price: 1, RewardPool: pool}, {PayType: 4, PayTypeID: 99, Price: 2, RewardPool: pool}}}
	s := &Account{gachas: []gamestate.GachaProfile{p}, coinFree: 20, items: map[int]gamestate.Item{99: {ItemID: 99, Num: 4}}, itemDefinitions: map[int]gamestate.ItemDefinition{10: {ItemID: 10, MaxOwned: 100}, 99: {ItemID: 99, MaxOwned: 100}}}
	if _, err := s.PlayGacha(11, 3, nil); err != nil {
		t.Fatal(err)
	}
	visible := s.GachaState()
	if visible[0].PayType != 4 || visible[0].PayTypeID != 99 || visible[0].Price != 2 {
		t.Fatal("next stage advertises old payment")
	}
	if _, err := s.PlayGacha(11, 3, nil); err == nil || s.coinFree != 19 || s.gachas[0].PlayCount != 1 {
		t.Fatal("old currency accepted after stage change")
	}
	if _, err := s.PlayGacha(11, 4, nil); err != nil || s.items[99].Num != 2 || s.coinFree != 19 {
		t.Fatal("stage item not charged correctly", err)
	}
}

func TestGachaVisibilityRetainsAllProfessionChoices(t *testing.T) {
	s := &Account{}
	for job := int8(1); job <= 5; job++ {
		s.gachas = append(s.gachas, gamestate.GachaProfile{GachaID: 10 + int(job), GroupID: 7, ArthurType: job, PayType: 3, Price: 1, CardNum: 1, CardNumMax: 1, PlayCountMax: 1, CardIDs: []int{100}, CardWeights: []int{1}})
	}
	if len(s.GachaState()) != 5 {
		t.Fatal("single-draw preference collapsed profession choices")
	}
	s.gachas[2].PlayCount = 1
	if len(s.GachaState()) != 0 {
		t.Fatal("other profession choices escaped the shared one-time limit")
	}
}

func TestGachaConfigurationPreservesPlayerStateAndScheduledPaymentFallback(t *testing.T) {
	profiles := []gamestate.GachaProfile{
		{GachaID: 11, GroupID: 1, PayType: 4, PayTypeID: 99, Price: 1, PlayCount: 7, CardNum: 1, CardNumMax: 1, CardIDs: []int{100}, CardWeights: []int{1}},
		{GachaID: 12, GroupID: 1, PayType: 3, Price: 5, CardNum: 1, CardNumMax: 1, CardIDs: []int{100}, CardWeights: []int{1}},
	}
	s := &Account{gachas: CloneGachaProfiles(profiles), items: map[int]gamestate.Item{99: {Num: 5}}, gachaDailyClaims: map[int]gamestate.GachaDailyClaim{11: {GachaID: 11, Day: "2026-09-06", PlayCount: 1}}, gachaSelections: map[int][]gamestate.Reward{}}
	configured := profiles[0]
	configured.Price = 2
	configured.CardIDs = []int{200}
	configured.PlayCount = 0
	s.ApplyGachaConfiguration(1, []GachaConfiguration{{Profile: configured, StartUnix: time.Now().Add(time.Hour).Unix()}})
	if s.gachas[0].Price != 2 || s.gachas[0].PlayCount != 7 || s.gachaDailyClaims[11].Day != "2026-09-06" || s.gachas[0].CardIDs[0] != 200 {
		t.Fatal("operator config overwrote player state or did not update the pool")
	}
	visible := s.VisibleGachasLocked()
	if len(visible) != 1 || visible[0].GachaID != 12 {
		t.Fatalf("scheduled ticket hid the usable crystal alternative: %+v", visible)
	}
	if _, err := s.PlayGacha(11, 4, nil); err == nil || s.items[99].Num != 5 || s.gachas[0].PlayCount != 7 {
		t.Fatal("future pool allowed payment")
	}
	s.ApplyGachaConfiguration(2, []GachaConfiguration{{Profile: configured, EndUnix: time.Now().Add(-time.Hour).Unix()}})
	if s.GachaAvailableForPlayLocked(11) {
		t.Fatal("expired pool allowed payment")
	}
	s.ApplyGachaConfiguration(3, []GachaConfiguration{{Profile: configured}})
	visible = s.VisibleGachasLocked()
	if len(visible) != 1 || visible[0].GachaID != 11 {
		t.Fatalf("active ticket did not regain priority: %+v", visible)
	}
	s.ApplyGachaConfiguration(4, []GachaConfiguration{{Profile: configured, Disabled: true}})
	visible = s.VisibleGachasLocked()
	if len(visible) != 1 || visible[0].GachaID != 12 || s.GachaAvailableForPlayLocked(11) || s.gachas[0].PlayCount != 7 {
		t.Fatal("disabled single draw did not fall back without changing player count")
	}
	// In ID order the ticket is first and the substitute single follows the ten-draw.
	// An expired ticket with a positive balance must leave two usable buttons.
	multi := profiles[1]
	multi.GachaID, multi.CardNum, multi.CardNumMax = 13, 10, 10
	s.gachas = []gamestate.GachaProfile{profiles[0], multi, profiles[1]}
	s.gachaWindows = nil
	s.coinFree = 10
	s.items[99] = gamestate.Item{Num: 5, LimitTime: int(time.Now().Add(-time.Hour).Unix())}
	visible = s.GachaState()
	if len(visible) != 2 || visible[0].GachaID != 12 || visible[1].GachaID != 13 {
		t.Fatalf("expired ticket hid the usable single draw or its button: %+v", visible)
	}
}

func TestGachaStepsCommitWithLimitedGifts(t *testing.T) {
	pool := []gamestate.WeightedReward{{Weight: 1, Reward: gamestate.Reward{Type: 8, RewardTypeID: 10, Num: 3, CardSkillLevels: []int16{}}}}
	profile := gamestate.GachaProfile{GachaID: 7, GroupID: 7, PayType: 3, Price: 1, CardNum: 1, CardNumMax: 1,
		RewardPool: pool, Steps: []gamestate.GachaStep{{Price: 1, RewardPool: pool}, {Price: 2, RewardPool: pool}},
		Gifts: []gamestate.GachaGiftRule{{FromPlay: 1, ToPlay: 5, Rewards: []gamestate.Reward{{Type: 8, RewardTypeID: 9, Num: 1, CardSkillLevels: []int16{}}}}}}
	s := &Account{gachas: gamestate.CloneGachas([]gamestate.GachaProfile{profile}), items: map[int]gamestate.Item{10: {ItemID: 10, Num: 20}},
		itemDefinitions: map[int]gamestate.ItemDefinition{9: {ItemID: 9, MaxOwned: 5}, 10: {ItemID: 10, MaxOwned: 100}}}
	if _, err := s.PlayGacha(7, 3, nil); err == nil || s.gachas[0].PlayCount != 0 || s.gold != 0 {
		t.Fatal("failed payment advanced the step")
	}
	s.coinFree = 100
	for play := 1; play <= 6; play++ {
		result, err := s.PlayGacha(7, 3, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Reward.Rewards) != 1 || len(result.PresentGifts) != min(1, 6-play) {
			t.Fatal("gift was lost or animated as an extra draw")
		}
		// Simulate a serialization boundary: counters persist, publication rules do not change.
		s.gachas = gamestate.CloneGachas(s.gachas)
	}
	if s.coinFree != 89 || s.items[10].Num != 38 || s.items[9].Num != 0 || len(s.presents) != 5 || s.gachas[0].PlayCount != 6 || profile.PlayCount != 0 {
		t.Fatal("step price, gift limit, or account isolation failed")
	}
	giftID := s.presents[0].PresentID
	if claim, err := s.ReceivePresent(giftID); err != nil || len(claim.PresentID) != 1 || s.items[9].Num != 1 {
		t.Fatal("claiming the gift did not credit inventory", err)
	}
	if claim, err := s.ReceivePresent(giftID); err != nil || len(claim.PresentID) != 0 || s.items[9].Num != 1 {
		t.Fatal("claiming a second time duplicated the gift", err)
	}
	// A full gift inventory must still allow the draw and create the gift mail.
	s.items[9] = gamestate.Item{ItemID: 9, Num: 5}
	s.gachas[0].PlayCount = 0
	result, err := s.PlayGacha(7, 3, nil)
	if err != nil || s.coinFree != 88 || len(result.PresentGifts) != 1 ||
		len(s.presents) != 6 || s.presents[5].Reward.RewardTypeID != 9 || len(result.Reward.Rewards) != 1 || result.Reward.Rewards[0].InPresentBox {
		t.Fatalf("full gift inventory blocked or duplicated draw: %v", err)
	}
	// Operator switches this pool to a named item. Insufficient items cannot
	// charge crystals or advance the stage, and a valid draw spends that item only.
	configured := profile
	configured.PayType, configured.PayTypeID = 4, 4000
	s.ApplyGachaConfiguration(1, []GachaConfiguration{{Profile: configured}})
	if _, err := s.PlayGacha(7, 3, nil); err == nil || s.coinFree != 88 {
		t.Fatal("old payment type charged after configuration changed")
	}
	if _, err := s.PlayGacha(7, 4, nil); err == nil || s.coinFree != 88 || s.gachas[0].PlayCount != 1 {
		t.Fatal("insufficient configured item spent money or advanced the step")
	}
	s.items[4000] = gamestate.Item{ItemID: 4000, Num: 10}
	if _, err := s.PlayGacha(7, 4, nil); err != nil || s.items[4000].Num != 8 || s.coinFree != 88 || s.gachas[0].PlayCount != 2 {
		t.Fatal("configured item or step price was not charged correctly", err)
	}
}

func TestMixedGachaExpectancyComparesPresentationGrades(t *testing.T) {
	s := &Account{
		cardDefinitions:  map[int]gamestate.Card{1: {RarityRank: 5}},
		buddyDefinitions: map[int]gamestate.BuddyDefinition{2: {Rarity: "LEGEND"}},
		sphereDefinitions: map[int]gamestate.SphereDefinition{
			3: {Type: "CHALICE", Rarity: "NORMAL"},
			4: {Type: "CHALICE", Rarity: "MILLIONRARE"},
		},
	}
	card := gamestate.Reward{Type: 6, RewardTypeID: 1, Num: 1}
	buddy := gamestate.Reward{Type: 19, RewardTypeID: 2, Num: 1}
	normal := gamestate.Reward{Type: 15, RewardTypeID: 3, Num: 1}
	rare := gamestate.Reward{Type: 15, RewardTypeID: 4, Num: 1}
	for _, tc := range []struct {
		rewards []gamestate.Reward
		want    int
	}{
		{[]gamestate.Reward{normal}, 8},
		{[]gamestate.Reward{normal, card}, 4},
		{[]gamestate.Reward{card, rare}, 10},
		{[]gamestate.Reward{normal, buddy, rare}, 7},
		{[]gamestate.Reward{rare, buddy, normal}, 7},
	} {
		if got := s.gachaMixedResultExpectancy(tc.rewards); got != tc.want {
			t.Fatalf("mixed expectancy = %d, want %d", got, tc.want)
		}
	}
}

func TestOperatorGachaPoolIsAppendedWithStoredPlays(t *testing.T) {
	s := &Account{operatorGachaPlays: map[int]int{60300001: 3}}
	pool := gamestate.GachaProfile{GachaID: 60300001, GroupID: 60300001, CardNum: 1, CardNumMax: 1, CardIDs: []int{1}, CardWeights: []int{1}}
	s.ApplyGachaConfiguration(1, []GachaConfiguration{{Operator: true, Profile: pool}})
	pool.Name = "renamed"
	s.ApplyGachaConfiguration(2, []GachaConfiguration{{Operator: true, Disabled: true, Profile: pool}})
	if len(s.gachas) != 1 || s.gachas[0].PlayCount != 3 || s.gachas[0].Name != "renamed" || s.gachaScheduledLocked(60300001) || len(s.operatorGachaPlays) != 0 {
		t.Fatalf("operator pool: %+v plays=%v", s.gachas, s.operatorGachaPlays)
	}
	s.ApplyGachaConfiguration(3, nil)
	if len(s.gachas) != 0 || len(s.GachaState()) != 0 {
		t.Fatal("retired operator pool reappeared after removing configuration")
	}
}
