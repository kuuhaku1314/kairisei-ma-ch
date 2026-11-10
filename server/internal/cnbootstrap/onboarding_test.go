package cnbootstrap

import (
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
)

func TestFirstAccountUsesOnboardingAndPreservesProgressOnReload(t *testing.T) {
	storage, err := accountstore.OpenDatabase(filepath.Join(t.TempDir(), "save.json"),
		filepath.Join("..", "..", "config", "cn602-save-template.json"),
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := storage.Close(); err != nil {
			t.Error(err)
		}
	})
	state, err := storage.LoadOrImport()
	if err != nil {
		t.Fatal(err)
	}
	if state.User.Name != "" || state.User.TutorialFlag != 0 || len(state.Cards) != 10 ||
		state.Onboarding.Step != 0 ||
		state.User.CardMax != gamestate.CardCapacityLimit || state.User.CardContainerMax != gamestate.CardContainerCapacityDefault || state.User.NaviID != 0 || state.User.NaviUnlockFlag != 1 || !slices.Equal(state.User.SelectableNaviIDs, []int8{0}) || slices.Contains(state.User.UnlockedFeatureIDs, uint(10)) {
		t.Fatal("fresh primary account did not enter clean training")
	}
	state.User.Name = "保留玩家名字"
	state.User.Gold = 123
	state.User.PVPPoint = 321
	state.PVP.Challenge, state.PVP.NextBattleID = 0, 42
	state.LoginBonus = gamestate.LoginBonusState{LastClaimDay: "2026-11-10", CycleDay: 1, BeginnerDay: 1, TotalClaims: 1}
	loginProgress := state.LoginBonus
	state.User.NaviID = 1 // A player's later choice must survive reload.
	state.User.NaviUnlockFlag = 3
	state.User.SelectableNaviIDs = []int8{0, 1}
	state.Onboarding.Step = gamestate.OnboardingStepCount
	state.User.UnlockedFeatureIDs = append(state.User.UnlockedFeatureIDs, 10)
	if err := storage.Persist(state); err != nil {
		t.Fatal(err)
	}
	state, err = storage.LoadOrImport()
	if err != nil {
		t.Fatal(err)
	}
	if state.PVP.Challenge != 0 || state.PVP.NextBattleID != 42 || state.User.PVPPoint != 321 || state.LoginBonus != loginProgress {
		t.Fatal("reload reset PvP or login reward progress")
	}
	if state.User.Name != "保留玩家名字" || state.User.Gold != 123 || state.User.NaviID != 1 ||
		state.Onboarding.Step != gamestate.OnboardingStepCount || !slices.Contains(state.User.UnlockedFeatureIDs, uint(10)) {
		t.Fatal("reload reset player progress or left encyclopedia locked")
	}
}

