package accountstore

import (
	"errors"
	"fmt"
	"sort"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
)

var StarterCardIDs = []int{
	10000010, 10000018, 10000022, 10000026, 10000030,
	10000034, 10000038, 10000042, 10000046, 10000050,
}

var StarterLeaderCardIDByArthur = map[int8]int{
	1: 10000010,
	2: 10000022,
	3: 10000030,
	4: 10000034,
}

var DefaultDeckNameByArthur = map[int8]string{
	1: "佣兵卡组1",
	2: "富豪卡组1",
	3: "盗贼卡组1",
	4: "歌姬卡组1",
}

// InitializeOnboardingSnapshot selects starter cards from the shared catalog
// and initializes player-owned state. The ten-card identity is INFERRED from the seed
// and the user-provided original-flow screenshot; the quest IDs, ticket 2001
// and first draw card 10002001 come from the official CN client/master contract.
func InitializeOnboardingSnapshot(state *gamestate.State, userID int) error {
	if state == nil || userID <= 0 {
		return errors.New("CN onboarding snapshot identity is invalid")
	}
	startersByCardID := make(map[int]gamestate.Card, len(StarterCardIDs))
	for _, card := range state.Cards {
		for _, starterCardID := range StarterCardIDs {
			if card.CardID == starterCardID {
				if _, duplicate := startersByCardID[card.CardID]; !duplicate {
					startersByCardID[card.CardID] = card
				}
				break
			}
		}
	}
	if len(startersByCardID) != len(StarterCardIDs) {
		return errors.New("CN onboarding seed does not contain the ten starter cards")
	}
	state.Cards = make([]gamestate.Card, 0, len(StarterCardIDs))
	starterUniqueIDByCardID := make(map[int]int64, len(StarterCardIDs))
	for _, cardID := range StarterCardIDs {
		card := startersByCardID[cardID]
		state.Cards = append(state.Cards, card)
		starterUniqueIDByCardID[cardID] = card.UniqueID
	}
	state.ContainerCards = []gamestate.Card{}
	state.StackCards = []gamestate.CardStack{}
	state.Spheres = []gamestate.Sphere{}
	state.BattleLoadoutCardUniqueIDs = make([]int64, 0, len(state.Cards))
	for _, card := range state.Cards {
		state.BattleLoadoutCardUniqueIDs = append(state.BattleLoadoutCardUniqueIDs, card.UniqueID)
	}

	decksByArthur := make(map[int8]gamestate.Deck, 4)
	for _, deck := range state.Decks {
		if deck.ArthurType >= 1 && deck.ArthurType <= 4 {
			if _, exists := decksByArthur[deck.ArthurType]; !exists || deck.IsActive != 0 {
				decksByArthur[deck.ArthurType] = deck
			}
		}
	}
	state.Decks = make([]gamestate.Deck, 0, 4)
	for arthurType := int8(1); arthurType <= 4; arthurType++ {
		deck, exists := decksByArthur[arthurType]
		if !exists {
			return fmt.Errorf("CN onboarding seed has no Arthur type %d deck", arthurType)
		}
		leaderCardID := StarterLeaderCardIDByArthur[arthurType]
		deck.ArthurType = arthurType
		deck.JobType = arthurType
		deck.Index = 0
		deck.LeaderCardIndex = 0
		deck.CardUniqueIDs = []int64{starterUniqueIDByCardID[leaderCardID]}
		for _, cardID := range StarterCardIDs {
			if cardID != leaderCardID {
				deck.CardUniqueIDs = append(deck.CardUniqueIDs, starterUniqueIDByCardID[cardID])
			}
		}
		deck.SupportCardUniqueIDs = make([]int64, len(deck.SupportCardUniqueIDs))
		deck.SphereUniqueIDs = []int64{}
		deck.BuddyUniqueIDs = make([]int64, 5)
		deck.Name = DefaultDeckNameByArthur[arthurType]
		deck.IsActive = 1
		deck.IsRental = 0
		state.Decks = append(state.Decks, deck)
	}

	state.User.UserID = userID
	state.User.InviteID = InviteID(userID)
	state.User.Name = ""
	state.User.Comment = "请多关照！"
	state.User.NaviID = 0 // Original CN navi.csv: 妖精乌莎哈 (uathach); 1 is 妮妙.
	state.User.NaviUnlockFlag = 1
	state.User.SelectableNaviIDs = []int8{0}
	state.User.ActiveArthurType = 1
	state.User.LeaderCardUniqueID = starterUniqueIDByCardID[StarterLeaderCardIDByArthur[1]]
	state.User.LeaderCardID = StarterLeaderCardIDByArthur[1]
	state.User.Gold = 0
	state.User.FriendPoint = 0
	state.User.Coin = 0
	state.User.CoinFree = 0
	state.User.PVPPoint = 0
	state.User.TutorialFlag = 0
	state.User.ArthurRank = 0
	state.User.LastHomeDeckRank = 0
	state.User.CardMax = gamestate.CardCapacityLimit
	state.User.CardContainerMax = gamestate.CardContainerCapacityDefault
	state.User.SphereMax = gamestate.SphereCapacityDefault
	state.User.BuddyMax = gamestate.BuddyCapacityDefault
	state.Buddy = gamestate.Buddy{}
	state.Buddies = []gamestate.Buddy{}
	// The profession bit is finalized by UserCreate after the player chooses one
	// Arthur.  Do not pre-open the other three professions on a clean account.
	state.User.UnlockedFeatureIDs = []uint{4, 5, 6, 7, 12}
	state.Onboarding = gamestate.OnboardingState{ConfigVersion: masterdata.OnboardingConfigVersion}

	state.Items = []gamestate.Item{
		{ItemID: 1000}, {ItemID: 1200}, {ItemID: 2000}, {ItemID: 2001},
	}
	state.Engagement = gamestate.EngagementState{
		Initialized:  true,
		Missions:     []gamestate.Mission{},
		Presents:     []gamestate.Present{},
		Histories:    []gamestate.Present{},
		PopupReadIDs: []int{},
	}
	state.GachaSelections = []gamestate.GachaSelection{}
	state.GachaDailyClaims = []gamestate.GachaDailyClaim{}
	state.EventShopPurchases = []gamestate.EventShopPurchase{}
	state.TradeShopPurchases = []gamestate.TradeShopPurchase{}
	state.CardDevelopment = gamestate.CardDevelopmentState{}
	state.SupportDeck.UnlockSlotNums = []int8{0, 0, 0, 0}
	state.SupportDeck.CardCollectionIDs = append([]int(nil), StarterCardIDs...)
	state.SupportDeck.CardCollectionLoveMaxIDs = nil
	state.TeamBattleResultReceipts = []gamestate.TeamBattleResultReceipt{}
	state.TeamBattleStartReceipts = nil
	state.TeamBattleContinueReceipts = nil
	state.TeamBattleSoloResultReceipts = []gamestate.TeamBattleSoloResultReceipt{}
	state.ExploreResultReceipt = nil
	state.PVPResultReceipts = []gamestate.PVPResultReceipt{}
	state.ActiveTeamBattle = nil
	state.PVP.ActiveMatch = nil
	state.PVP.History = []gamestate.PVPMatch{}
	state.PVP.DefenseDecks = []gamestate.PVPDeckSelection{}
	state.FriendPointInboxCursor = 0
	state.TeamBattleScores = nil
	state.TeamBattleSoloScores = nil
	state.TowerQuestProgress = nil
	state.TowerQuestConfigVersion = 0
	progress, err := collectCatalogProgress(*state)
	if err != nil {
		return err
	}
	// New accounts start with no quest clears or reward claims.
	for i := range progress.Areas {
		area := &progress.Areas[i]
		area.StageClear, area.NewClearStage = []byte("[]"), []byte("[]")
		for j := range area.StageQuest.Stages {
			stage := &area.StageQuest.Stages[j]
			stage.IsClearDone = 0
			for k := range stage.Raids {
				raid := &stage.Raids[k]
				for n := range raid.BossGroup.Bosses {
					raid.BossGroup.Bosses[n].State = 0
				}
				for n := range raid.ClearRewards {
					raid.ClearRewards[n].IsAlready = 0
				}
			}
		}
	}
	if err := (catalogProgress{Areas: progress.Areas}).apply(state); err != nil {
		return err
	}

	if err := InstallOnboardingGacha(state); err != nil {
		return err
	}
	return nil
}

