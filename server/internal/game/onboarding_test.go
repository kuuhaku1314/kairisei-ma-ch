package game

import (
	"encoding/json"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func completedTestOnboarding() gamestate.OnboardingState {
	return gamestate.OnboardingState{Step: cnOnboardingStepCount}
}

func TestTutorialRouteFlagPreservesExistingProgress(t *testing.T) {
	s := onboardingTestStore()
	const oldFlags = int64(1)<<26 - 1
	if !s.MergeTutorialFlag(oldFlags) || !s.MergeTutorialFlag(1<<26) || s.TutorialState() != oldFlags|1<<26 {
		t.Fatal("main-quest route guide did not merge with existing tutorial progress")
	}
	if s.MergeTutorialFlag(1<<27) || s.MergeTutorialFlag(-1) || s.Snapshot(gamestate.State{}).User.TutorialFlag != oldFlags|1<<26 {
		t.Fatal("invalid guide bits changed saved tutorial progress")
	}
}

func onboardingTestStore() *Account {
	return &Account{
		currentActiveArthur: 1,
		onboarding:          gamestate.OnboardingState{},
		unlockedFeatureIDs:  map[uint]struct{}{12: {}},
		items: map[int]gamestate.Item{
			2001: {ItemID: 2001},
		},
		itemDefinitions: map[int]gamestate.ItemDefinition{
			2001: {ItemID: 2001, MaxOwned: 9999},
		},
		stackCardTemplates: map[int]gamestate.CardStack{
			20000001: {CardID: 20000001, AddExperience: 100, BaseAddPrice: 1},
			20000011: {CardID: 20000011, AddExperience: 100, BaseAddPrice: 1},
		},
		stackCards: []gamestate.CardStack{},
	}
}

func TestStageQuestAreaCompletionRequiresEveryStage(t *testing.T) {
	configuration := json.RawMessage(`{
		"stage_quest":{"stage_object":[
			{"is_clear_done":1},{"is_clear_done":1},{"is_clear_done":0}
		]}
	}`)
	complete, err := stageQuestAllStagesCleared(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if complete {
		t.Fatal("partially cleared StageQuest area was reported complete")
	}
	configuration = json.RawMessage(`{
		"stage_quest":{"stage_object":[
			{"is_clear_done":1},{"is_clear_done":1},{"is_clear_done":1}
		]}
	}`)
	complete, err = stageQuestAllStagesCleared(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if !complete {
		t.Fatal("fully cleared StageQuest area was not reported complete")
	}
}

func TestUserCreateSelectsTheChosenArthurStarterLeader(t *testing.T) {
	store := &Account{onboarding: completedTestOnboarding(),
		currentActiveArthur: 1,
		cards: []CardInfo{
			{UniqueID: 1, CardID: 10000010},
			{UniqueID: 2, CardID: 10000022},
			{UniqueID: 3, CardID: 10000030},
			{UniqueID: 4, CardID: 10000034},
		},
		decks: []DeckInfo{
			{ArthurType: 1, LeaderCardIndex: 0, CardUniqueIDs: []int64{1}, IsActive: 1},
			{ArthurType: 2, LeaderCardIndex: 0, CardUniqueIDs: []int64{2}, IsActive: 1},
			{ArthurType: 3, LeaderCardIndex: 0, CardUniqueIDs: []int64{3}, IsActive: 1},
			{ArthurType: 4, LeaderCardIndex: 0, CardUniqueIDs: []int64{4}, IsActive: 1},
		},
	}
	if !store.CreateUser("盗贼", 3) {
		t.Fatal("create user failed")
	}
	if len(store.unlockedFeatureIDs) != 1 {
		t.Fatalf("profession feature count = %d, want 1", len(store.unlockedFeatureIDs))
	}
	if _, unlocked := store.unlockedFeatureIDs[2]; !unlocked {
		t.Fatal("chosen thief profession feature was not unlocked")
	}
	uniqueID, cardID, found := store.ActiveArthurLeaderState()
	if !found || uniqueID != 3 || cardID != 10000030 {
		t.Fatalf("selected Arthur leader = (%d, %d, %t)", uniqueID, cardID, found)
	}
}

func TestOnboardingSequencePublishesQuestsRewardsAndFeatures(t *testing.T) {
	store := onboardingTestStore()
	store.tutorialCompletionMail = TutorialCompletionMail{Enabled: true, Title: "毕业礼物", Message: "完成全部训练", Rewards: []gamestate.Reward{
		{Type: 10, Num: 456, CardSkillLevels: []int16{}},
		{Type: 13, RewardTypeID: 20000001, Num: 20, CardSkillLevels: []int16{}},
		{Type: 6, RewardTypeID: 10000010, Num: 2, CardLevel: 60, CardFame: 90, CardLove: 100, CardSkillLevels: []int16{1}},
	}}
	store.cardDefinitions = map[int]gamestate.Card{10000010: {CardID: 10000010, LevelMax: 60, FameMax: 90, LoveMax: 100}}
	quests, err := store.HomeOnboardingQuests()
	if err != nil {
		t.Fatal(err)
	}
	if len(quests) != 1 {
		t.Fatalf("initial quest wrapper = %#v", quests)
	}
	questInfo := quests[0].(map[string]any)
	if len(questInfo["new_quest"].([]any)) != 1 || len(questInfo["now_quest"].([]any)) != 0 {
		t.Fatalf("initial quest publication = %#v", quests)
	}
	quests, err = store.HomeOnboardingQuests()
	if err != nil {
		t.Fatal(err)
	}
	questInfo = quests[0].(map[string]any)
	if len(questInfo["now_quest"].([]any)) != 1 {
		t.Fatalf("repeat quest publication = %#v", quests)
	}

	store.mu.Lock()
	err = store.advanceOnboardingLocked(onboardingEvent{
		kind: "battle", areaID: 100001, stageID: 10000101,
	})
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if store.onboarding.Step != 1 || store.items[2001].Num != 1 {
		t.Fatalf("battle completion state = %+v, ticket = %+v", store.onboarding, store.items[2001])
	}
	if _, unlocked := store.unlockedFeatureIDs[20]; !unlocked {
		t.Fatal("gacha feature was not unlocked")
	}
	quests, err = store.HomeOnboardingQuests()
	if err != nil {
		t.Fatal(err)
	}
	questInfo = quests[0].(map[string]any)
	clearQuests := questInfo["clear_quest"].([]any)
	if len(clearQuests) != 1 || len(questInfo["new_quest"].([]any)) != 1 {
		t.Fatalf("battle-to-gacha publication = %#v", quests)
	}
	clearQuest := clearQuests[0].(map[string]any)
	if _, ok := clearQuest["clear_comment"]; !ok {
		t.Fatalf("clear quest does not use QuestClearInfo contract: %#v", clearQuest)
	}
	clearRewards := clearQuest["reward"].([]any)
	if len(clearRewards) != 1 {
		t.Fatalf("clear quest result rewards = %#v", clearRewards)
	}
	if _, ok := clearRewards[0].(map[string]any)["reward"]; !ok {
		t.Fatalf("clear quest reward does not use ResultRewardInfo: %#v", clearRewards[0])
	}
	if got := clearQuest["feature_flag"].(int64); got != 0 {
		t.Fatalf("clear quest presentation feature flag = %d, want 0", got)
	}

	store.mu.Lock()
	err = store.advanceOnboardingLocked(onboardingEvent{kind: "gacha", gachaID: cnOnboardingGachaID})
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if store.onboarding.Step != 2 || len(store.stackCards) != 1 ||
		store.stackCards[0].CardID != 20000001 || store.stackCards[0].Num != 1 {
		t.Fatalf("gacha completion state = %+v, stack = %+v", store.onboarding, store.stackCards)
	}
	if _, unlocked := store.unlockedFeatureIDs[16]; !unlocked {
		t.Fatal("fusion feature was not unlocked")
	}

	store.mu.Lock()
	err = store.advanceOnboardingLocked(onboardingEvent{kind: "fusion"})
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if store.coinFree != 250 {
		t.Fatalf("fusion completion crystals = %d, want 250", store.coinFree)
	}
	store.mu.Lock()
	err = store.advanceOnboardingLocked(onboardingEvent{kind: "deck"})
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if len(store.stackCards) != 2 || store.stackCards[1].CardID != 20000011 || store.stackCards[1].Num != 1 {
		t.Fatalf("deck completion stack reward = %+v", store.stackCards)
	}
	quests, err = store.HomeOnboardingQuests()
	if err != nil {
		t.Fatal(err)
	}
	questInfo = quests[0].(map[string]any)
	areaQuests := questInfo["new_quest"].([]any)
	if len(areaQuests) != 1 ||
		areaQuests[0].(map[string]any)["description"] != "通关『大地起源』中的所有关卡" {
		t.Fatalf("post-deck objective = %#v", quests)
	}
	store.mu.Lock()
	err = store.advanceOnboardingLocked(onboardingEvent{kind: "explore"})
	store.mu.Unlock()
	if err != nil || store.onboarding.Step != 4 {
		t.Fatal("Explore bypassed the first-area objective")
	}
	store.mu.Lock()
	err = store.advanceOnboardingLocked(onboardingEvent{kind: "battle_area", areaID: 100001, stageID: 10000103})
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if store.onboarding.Step != 5 || store.coinFree != 400 {
		t.Fatalf("first-area completion state = %+v, crystals = %d", store.onboarding, store.coinFree)
	}
	store.mu.Lock()
	err = store.advanceOnboardingLocked(onboardingEvent{kind: "gacha", gachaID: cnOnboardingMultiGachaID})
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if store.onboarding.Step != 6 {
		t.Fatal("the client-reserved first multi-draw did not advance onboarding")
	}
	store.mu.Lock()
	err = store.advanceOnboardingLocked(onboardingEvent{kind: "explore"})
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if store.onboarding.Step != 7 {
		t.Fatal("Explore did not advance onboarding")
	}

	for _, event := range []onboardingEvent{
		{kind: "story"}, {kind: "activity", activityBoss: true},
	} {
		if len(store.presents) != 0 {
			t.Fatal("graduation mail sent before all nine training steps completed")
		}
		store.mu.Lock()
		err = store.advanceOnboardingLocked(event)
		store.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	}
	if store.coinFree != 600 {
		t.Fatalf("tutorial crystals = %d, want 600", store.coinFree)
	}
	if err := store.advanceOnboardingLocked(onboardingEvent{kind: "activity", activityBoss: true}); err != nil {
		t.Fatal(err)
	}
	if store.coinFree != 600 {
		t.Fatal("duplicate completion granted tutorial crystals twice")
	}
	if len(store.presents) != 3 || store.presents[0].PresentID == store.presents[1].PresentID || store.presents[0].IssuedAtUnix <= 0 || store.presents[0].Title != "毕业礼物" || store.presents[0].Comment != "完成全部训练" || store.presents[0].Reward.Num != 456 || store.stackCards[0].Num != 1 || len(store.cards) != 0 {
		t.Fatal("graduation mail missing, duplicated, or applied directly to inventory")
	}
	if result, err := store.ReceivePresent(store.presents[2].PresentID); err != nil || len(result.FailedID) != 1 || store.presents[2].State != 0 {
		t.Fatal("full card bag should keep the graduation card mail claimable")
	}
	if _, err := store.ReceivePresent(store.presents[0].PresentID); err != nil || store.coinFree != 1056 {
		t.Fatal("graduation crystals were not applied on claim")
	}
	if _, err := store.ReceivePresent(store.presents[0].PresentID); err != nil || store.coinFree != 1056 {
		t.Fatal("graduation mail could be claimed twice")
	}
	// The actual account snapshot carries both completion and mail. Loading it
	// and applying current public policy must not send again.
	saved := store.Snapshot(gamestate.State{})
	reloaded := onboardingTestStore()
	reloaded.onboarding = saved.Onboarding
	reloaded.presents = clonePresents(saved.Engagement.Presents)
	reloaded.tutorialCompletionMail = store.tutorialCompletionMail
	if err := reloaded.advanceOnboardingLocked(onboardingEvent{kind: "activity", activityBoss: true}); err != nil || len(reloaded.presents) != 3 {
		t.Fatal("persisted completion retried the graduation delivery")
	}
	reloaded.presents = nil // Account finished before this policy was enabled.
	if err := reloaded.advanceOnboardingLocked(onboardingEvent{kind: "activity", activityBoss: true}); err != nil || len(reloaded.presents) != 0 {
		t.Fatal("completed account received a retroactive graduation mail")
	}
	if store.onboarding.Step != cnOnboardingStepCount {
		t.Fatalf("final onboarding step = %d", store.onboarding.Step)
	}
	for featureID := uint(4); featureID <= 31; featureID++ {
		if _, unlocked := store.unlockedFeatureIDs[featureID]; !unlocked {
			t.Fatalf("feature %d was not unlocked", featureID)
		}
	}
	// Accounts that finished before the menu-unlock fix must recover through
	// the same reconciliation used when a persisted store is loaded.
	delete(store.unlockedFeatureIDs, 25)
	delete(store.unlockedFeatureIDs, 28)
	store.reconcileOnboardingFeatureUnlocksLocked()
	if !store.FeatureUnlocked(25) || !store.FeatureUnlocked(28) || store.FeatureUnlocked(32) || !store.FeatureUnlocked(33) || !store.FeatureUnlocked(36) {
		t.Fatal("completed training did not restore ordinary menus and swords independently of tower")
	}
}

func TestOnboardingIgnoresWrongBattleAndOrdinaryGacha(t *testing.T) {
	store := onboardingTestStore()
	store.mu.Lock()
	if err := store.advanceOnboardingLocked(onboardingEvent{
		kind: "battle", areaID: 100001, stageID: 10000102,
	}); err != nil {
		t.Fatal(err)
	}
	store.mu.Unlock()
	if store.onboarding.Step != 0 || store.items[2001].Num != 0 {
		t.Fatal("wrong first battle advanced onboarding")
	}
}

func TestOnboardingFirstDrawPublishesOnlyTutorialGacha(t *testing.T) {
	store := onboardingTestStore()
	store.onboarding.Step = 1
	store.gachas = []gamestate.GachaProfile{
		{GachaID: 60200011, GroupID: 60200011, CategoryNum: 1, CardNumMax: 1, PayType: 3},
		{GachaID: cnOnboardingGachaID, GroupID: cnOnboardingGachaID, CategoryNum: 4, CardNumMax: 1, PayType: 4, PayTypeID: 2001},
	}
	item := store.items[2001]
	item.Num = 1
	store.items[2001] = item

	gachas := store.GachaState()
	if len(gachas) != 1 || gachas[0].GachaID != cnOnboardingGachaID {
		t.Fatalf("first draw gacha state = %+v, want only the tutorial pool", gachas)
	}
}

func TestOnboardingFirstMultiDrawPublishesOnlyReservedGacha(t *testing.T) {
	store := onboardingTestStore()
	store.gachas = []gamestate.GachaProfile{
		{GachaID: 60200011, GroupID: 60200011, CategoryNum: 1, CardNumMax: 1, PayType: 3},
		{GachaID: cnOnboardingGachaID, GroupID: cnOnboardingGachaID, CategoryNum: 4, CardNumMax: 1, PayType: 4},
		{GachaID: cnOnboardingMultiGachaID, GroupID: cnOnboardingMultiGachaID, CategoryNum: 4, CardNum: 11, CardNumMax: 11, PayType: 3},
	}

	for _, step := range []int{2, 3, 4, 5} {
		store.onboarding.Step = step
		gachas := store.GachaState()
		if len(gachas) != 1 || gachas[0].GachaID != cnOnboardingMultiGachaID {
			t.Fatalf("step %d first multi-draw gacha state = %+v, want only the reserved multi pool", step, gachas)
		}
	}

	store.onboarding.Step = 2
	if store.GachaAvailableForPlayLocked(cnOnboardingGachaID) ||
		store.GachaAvailableForPlayLocked(cnOnboardingMultiGachaID) ||
		store.GachaAvailableForPlayLocked(60200011) {
		t.Fatal("intermediate tutorial step allowed a gacha play before the first area was complete")
	}
	store.onboarding.Step = 5
	if store.GachaAvailableForPlayLocked(cnOnboardingGachaID) ||
		!store.GachaAvailableForPlayLocked(cnOnboardingMultiGachaID) ||
		store.GachaAvailableForPlayLocked(60200011) {
		t.Fatal("onboarding gacha availability does not match the active multi-draw step")
	}
	store.onboarding.Step = 6
	gachas := store.GachaState()
	if len(gachas) != 1 || gachas[0].GachaID != 60200011 ||
		!store.GachaAvailableForPlayLocked(60200011) ||
		store.GachaAvailableForPlayLocked(cnOnboardingGachaID) ||
		store.GachaAvailableForPlayLocked(cnOnboardingMultiGachaID) {
		t.Fatalf("completed onboarding did not switch exclusively to normal pools: %+v", gachas)
	}
}

func TestGachaPoolForRarityUsesCardDefinitions(t *testing.T) {
	store := &Account{onboarding: completedTestOnboarding(), cardDefinitions: map[int]gamestate.Card{
		10: {CardID: 10, RarityRank: 4},
		20: {CardID: 20, RarityRank: 5},
		30: {CardID: 30, RarityRank: 4},
	}}
	ids, weights, err := store.gachaPoolForRarityLocked([]int{10, 20, 30}, []int{2, 3, 5}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != 10 || ids[1] != 30 || weights[0] != 2 || weights[1] != 5 {
		t.Fatalf("rarity-filtered pool = %v / %v", ids, weights)
	}
}