func TestInitializeCNOnboardingSnapshot(t *testing.T) {
	state, err := accountstore.LoadSaveState(filepath.Join("..", "..", "config", "cn602-save-template.json"))
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}
	if err := accountstore.InitializeOnboardingSnapshot(&state, 100000123); err != nil {
		t.Fatalf("initialize onboarding: %v", err)
	}
	if state.User.Name != "" || state.User.UserID != 100000123 ||
		state.User.TutorialFlag != 0 || state.User.Gold != 0 || state.User.CoinFree != 0 {
		t.Fatalf("clean user state was not installed: %+v", state.User)
	}
	if state.Onboarding.Step != 0 {
		t.Fatalf("onboarding state = %+v", state.Onboarding)
	}
	if slices.Contains(state.User.UnlockedFeatureIDs, uint(0)) ||
		slices.Contains(state.User.UnlockedFeatureIDs, uint(1)) ||
		slices.Contains(state.User.UnlockedFeatureIDs, uint(2)) ||
		slices.Contains(state.User.UnlockedFeatureIDs, uint(3)) {
		t.Fatalf("clean account pre-opened profession features: %v", state.User.UnlockedFeatureIDs)
	}
	if len(state.Cards) != 10 || len(state.ContainerCards) != 0 || len(state.StackCards) != 0 {
		t.Fatalf("inventory sizes = cards %d, container %d, stack %d", len(state.Cards), len(state.ContainerCards), len(state.StackCards))
	}
	actualCardIDs := make([]int, len(state.Cards))
	for index, card := range state.Cards {
		actualCardIDs[index] = card.CardID
	}
	if !slices.Equal(actualCardIDs, accountstore.StarterCardIDs) {
		t.Fatalf("starter cards = %v, want %v", actualCardIDs, accountstore.StarterCardIDs)
	}
	if len(state.Decks) != 4 {
		t.Fatalf("deck count = %d, want 4", len(state.Decks))
	}
	for _, deck := range state.Decks {
		if len(deck.CardUniqueIDs) != 10 || deck.LeaderCardIndex != 0 || deck.ArthurType != deck.JobType ||
			len(deck.BuddyUniqueIDs) != 5 || slices.ContainsFunc(deck.BuddyUniqueIDs, func(uniqueID int64) bool { return uniqueID != 0 }) ||
			deck.Name != accountstore.DefaultDeckNameByArthur[deck.ArthurType] {
			t.Fatalf("invalid starter deck: %+v", deck)
		}
		leaderCardID := 0
		for _, card := range state.Cards {
			if card.UniqueID == deck.CardUniqueIDs[0] {
				leaderCardID = card.CardID
				break
			}
		}
		if leaderCardID != accountstore.StarterLeaderCardIDByArthur[deck.ArthurType] {
			t.Fatalf("Arthur %d leader = %d", deck.ArthurType, leaderCardID)
		}
	}
	if len(state.Buddies) != 0 || state.Buddy.UniqueID != 0 {
		t.Fatalf("fresh account buddy inventory is not empty: buddy=%+v buddies=%+v", state.Buddy, state.Buddies)
	}
	if len(state.Engagement.Missions) != 0 || len(state.Engagement.Presents) != 0 ||
		len(state.TeamBattleResultReceipts) != 0 || state.ActiveTeamBattle != nil {
		t.Fatal("fresh account has engagement or battle progress")
	}
	foundTutorialGacha := false
	foundTutorialMultiGacha := false
	for _, gacha := range state.Gachas {
		switch gacha.GachaID {
		case gamestate.OnboardingGachaID:
			foundTutorialGacha = true
			if gacha.PayTypeID != 2001 || !slices.Equal(gacha.CardIDs, []int{10002001}) {
				t.Fatalf("tutorial gacha = %+v", gacha)
			}
		case gamestate.OnboardingMultiGachaID:
			foundTutorialMultiGacha = true
			if gacha.PayType != 3 || gacha.Price != 400 || gacha.CardNum != 11 ||
				gacha.GuaranteedRarityRank != 5 || gacha.GuaranteedCount != 1 ||
				gacha.RemainderRarityRank != 4 {
				t.Fatalf("tutorial multi gacha = %+v", gacha)
			}
			if !slices.Equal(gacha.CardIDs, []int{10000013, 10000025, 10000033, 10000037,
				10000058, 10000087, 10000111, 10000129, 10000133, 10000171, 10001008, 10001014}) ||
				len(gacha.CardWeights) != len(gacha.CardIDs) || slices.ContainsFunc(gacha.CardWeights, func(w int) bool { return w != 1 }) {
				t.Fatalf("tutorial lineup inherited the standard pool: %+v", gacha)
			}
		}
	}
	if !foundTutorialGacha || !foundTutorialMultiGacha {
		t.Fatal("one or more client-reserved tutorial gachas are missing")
	}
	if gamestate.OnboardingGachaID != 90000200 || gamestate.OnboardingMultiGachaID != 90000100 {
		t.Fatalf("tutorial gacha IDs = %d/%d, want client-reserved single/multi IDs", gamestate.OnboardingGachaID, gamestate.OnboardingMultiGachaID)
	}
	for index := range state.Gachas {
		if state.Gachas[index].GachaID == gamestate.OnboardingMultiGachaID {
			state.Gachas[index].PlayCount = 1
			state.Gachas[index].CardIDs = []int{99990100}
		}
	}
	if err := accountstore.InstallOnboardingGacha(&state); err != nil {
		t.Fatal(err)
	}
	for _, gacha := range state.Gachas {
		if gacha.GachaID == gamestate.OnboardingMultiGachaID &&
			(gacha.PlayCount != 1 || slices.Contains(gacha.CardIDs, 99990100)) {
			t.Fatal("updating tutorial lineup reset the completed draw or retained an old prize")
		}
	}
}