func InstallOnboardingGacha(state *gamestate.State) error {
	var singleSource *gamestate.GachaProfile
	var multiSource *gamestate.GachaProfile
	playCounts := make(map[int]int, 3)
	for _, gacha := range state.Gachas {
		playCounts[gacha.GachaID] = gacha.PlayCount
	}
	gachas := make([]gamestate.GachaProfile, 0, len(state.Gachas)+2)
	for index := range state.Gachas {
		gacha := state.Gachas[index]
		if gacha.GachaID == 60200201 {
			copy := gacha
			singleSource = &copy
		}
		if gacha.GachaID == 60200212 {
			copy := gacha
			multiSource = &copy
		}
		if gacha.GachaID != masterdata.OnboardingGachaID &&
			gacha.GachaID != masterdata.OnboardingMultiGachaID {
			gachas = append(gachas, gacha)
		}
	}
	if singleSource == nil || multiSource == nil {
		return errors.New("CN onboarding source gachas are unavailable")
	}
	tutorial := *singleSource
	tutorial.GachaID = masterdata.OnboardingGachaID
	tutorial.BannerKey = "local_first"
	tutorial.GroupID = masterdata.OnboardingGachaID
	tutorial.Name = "首次扭蛋（仅限一次）"
	tutorial.BuyMessage = "使用新手扭蛋券抽取1张骑士卡牌吗？"
	tutorial.SubMessage = "完成『最初的一步』后开放"
	tutorial.OrderNum = 0
	tutorial.PayType = 4
	tutorial.PayTypeID = 2001
	tutorial.Price = 1
	tutorial.CardNum = 1
	tutorial.CardNumMax = 1
	tutorial.UserSelectMax = 0
	tutorial.DailyFirstFree = false
	tutorial.PlayCount = playCounts[masterdata.OnboardingGachaID]
	tutorial.GuaranteedRarityRank = 0
	tutorial.GuaranteedCount = 0
	tutorial.RemainderRarityRank = 0
	tutorial.CardIDs = []int{10002001}
	tutorial.CardWeights = []int{1}
	gachas = append(gachas, tutorial)

	// The first multi-draw returns eleven cards: one UR and ten SR.
	multi := *multiSource
	multi.GachaID = masterdata.OnboardingMultiGachaID
	multi.BannerKey = "local_first_multi"
	multi.GroupID = masterdata.OnboardingMultiGachaID
	multi.Name = "新手11连扭蛋（仅限一次）"
	multi.BuyMessage = "使用400个水晶进行新手11连扭蛋吗？"
	multi.SubMessage = "通关『大地起源』后开放"
	multi.OrderNum = 0
	multi.PayType = 3
	multi.PayTypeID = 0
	multi.Price = 400
	multi.CardNum = 11
	multi.CardNumMax = 11
	multi.UserSelectMax = 0
	multi.DailyFirstFree = false
	multi.PlayCount = playCounts[masterdata.OnboardingMultiGachaID]
	multi.GuaranteedRarityRank = 5
	multi.GuaranteedCount = 1
	multi.RemainderRarityRank = 4
	// Keep the tutorial lineup independent of the standard pool. The four URs
	// are basic starter evolutions, one per profession; the remaining draws use SRs.
	multi.CardIDs = []int{
		10000013, // 第二型加荷里斯：佣兵，单体火物攻。
		10000025, // 第二型佩里诺亚：富豪，单体火物攻＋全体抽卡。
		10000033, // 支援型丽奈特：盗贼，单体风魔攻。
		10000037, // 支援型奥尔特莉特：歌姬，全体回复。
		10000058, 10000087, 10000111, 10000129,
		10000133, 10000171, 10001008, 10001014,
	}
	multi.CardWeights = make([]int, len(multi.CardIDs))
	for index := range multi.CardWeights {
		multi.CardWeights[index] = 1
	}
	gachas = append(gachas, multi)
	sort.SliceStable(gachas, func(left, right int) bool {
		if gachas[left].CategoryNum != gachas[right].CategoryNum {
			return gachas[left].CategoryNum < gachas[right].CategoryNum
		}
		if gachas[left].OrderNum != gachas[right].OrderNum {
			return gachas[left].OrderNum < gachas[right].OrderNum
		}
		return gachas[left].GachaID < gachas[right].GachaID
	})
	state.Gachas = gachas
	return nil
}
