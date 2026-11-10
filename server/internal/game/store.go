package game

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"sync"
	"time"

	"kairisei.local/server/battlemodel"
	"kairisei.local/server/internal/gamestate"
)

const (
	stampDeckSlots      = 12
	DeckSphereSlots     = 3
	deckBuddySlots      = 5
	userCommentMaxBytes = 180
	maxBattleReceipts   = 32
	maxLoveUpItemKinds  = 10
)

type cardLoveRule struct {
	Max     int
	Premium bool
}

type cardDevelopmentRule struct {
	FameMax         int
	DevelopmentType int
	DecomposeRadix  int
	DevelopRadix    int
	AcquisitionText string
}

type Account struct {
	combatDefinitions           *battlemodel.Definitions
	evolutionRestrictions       *EvolutionRestrictions
	playerRevision              uint64
	gachaRevision               uint64
	operatorGachaPlays          map[int]int // plays of operator pools not yet appended to gachas
	gachaCoverBaseURL           string
	teamBattleScores            map[int]gamestate.TeamBattleScoreProgress
	teamBattleSoloScores        map[int]gamestate.TeamBattleScoreProgress
	localShop                   gamestate.LocalShopState
	localCrystalPurchaseEnabled bool
	mu                          sync.RWMutex
	cards                       []CardInfo
	containerCards              []CardInfo
	stackCards                  []gamestate.CardStack
	stackCardTemplates          map[int]gamestate.CardStack
	spheres                     []gamestate.Sphere
	sphereDefinitions           map[int]gamestate.SphereDefinition
	sphereExperience            map[int][]int
	sphereEvoPrices             map[string][]int
	sphereProgression           gamestate.SphereProgressionPolicy
	sphereMax                   int
	buddies                     []gamestate.Buddy
	buddyDefinitions            map[int]gamestate.BuddyDefinition
	buddyExperience             map[int][]int
	buddyEvoPrices              map[string][]int
	buddyProgression            gamestate.BuddyProgressionPolicy
	buddyMax                    int
	decks                       []DeckInfo
	avatars                     []gamestate.Avatar
	avatarParts                 map[int]struct{}
	avatarDefinitions           map[int]gamestate.AvatarPartDefinition
	avatarShopParts             map[int]struct{}
	avatarDefaultDecks          [][]int
	avatarCompletions           []gamestate.AvatarSeriesCompletion
	avatarShopPolicy            gamestate.AvatarShopPolicy
	cardTemplates               map[int]CardInfo
	cardDefinitions             map[int]gamestate.Card
	cardSellExtract             *gamestate.CardSellExtractPolicy
	cardCategoryProfiles        []gamestate.CardCategoryProfile
	cardGroupProfiles           []gamestate.CardGroupProfile
	cardExperience              map[int][]int
	cardProgression             gamestate.CardProgressionPolicy
	deckRankPolicy              gamestate.DeckRankPolicy
	highestDeckRank             int
	lastHomeDeckRank            int
	cardLoveRules               map[int]cardLoveRule
	cardDevelopmentRules        map[int]cardDevelopmentRule
	cardDevelopmentPolicy       gamestate.CardDevelopmentPolicy
	stive                       int
	cardFameTraining            *gamestate.CardFameTraining
	cardMax                     int
	cardContainerMax            int
	deckSlots                   int
	supportSlotCapacity         int
	supportDeckSetCardNum       int
	supportDeckRules            []gamestate.SupportDeckSlotUnlockRule
	supportUnlockedSlots        []int8
	cardCollectionIDs           map[int]struct{}
	cardCollectionLoveMaxIDs    map[int]struct{}
	cardCollectionPages         [][10]int
	buddySlots                  int
	missionCatalog              *MissionCatalog
	missionRevision             uint64
	missionDirty                map[gamestate.MissionMetric]bool
	missionDay                  int64
	missionNextStart            int64
	missionRequestTime          time.Time
	missionVisits               gamestate.MissionVisits
	pvpMissionRanks             []PVPRankConfig
	missions                    map[int]gamestate.MissionProgress
	missionUpdates              map[int]struct{}
	presents                    []gamestate.Present
	presentHistories            []gamestate.Present
	popupReadIDs                map[int]struct{}
	loginBonusPolicy            gamestate.LoginBonusPolicy
	tutorialCompletionMail      TutorialCompletionMail
	alchemistStarter            *gamestate.AlchemistStarterDeck
	alchemistMain               AlchemistMainParameters
	loginBonusState             gamestate.LoginBonusState
	nextUniqueID                int64
	nextSphereUniqueID          int64
	nextBuddyUniqueID           int64
	currentNaviID               int8
	naviUnlockFlag              int64
	selectableNaviIDs           map[int8]struct{}
	naviCatalogIDs              map[int8]struct{}
	naviPurchasePrice           int
	naviSettings                map[int8]NaviSetting
	stampIDs                    map[int]struct{}
	stampDeck                   []int
	costumeIDs                  map[int]struct{}
	collectionRewardIDs         map[[2]int]struct{}
	currentActiveArthur         int8
	currentLevel                int
	currentExperience           int
	currentLevelExperience      int
	nextLevelExperience         int
	currentJobs                 []gamestate.JobParameter
	currentFollowMax            int
	playerProgression           gamestate.PlayerProgressionPolicy
	currentName                 string
	currentComment              string
	honorIDs                    map[int]struct{}
	deckHonorIDs                []int
	gold                        int
	friendPoint                 int
	friendPointInboxCursor      int64
	coin                        int
	coinFree                    int
	pvpPoint                    int
	pvp                         gamestate.PVPPlayerState
	items                       map[int]gamestate.Item
	itemDefinitions             map[int]gamestate.ItemDefinition
	userBuffProfiles            map[int]gamestate.UserBuffProfile
	itemGachaProfiles           map[int]gamestate.ItemGachaProfile
	itemExchangeProfiles        map[int]gamestate.ItemExchangeProfile
	itemLackTipProfiles         map[int]gamestate.ItemLackTipProfile
	eventShopProfiles           map[int]gamestate.EventShopProfile
	eventShopPurchases          map[int]int
	tradeShopProfiles           map[int]gamestate.TradeShopProfile
	tradeShopPurchases          map[int]int
	itemShopTabs                []gamestate.ItemShopTab
	runtimeSettingsRevision     int
	itemShopSettings            map[int]ItemShopSetting
	itemShopPurchases           map[int]int
	itemShopPeriods             map[int]gamestate.ItemShopPeriodCounts
	gachas                      []gamestate.GachaProfile
	gachaWindows                map[int][2]int64
	gachaSelections             map[int][]gamestate.Reward
	gachaDailyClaims            map[int]gamestate.GachaDailyClaim
	bp                          int
	bpMax                       int
	bpRecoveryInterval          time.Duration
	bpNextRecovery              time.Time
	gameOptionFlag              int
	pushOptionFlag              int
	tutorialFlag                int64
	unlockedFeatureIDs          map[uint]struct{}
	onboarding                  gamestate.OnboardingState
	cardActions                 gamestate.CardActionState
	ap                          int
	apMax                       int
	apRecoveryInterval          time.Duration
	apNextRecovery              time.Time
	exploreActive               bool
	exploreActiveRewards        *[]gamestate.Reward
	exploreArthurType           int8
	exploreDeckIndex            int8
	exploreStartedAt            time.Time
	exploreStages               []gamestate.ExploreStage
	exploreStageCursor          int
	exploreActiveStage          int
	storyMainParts              []gamestate.StoryMainPart
	cnStoryMainParts            []gamestate.StoryMainPart
	storySubCharacters          []gamestate.StorySubCharacter
	storyEvents                 []gamestate.StoryEvent
	storyRewardPolicy           gamestate.StoryRewardPolicy
	storyBattleIDsByStory       map[string]map[int]struct{}
	activeMainStoryID           int
	activeMainStoryCN           bool
	activeSubStoryID            int
	activeSubStoryTowerID       int
	activeSubStoryTowerFloor    int
	burstProgress               [4]uint8
	storyTeamBattleSession      gamestate.StoryTeamBattleSession
	questStoryCleared           map[int]bool
	questStorySections          []gamestate.MainQuestReadingSectionDefinition
	mainQuest                   json.RawMessage
	stageQuests                 map[int]json.RawMessage
	defaultStageQuestAreaID     int
	teamBattleSolo              *battleCatalogState
	disabledTeamBattleBossIDs   map[int]bool
	teamBattleLimitedGroupIDs   []int
	teamBattleScheduleSoloPush  map[int]struct{}
	teamBattleScheduleMultiPush map[int]struct{}
	teamBattleReceipts          map[int64]gamestate.TeamBattleResultReceipt
	teamBattleStartReceipts     []gamestate.TeamBattleStartReceipt
	teamBattleContinueReceipts  []gamestate.TeamBattleContinueReceipt
	teamBattleSoloReceipts      map[string]gamestate.TeamBattleSoloResultReceipt
	lastExploreResultReceipt    *gamestate.ExploreResultReceipt
	pvpResultReceipts           map[int]gamestate.PVPResultReceipt
	teamBattleFameBonus         gamestate.TeamBattleFameBonusPolicy
	teamBattleHostBonus         gamestate.TeamBattleHostBonusPolicy
	towerQuestProfiles          map[int]gamestate.TowerQuestProfile
	towerAdmission              TowerAdmission
	towerQuestProgress          map[int]gamestate.TowerQuestProgress
	pendingStageAreaID          int
	activeBattle                *TeamBattleContext
	initialStateRepair          bool
}

func CardsFromState(cards []gamestate.Card, slot int) []CardInfo {
	result := make([]CardInfo, len(cards))
	for index, card := range cards {
		result[index] = CardInfo{
			UniqueID:      card.UniqueID,
			CreateTime:    card.CreateTime,
			CardID:        card.CardID,
			Level:         card.Level,
			LevelMax:      card.LevelMax,
			Experience:    card.Experience,
			NowLevelEXP:   card.NowLevelExperience,
			Love:          card.Love,
			LoveMax:       card.LoveMax,
			SkillLevels:   append([]int16(nil), card.SkillLevels...),
			HP:            card.HP,
			Attack:        card.Attack,
			Magic:         card.Magic,
			Mind:          card.Mind,
			NextLevelEXP:  card.NextLevelExperience,
			AddExperience: card.AddExperience,
			BaseAddPrice:  card.BaseAddPrice,
			IsLock:        card.IsLock,
			Fame:          card.Fame,
			Slot:          slot,
		}
	}
	return result
}

func cloneTeamBattleFameBonusPolicy(
	policy gamestate.TeamBattleFameBonusPolicy,
) gamestate.TeamBattleFameBonusPolicy {
	policy.EligibleRewardTypes = append([]int(nil), policy.EligibleRewardTypes...)
	return policy
}

func cloneTeamBattleHostBonusPolicy(
	policy gamestate.TeamBattleHostBonusPolicy,
) gamestate.TeamBattleHostBonusPolicy {
	policy.EligibleRewardTypes = append([]int(nil), policy.EligibleRewardTypes...)
	return policy
}

func cloneLoginBonusPolicy(policy gamestate.LoginBonusPolicy) gamestate.LoginBonusPolicy {
	policy.Cycle = cloneLoginBonusSchedule(policy.Cycle)
	policy.Beginner = cloneLoginBonusSchedule(policy.Beginner)
	policy.TotalMilestones = cloneLoginBonusSchedule(policy.TotalMilestones)
	return policy
}

func cloneStoryRewardPolicy(policy gamestate.StoryRewardPolicy) gamestate.StoryRewardPolicy {
	policy.MainFirstClear = cloneReward(policy.MainFirstClear)
	policy.SubFirstClear = cloneReward(policy.SubFirstClear)
	policy.EventFirstClear = cloneReward(policy.EventFirstClear)
	return policy
}

func cloneLoginBonusSchedule(schedule []gamestate.LoginBonusDay) []gamestate.LoginBonusDay {
	cloned := append([]gamestate.LoginBonusDay(nil), schedule...)
	for index := range cloned {
		cloned[index].Reward = cloneReward(cloned[index].Reward)
	}
	return cloned
}

func New(state gamestate.State, options ...AccountOption) (*Account, error) {
	if err := gamestate.ValidateItemShopProgress(state.ItemShopPurchases, state.ItemShopPeriods); err != nil {
		return nil, err
	}
	if state.PlayerProgressionPolicy.MaxLevel > 0 {
		policy := state.PlayerProgressionPolicy
		if state.User.Level < 1 || state.User.Level > policy.MaxLevel ||
			state.User.Experience < 0 || state.User.NowLevelExperience < 0 ||
			state.User.BPMax != policy.BattlePointMaximum(state.User.Level) ||
			!slices.Equal(state.User.Jobs, policy.JobsAtLevel(state.User.Level)) {
			return nil, errors.New("card store player progression state is invalid")
		}
		cumulative := policy.CumulativeExperience(state.User.Level)
		if state.User.Level == policy.MaxLevel {
			if state.User.Experience != cumulative || state.User.NowLevelExperience != 0 ||
				state.User.NextLevelExperience != 0 {
				return nil, errors.New("card store maximum-level progression state is invalid")
			}
		} else {
			required := policy.ExperienceRequired(state.User.Level)
			if required <= 0 || state.User.NowLevelExperience >= required ||
				state.User.Experience != cumulative+state.User.NowLevelExperience ||
				state.User.NextLevelExperience != required-state.User.NowLevelExperience {
				return nil, errors.New("card store player EXP state is invalid")
			}
		}
	}
	if state.CardProgressionPolicy.FusionGoldPerMaterialPerBaseLevel <= 0 ||
		len(state.CardExperienceTables) == 0 {
		return nil, errors.New("card store card progression state is invalid")
	}
	if err := validateCardFusionSuccessPolicy(state.CardProgressionPolicy); err != nil {
		return nil, err
	}
	cards := CardsFromState(state.Cards, 0)
	containerCards := CardsFromState(state.ContainerCards, 1)
	decks := DecksFromState(state.Decks)
	if len(cards) == 0 || len(decks) == 0 {
		return nil, errors.New("card store requires cards and decks")
	}
	if state.User.CardMax <= 0 || state.User.CardContainerMax <= 0 {
		return nil, errors.New("card store inventory capacity is invalid")
	}
	cardUniqueIDs := make(map[int64]struct{}, len(cards)+len(containerCards))
	for _, inventory := range [][]CardInfo{cards, containerCards} {
		for _, card := range inventory {
			if card.UniqueID <= 0 || card.CardID <= 0 || card.IsLock < 0 || card.IsLock > 1 {
				return nil, errors.New("card store contains an invalid card")
			}
			if _, duplicate := cardUniqueIDs[card.UniqueID]; duplicate {
				return nil, fmt.Errorf("card store repeats unique ID %d", card.UniqueID)
			}
			cardUniqueIDs[card.UniqueID] = struct{}{}
		}
	}
	deckSlots := len(decks[0].CardUniqueIDs)
	supportSlotCapacity := len(decks[0].SupportCardUniqueIDs)
	buddySlots := len(decks[0].BuddyUniqueIDs)
	if deckSlots == 0 || supportSlotCapacity == 0 || buddySlots == 0 {
		return nil, errors.New("card store deck shape is empty")
	}
	if state.SupportDeckSetCardNum <= 0 ||
		state.SupportDeckSetCardNum > supportSlotCapacity ||
		len(state.SupportDeckSlotUnlockRules) != state.SupportDeckSetCardNum ||
		len(state.SupportDeck.UnlockSlotNums) != 4 {
		return nil, errors.New("card store support-deck configuration is incomplete")
	}
	for index, rule := range state.SupportDeckSlotUnlockRules {
		if rule.SlotIndex != index+1 || rule.Level <= 0 ||
			rule.CardCollectionNum <= 0 || rule.Gold <= 0 {
			return nil, errors.New("card store support-deck slot rule is invalid")
		}
	}
	cardCollectionIDs := make(map[int]struct{}, len(state.SupportDeck.CardCollectionIDs))
	for _, progress := range state.BurstProgress {
		if progress > 3 {
			return nil, errors.New("card store sword-release progress is invalid")
		}
	}
	for _, cardID := range state.SupportDeck.CardCollectionIDs {
		if cardID <= 0 {
			return nil, errors.New("card store collection contains an invalid card ID")
		}
		cardCollectionIDs[cardID] = struct{}{}
	}
	var costume struct {
		CostumeIDs []int `json:"costumeids"`
	}
	if err := json.Unmarshal(state.Costume, &costume); err != nil ||
		costume.CostumeIDs == nil {
		return nil, errors.New("card store requires costume IDs")
	}
	exploreStages := state.Explore.Stages
	if len(exploreStages) == 0 {
		exploreStages = []gamestate.ExploreStage{state.Explore.Stage}
	}
	stageQuests, defaultStageQuestAreaID, err := stageQuestConfigurations(
		state.MainQuest,
		state.StageQuestAreas,
	)
	if err != nil {
		return nil, err
	}
	result := &Account{
		cards:                       cards,
		containerCards:              containerCards,
		stackCards:                  append([]gamestate.CardStack(nil), state.StackCards...),
		stackCardTemplates:          make(map[int]gamestate.CardStack, len(state.StackCardTemplates)+len(state.StackCards)),
		spheres:                     append([]gamestate.Sphere{}, state.Spheres...),
		sphereDefinitions:           make(map[int]gamestate.SphereDefinition, len(state.SphereDefinitions)),
		sphereExperience:            cloneSphereExperienceTables(state.SphereExperienceTables),
		sphereEvoPrices:             cloneSphereEvolutionPrices(state.SphereEvolutionPrices),
		sphereProgression:           state.SphereProgressionPolicy,
		sphereMax:                   state.User.SphereMax,
		buddies:                     append([]gamestate.Buddy{}, state.Buddies...),
		buddyDefinitions:            make(map[int]gamestate.BuddyDefinition, len(state.BuddyDefinitions)),
		buddyExperience:             cloneBuddyExperienceTables(state.BuddyExperienceTables),
		buddyEvoPrices:              cloneBuddyEvolutionPrices(state.BuddyEvolutionPrices),
		buddyProgression:            state.BuddyProgressionPolicy,
		buddyMax:                    state.User.BuddyMax,
		decks:                       decks,
		avatars:                     cloneAvatars(state.Avatars),
		avatarParts:                 make(map[int]struct{}, len(state.AvatarParts)),
		avatarDefinitions:           make(map[int]gamestate.AvatarPartDefinition, len(state.AvatarPartDefinitions)),
		avatarShopParts:             make(map[int]struct{}, len(state.AvatarShopPartIDs)),
		avatarDefaultDecks:          cloneIntMatrix(state.AvatarDefaultDecks),
		avatarCompletions:           cloneAvatarCompletions(state.AvatarSeriesCompletions),
		avatarShopPolicy:            state.AvatarShopPolicy,
		deckRankPolicy:              state.DeckRankPolicy,
		highestDeckRank:             state.User.ArthurRank,
		lastHomeDeckRank:            state.User.LastHomeDeckRank,
		cardProgression:             state.CardProgressionPolicy,
		alchemistStarter:            state.AlchemistStarterDeck,
		cardDevelopmentPolicy:       state.CardDevelopmentPolicy,
		teamBattleFameBonus:         cloneTeamBattleFameBonusPolicy(state.TeamBattleFameBonusPolicy),
		teamBattleHostBonus:         cloneTeamBattleHostBonusPolicy(state.TeamBattleHostBonusPolicy),
		stive:                       state.CardDevelopment.Stive,
		cardMax:                     state.User.CardMax,
		cardContainerMax:            state.User.CardContainerMax,
		deckSlots:                   deckSlots,
		supportSlotCapacity:         supportSlotCapacity,
		supportDeckSetCardNum:       state.SupportDeckSetCardNum,
		supportDeckRules:            append([]gamestate.SupportDeckSlotUnlockRule(nil), state.SupportDeckSlotUnlockRules...),
		supportUnlockedSlots:        append([]int8(nil), state.SupportDeck.UnlockSlotNums...),
		cardCollectionIDs:           cardCollectionIDs,
		cardCollectionLoveMaxIDs:    make(map[int]struct{}),
		buddySlots:                  buddySlots,
		missionVisits:               state.Engagement.Visits,
		missions:                    make(map[int]gamestate.MissionProgress, len(state.Engagement.Missions)),
		presents:                    clonePresents(state.Engagement.Presents),
		presentHistories:            clonePresents(state.Engagement.Histories),
		popupReadIDs:                make(map[int]struct{}, len(state.Engagement.PopupReadIDs)),
		loginBonusPolicy:            cloneLoginBonusPolicy(state.LoginBonusPolicy),
		loginBonusState:             state.LoginBonus,
		currentNaviID:               state.User.NaviID,
		naviUnlockFlag:              state.User.NaviUnlockFlag,
		selectableNaviIDs:           make(map[int8]struct{}),
		naviCatalogIDs:              make(map[int8]struct{}, len(state.User.NaviCatalogIDs)),
		naviPurchasePrice:           state.User.NaviPurchasePrice,
		stampIDs:                    make(map[int]struct{}, len(state.Stamps.StampIDs)),
		stampDeck:                   normalizeStampDeck(state.Stamps.DeckStampIDs),
		costumeIDs:                  make(map[int]struct{}, len(costume.CostumeIDs)),
		currentActiveArthur:         int8(state.User.ActiveArthurType),
		currentLevel:                state.User.Level,
		currentExperience:           state.User.Experience,
		currentLevelExperience:      state.User.NowLevelExperience,
		nextLevelExperience:         state.User.NextLevelExperience,
		currentJobs:                 append([]gamestate.JobParameter(nil), state.User.Jobs...),
		currentFollowMax:            state.User.FollowMax,
		playerProgression:           state.PlayerProgressionPolicy,
		currentName:                 state.User.Name,
		currentComment:              state.User.Comment,
		honorIDs:                    make(map[int]struct{}, len(state.Honors.HonorIDs)),
		deckHonorIDs:                append([]int(nil), state.Honors.DeckHonorIDs...),
		gold:                        state.User.Gold,
		friendPoint:                 state.User.FriendPoint,
		friendPointInboxCursor:      state.FriendPointInboxCursor,
		coin:                        state.User.Coin,
		coinFree:                    state.User.CoinFree,
		pvpPoint:                    state.User.PVPPoint,
		pvp:                         ClonePVPState(state.PVP),
		items:                       make(map[int]gamestate.Item, len(state.Items)),
		itemDefinitions:             make(map[int]gamestate.ItemDefinition, len(state.ItemDefinitions)),
		userBuffProfiles:            make(map[int]gamestate.UserBuffProfile, len(state.UserBuffProfiles)),
		itemGachaProfiles:           make(map[int]gamestate.ItemGachaProfile, len(state.ItemGachaProfiles)),
		itemExchangeProfiles:        make(map[int]gamestate.ItemExchangeProfile, len(state.ItemExchangeProfiles)),
		itemLackTipProfiles:         make(map[int]gamestate.ItemLackTipProfile, len(state.ItemLackTipProfiles)),
		eventShopProfiles:           make(map[int]gamestate.EventShopProfile, len(state.EventShopProfiles)),
		eventShopPurchases:          make(map[int]int, len(state.EventShopPurchases)),
		tradeShopProfiles:           make(map[int]gamestate.TradeShopProfile, len(state.TradeShopProfiles)),
		tradeShopPurchases:          make(map[int]int, len(state.TradeShopPurchases)),
		teamBattleScores:            cloneTeamBattleScores(state.TeamBattleScores),
		teamBattleSoloScores:        cloneTeamBattleScores(state.TeamBattleSoloScores),
		localShop:                   cloneLocalShop(state.LocalShop),
		itemShopTabs:                cloneItemShopTabs(state.ItemShopTabs),
		itemShopPurchases:           maps.Clone(state.ItemShopPurchases),
		itemShopPeriods:             maps.Clone(state.ItemShopPeriods),
		gachas:                      CloneGachaProfiles(state.Gachas),
		operatorGachaPlays:          maps.Clone(state.OperatorGachaPlays),
		gachaSelections:             make(map[int][]gamestate.Reward, len(state.GachaSelections)),
		gachaDailyClaims:            make(map[int]gamestate.GachaDailyClaim, len(state.GachaDailyClaims)),
		bp:                          state.User.BP,
		bpMax:                       state.User.BPMax,
		bpRecoveryInterval:          time.Duration(state.BattlePoint.RecoverySeconds) * time.Second,
		gameOptionFlag:              state.Options.GameEnableFlag,
		pushOptionFlag:              state.Options.PushEnableFlag,
		tutorialFlag:                state.User.TutorialFlag,
		unlockedFeatureIDs:          make(map[uint]struct{}, len(state.User.UnlockedFeatureIDs)),
		onboarding:                  state.Onboarding,
		cardActions:                 state.CardActions,
		activeMainStoryID:           state.Navigation.MainStoryID,
		activeMainStoryCN:           state.Navigation.MainStoryCN,
		activeSubStoryID:            state.Navigation.SubStoryID,
		activeSubStoryTowerID:       state.Navigation.SubStoryTowerID,
		activeSubStoryTowerFloor:    state.Navigation.SubStoryTowerFloor,
		pendingStageAreaID:          state.Navigation.StageAreaID,
		ap:                          state.User.AP,
		apMax:                       state.User.APMax,
		apRecoveryInterval:          time.Duration(state.Explore.APRecoverySeconds) * time.Second,
		exploreActive:               state.Explore.Active,
		exploreActiveRewards:        state.Explore.ActiveRewards,
		exploreArthurType:           state.Explore.ArthurType,
		exploreDeckIndex:            state.Explore.DeckIndex,
		exploreStages:               cloneExploreStages(exploreStages),
		exploreStageCursor:          state.Explore.StageCursor,
		exploreActiveStage:          state.Explore.ActiveStageID,
		storyMainParts:              cloneStoryMainParts(state.Story.MainParts),
		cnStoryMainParts:            cloneStoryMainParts(state.Story.CNMainParts),
		storySubCharacters:          cloneStorySubCharacters(state.Story.SubCharacters),
		storyEvents:                 cloneStoryEvents(state.Story.Events),
		storyRewardPolicy:           cloneStoryRewardPolicy(state.StoryRewardPolicy),
		storyBattleIDsByStory:       make(map[string]map[int]struct{}, len(state.Story.StoryBattleReferences)),
		mainQuest:                   append(json.RawMessage(nil), state.MainQuest...),
		stageQuests:                 stageQuests,
		defaultStageQuestAreaID:     defaultStageQuestAreaID,
		disabledTeamBattleBossIDs:   state.DisabledTeamBattleBossIDs,
		teamBattleScheduleSoloPush:  make(map[int]struct{}, len(state.TeamBattleSchedule.SoloPushGroupIDs)),
		teamBattleScheduleMultiPush: make(map[int]struct{}, len(state.TeamBattleSchedule.MultiPushGroupIDs)),
		teamBattleReceipts:          make(map[int64]gamestate.TeamBattleResultReceipt, len(state.TeamBattleResultReceipts)),
		teamBattleSoloReceipts:      make(map[string]gamestate.TeamBattleSoloResultReceipt, len(state.TeamBattleSoloResultReceipts)),
		pvpResultReceipts:           make(map[int]gamestate.PVPResultReceipt, len(state.PVPResultReceipts)),
		towerQuestProfiles:          make(map[int]gamestate.TowerQuestProfile, len(state.TowerQuestProfiles)),
		towerQuestProgress:          make(map[int]gamestate.TowerQuestProgress, len(state.TowerQuestProgress)),
	}
	var dependencies accountOptions
	for _, option := range options {
		option(&dependencies)
	}
	result.alchemistMain = dependencies.alchemistMain
	result.combatDefinitions = dependencies.combatDefinitions
	if dependencies.cardCatalog == nil {
		var err error
		dependencies.cardCatalog, err = NewCardCatalog(state)
		if err != nil {
			return nil, err
		}
	}
	result.teamBattleSolo, err = newBattleCatalogState(state.TeamBattleSolo, dependencies.battleCatalog)
	if err != nil {
		return nil, fmt.Errorf("prepare battle catalog: %w", err)
	}
	result.useCardCatalog(dependencies.cardCatalog)
	result.collectionRewardIDs = make(map[[2]int]struct{}, len(state.CollectionRewards))
	result.nextUniqueID = max(1, state.InventorySequence.Card)
	result.nextSphereUniqueID = max(1, state.InventorySequence.Sphere)
	result.nextBuddyUniqueID = max(1, state.InventorySequence.Buddy)
	for _, definition := range state.CollectionRewards {
		result.collectionRewardIDs[[2]int{definition.Type, definition.ID}] = struct{}{}
	}
	for _, featureID := range state.User.UnlockedFeatureIDs {
		result.unlockedFeatureIDs[featureID] = struct{}{}
	}
	result.burstProgress = state.BurstProgress
	result.storyTeamBattleSession = state.StoryTeamBattleSession
	result.questStoryCleared = make(map[int]bool, len(state.Story.QuestClearedIDs))
	for _, id := range state.Story.QuestClearedIDs {
		result.questStoryCleared[id] = true
	}
	result.questStorySections = state.MainQuestReadingSections
	result.reconcileOnboardingFeatureUnlocksLocked()
	for _, template := range state.StackCardTemplates {
		if template.CardID <= 0 || template.Num != 0 || template.AddExperience <= 0 || template.BaseAddPrice < 0 {
			return nil, fmt.Errorf("stack-card template %d is invalid", template.CardID)
		}
		if _, duplicate := result.stackCardTemplates[template.CardID]; duplicate {
			return nil, fmt.Errorf("duplicate stack-card template %d", template.CardID)
		}
		result.stackCardTemplates[template.CardID] = template
	}
	for _, inventory := range state.StackCards {
		template, exists := result.stackCardTemplates[inventory.CardID]
		if !exists {
			return nil, fmt.Errorf("stack-card inventory %d has no template", inventory.CardID)
		}
		if inventory.AddExperience != template.AddExperience || inventory.BaseAddPrice != template.BaseAddPrice ||
			inventory.MaterialType != template.MaterialType {
			return nil, fmt.Errorf("stack-card inventory %d differs from its template", inventory.CardID)
		}
	}
	for _, popupID := range state.Engagement.PopupReadIDs {
		if popupID <= 0 {
			return nil, errors.New("popup read state contains an invalid ID")
		}
		if _, duplicate := result.popupReadIDs[popupID]; duplicate {
			return nil, fmt.Errorf("popup read state repeats ID %d", popupID)
		}
		result.popupReadIDs[popupID] = struct{}{}
	}
	for _, mission := range state.Engagement.Missions {
		if _, duplicate := result.missions[mission.Info.MissionID]; duplicate {
			return nil, errors.New("mission IDs must be unique")
		}
		mission.Info.MatchedTargets = append([]int(nil), mission.Info.MatchedTargets...)
		result.missions[mission.Info.MissionID] = mission.Info
	}
	for _, profile := range state.TowerQuestProfiles {
		if _, duplicate := result.towerQuestProfiles[profile.TowerID]; duplicate {
			return nil, fmt.Errorf("duplicate tower profile %d", profile.TowerID)
		}
		result.towerQuestProfiles[profile.TowerID] = cloneTowerQuestProfile(profile)
	}
	for _, progress := range state.TowerQuestProgress {
		if _, duplicate := result.towerQuestProgress[progress.TowerID]; duplicate {
			return nil, fmt.Errorf("duplicate tower progress %d", progress.TowerID)
		}
		result.towerQuestProgress[progress.TowerID] = cloneTowerQuestProgress(progress)
	}
	for _, reference := range state.Story.StoryBattleReferences {
		key := fmt.Sprintf("%s:%d", reference.StoryKind, reference.StoryID)
		battleIDs := make(map[int]struct{}, len(reference.StoryBattleIDs))
		for _, battleID := range reference.StoryBattleIDs {
			battleIDs[battleID] = struct{}{}
		}
		result.storyBattleIDsByStory[key] = battleIDs
	}
	allowedScheduleGroups := make(map[int]struct{}, len(state.TeamBattleScheduleGroupIDs))
	for _, groupID := range state.TeamBattleScheduleGroupIDs {
		allowedScheduleGroups[groupID] = struct{}{}
	}
	result.teamBattleLimitedGroupIDs = append(
		[]int(nil), state.TeamBattleScheduleGroupIDs...,
	)
	for _, groupID := range state.TeamBattleSchedule.SoloPushGroupIDs {
		if _, allowed := allowedScheduleGroups[groupID]; allowed {
			result.teamBattleScheduleSoloPush[groupID] = struct{}{}
		}
	}
	for _, groupID := range state.TeamBattleSchedule.MultiPushGroupIDs {
		if _, allowed := allowedScheduleGroups[groupID]; allowed {
			result.teamBattleScheduleMultiPush[groupID] = struct{}{}
		}
	}
	if state.CardDevelopment.Training != nil {
		training := *state.CardDevelopment.Training
		result.cardFameTraining = &training
	}
	if err := gamestate.ValidateTeamBattleContinueReceipts(state.TeamBattleContinueReceipts); err != nil {
		return nil, err
	}
	result.teamBattleContinueReceipts = append([]gamestate.TeamBattleContinueReceipt(nil), state.TeamBattleContinueReceipts...)
	seenStarts := make(map[int64]struct{})
	for _, receipt := range state.TeamBattleStartReceipts {
		if receipt.RoomID <= 0 || receipt.BossID <= 0 || receipt.BPUse < 0 {
			return nil, errors.New("card store multiplayer start receipt is invalid")
		}
		if _, duplicate := seenStarts[receipt.RoomID]; duplicate {
			return nil, errors.New("card store multiplayer start receipt is duplicated")
		}
		seenStarts[receipt.RoomID] = struct{}{}
		result.teamBattleStartReceipts = append(result.teamBattleStartReceipts, receipt)
	}
	for _, receipt := range state.TeamBattleResultReceipts {
		if receipt.RoomID <= 0 || receipt.ClaimedAtUnix <= 0 || (len(receipt.Snapshot) != 0 && !json.Valid(receipt.Snapshot)) {
			return nil, errors.New("card store team battle result receipt is invalid")
		}
		if _, duplicate := result.teamBattleReceipts[receipt.RoomID]; duplicate {
			return nil, errors.New("card store team battle result receipt room is duplicated")
		}
		receipt.Snapshot = append(json.RawMessage(nil), receipt.Snapshot...)
		result.teamBattleReceipts[receipt.RoomID] = receipt
	}
	for _, receipt := range state.TeamBattleSoloResultReceipts {
		if !isLowerSHA256Digest(receipt.RequestSHA256) || receipt.BossID <= 0 || receipt.ClaimedAtUnix <= 0 ||
			(len(receipt.Snapshot) != 0 && !json.Valid(receipt.Snapshot)) {
			return nil, errors.New("card store solo team battle result receipt is invalid")
		}
		if _, duplicate := result.teamBattleSoloReceipts[receipt.RequestSHA256]; duplicate {
			return nil, errors.New("card store solo team battle result receipt digest is duplicated")
		}
		receipt.Snapshot = append(json.RawMessage(nil), receipt.Snapshot...)
		result.teamBattleSoloReceipts[receipt.RequestSHA256] = receipt
	}
	if receipt := state.ExploreResultReceipt; receipt != nil {
		if receipt.StartedAtUnix <= 0 || receipt.ClaimedAtUnix <= 0 ||
			len(receipt.Response) == 0 || !json.Valid(receipt.Response) {
			return nil, errors.New("card store Explore result receipt is invalid")
		}
		copy := *receipt
		copy.Response = append(json.RawMessage(nil), receipt.Response...)
		result.lastExploreResultReceipt = &copy
	}
	for _, receipt := range state.PVPResultReceipts {
		if receipt.BattleID <= 0 || !isLowerSHA256Digest(receipt.RequestSHA256) || receipt.ClaimedAtUnix <= 0 ||
			len(receipt.Response) == 0 || !json.Valid(receipt.Response) {
			return nil, errors.New("card store PVP result receipt is invalid")
		}
		if _, duplicate := result.pvpResultReceipts[receipt.BattleID]; duplicate {
			return nil, errors.New("card store PVP result receipt battle is duplicated")
		}
		receipt.Response = append(json.RawMessage(nil), receipt.Response...)
		result.pvpResultReceipts[receipt.BattleID] = receipt
	}
	if result.pvp.NextBattleID <= 0 {
		result.pvp.NextBattleID = 1
	}
	if result.pvp.Challenge < 0 || result.pvp.ChallengeDay < 0 {
		return nil, errors.New("persisted PVP challenge count is invalid")
	}
	for _, definition := range state.ItemDefinitions {
		result.itemDefinitions[definition.ItemID] = definition
	}
	for _, profile := range state.UserBuffProfiles {
		if _, duplicate := result.userBuffProfiles[profile.UserBuffID]; duplicate {
			return nil, fmt.Errorf("duplicate user-buff profile %d", profile.UserBuffID)
		}
		if _, exists := result.itemDefinitions[profile.ItemID]; !exists ||
			profile.RequiredNum <= 0 || profile.DurationSeconds <= 0 {
			return nil, fmt.Errorf("invalid user-buff profile %d", profile.UserBuffID)
		}
		result.userBuffProfiles[profile.UserBuffID] = profile
	}
	for _, profile := range state.ItemGachaProfiles {
		if _, duplicate := result.itemGachaProfiles[profile.ItemID]; duplicate {
			return nil, fmt.Errorf("duplicate item gacha profile %d", profile.ItemID)
		}
		result.itemGachaProfiles[profile.ItemID] = profile
	}
	for _, profile := range state.ItemExchangeProfiles {
		if _, duplicate := result.itemExchangeProfiles[profile.ItemID]; duplicate {
			return nil, fmt.Errorf("duplicate item exchange profile %d", profile.ItemID)
		}
		profile.Reward = cloneReward(profile.Reward)
		result.itemExchangeProfiles[profile.ItemID] = profile
	}
	for _, profile := range state.ItemLackTipProfiles {
		if _, duplicate := result.itemLackTipProfiles[profile.Index]; duplicate {
			return nil, fmt.Errorf("duplicate item lack-tip profile %d", profile.Index)
		}
		links := make([]gamestate.ItemLackTipLink, len(profile.TextURLs))
		copy(links, profile.TextURLs)
		profile.TextURLs = links
		result.itemLackTipProfiles[profile.Index] = profile
	}
	for _, profile := range state.EventShopProfiles {
		if _, duplicate := result.eventShopProfiles[profile.EventID]; duplicate {
			return nil, fmt.Errorf("duplicate event shop profile %d", profile.EventID)
		}
		result.eventShopProfiles[profile.EventID] = cloneEventShopProfile(profile)
	}
	for _, profile := range state.TradeShopProfiles {
		if profile.TradeShopID <= 0 {
			return nil, errors.New("trade shop profile has an invalid ID")
		}
		if _, duplicate := result.tradeShopProfiles[profile.TradeShopID]; duplicate {
			return nil, fmt.Errorf("duplicate trade shop profile %d", profile.TradeShopID)
		}
		result.tradeShopProfiles[profile.TradeShopID] = cloneTradeShopProfile(profile)
	}
	for _, selection := range state.GachaSelections {
		if _, duplicate := result.gachaSelections[selection.GachaID]; duplicate {
			return nil, fmt.Errorf("card store repeats gacha selection %d", selection.GachaID)
		}
		profile := findGachaProfile(result.gachas, selection.GachaID)
		if profile == nil {
			if selection.GachaID < gamestate.OperatorGachaFirstID {
				return nil, fmt.Errorf("card store gacha selection %d has no profile", selection.GachaID)
			}
			// The published operator profile is attached after loading the account.
		} else if err := validateGachaSelection(*profile, selection.Rewards); err != nil {
			return nil, err
		}
		result.gachaSelections[selection.GachaID] = cloneRewards(selection.Rewards)
	}
	for _, claim := range state.GachaDailyClaims {
		if _, duplicate := result.gachaDailyClaims[claim.GachaID]; duplicate {
			return nil, fmt.Errorf("card store repeats gacha daily claim %d", claim.GachaID)
		}
		profile := findGachaProfile(result.gachas, claim.GachaID)
		operator := claim.GachaID >= gamestate.OperatorGachaFirstID && (profile == nil || profile.PublicationKey == "operator")
		if (profile == nil && !operator) || claim.PlayCount < 0 || claim.PlayCount > math.MaxInt32 {
			return nil, fmt.Errorf("card store gacha daily claim %d has no daily profile", claim.GachaID)
		}
		if _, err := time.Parse("2006-01-02", claim.Day); err != nil {
			return nil, fmt.Errorf("card store gacha daily claim %d has an invalid day", claim.GachaID)
		}
		result.gachaDailyClaims[claim.GachaID] = claim
	}
	for _, definition := range state.SphereDefinitions {
		if _, duplicate := result.sphereDefinitions[definition.SphereID]; duplicate {
			return nil, fmt.Errorf("duplicate sphere definition %d", definition.SphereID)
		}
		result.sphereDefinitions[definition.SphereID] = definition
	}
	// Capacity limits new grants, not loading or reducing existing inventory.
	if result.sphereMax <= 0 ||
		result.sphereProgression.MaterialLevelBonusPermillePerLevel <= 0 ||
		len(result.sphereDefinitions) == 0 || len(result.sphereExperience) == 0 || len(result.sphereEvoPrices) == 0 {
		return nil, errors.New("card store requires sphere master and valid inventory capacity")
	}
	sphereUniqueIDs := make(map[int64]struct{}, len(result.spheres))
	for _, sphere := range result.spheres {
		definition, exists := result.sphereDefinitions[sphere.SphereID]
		if !exists || sphere.UniqueID <= 0 || sphere.Level < 1 || sphere.Level > definition.MaxLevel ||
			sphere.Experience < 0 || sphere.IsLock < 0 || sphere.IsLock > 1 {
			return nil, fmt.Errorf("invalid persisted sphere %d", sphere.UniqueID)
		}
		if _, duplicate := sphereUniqueIDs[sphere.UniqueID]; duplicate {
			return nil, fmt.Errorf("duplicate persisted sphere unique ID %d", sphere.UniqueID)
		}
		sphereUniqueIDs[sphere.UniqueID] = struct{}{}
		if sphere.UniqueID >= result.nextSphereUniqueID {
			result.nextSphereUniqueID = sphere.UniqueID + 1
		}
	}
	if result.nextSphereUniqueID == 0 {
		result.nextSphereUniqueID = 1
	}
	for _, definition := range state.BuddyDefinitions {
		result.buddyDefinitions[definition.BuddyID] = definition
	}
	if len(result.buddyDefinitions) == 0 || len(result.buddyExperience) == 0 || len(result.buddyEvoPrices) == 0 ||
		result.buddyProgression.MaximumMaterialCount <= 0 ||
		result.buddyMax <= 0 {
		// An initialized but empty Buddy inventory is valid for a clean account.
		// Master data and capacity are still mandatory, and every non-empty entry
		// is validated below.
		return nil, errors.New("card store requires official buddy master and valid inventory capacity")
	}
	buddyUniqueIDs := make(map[int64]struct{}, len(result.buddies))
	for _, buddy := range result.buddies {
		definition, exists := result.buddyDefinitions[buddy.BuddyID]
		if !exists || buddy.UniqueID <= 0 || buddy.Level < 1 || buddy.Level > definition.MaxLevel ||
			buddy.Experience < 0 || buddy.IsLock < 0 || buddy.IsLock > 1 ||
			buddy.AddExperience != definition.MaterialAddExperience ||
			buddy.BaseAddPrice != definition.FusionBaseAddPrice {
			return nil, fmt.Errorf("invalid persisted buddy %d", buddy.UniqueID)
		}
		if _, duplicate := buddyUniqueIDs[buddy.UniqueID]; duplicate {
			return nil, fmt.Errorf("duplicate persisted buddy unique ID %d", buddy.UniqueID)
		}
		buddyUniqueIDs[buddy.UniqueID] = struct{}{}
		if buddy.UniqueID >= result.nextBuddyUniqueID {
			result.nextBuddyUniqueID = buddy.UniqueID + 1
		}
	}
	if result.nextBuddyUniqueID == 0 {
		result.nextBuddyUniqueID = 1
	}
	for _, definition := range state.AvatarPartDefinitions {
		if definition.PartID <= 0 {
			return nil, errors.New("card store contains an invalid Avatar part definition")
		}
		if _, duplicate := result.avatarDefinitions[definition.PartID]; duplicate {
			return nil, fmt.Errorf("duplicate Avatar part definition %d", definition.PartID)
		}
		result.avatarDefinitions[definition.PartID] = definition
	}
	for _, partID := range state.AvatarShopPartIDs {
		if _, exists := result.avatarDefinitions[partID]; !exists {
			return nil, fmt.Errorf("Avatar shop part %d is absent from the official master", partID)
		}
		if _, duplicate := result.avatarShopParts[partID]; duplicate {
			return nil, fmt.Errorf("duplicate Avatar shop part %d", partID)
		}
		result.avatarShopParts[partID] = struct{}{}
	}
	if len(result.avatarDefinitions) == 0 || len(result.avatarDefaultDecks) != 4 || len(result.avatarCompletions) == 0 ||
		result.avatarShopPolicy.PayType < 1 || result.avatarShopPolicy.PayType > 7 ||
		result.avatarShopPolicy.Price <= 0 {
		return nil, errors.New("card store requires Avatar master and local shop policy")
	}
	for _, partID := range state.AvatarParts {
		if _, exists := result.avatarDefinitions[partID]; !exists {
			return nil, fmt.Errorf("persisted Avatar part %d is absent from the official master", partID)
		}
		if _, duplicate := result.avatarParts[partID]; duplicate {
			return nil, fmt.Errorf("duplicate persisted Avatar part %d", partID)
		}
		result.avatarParts[partID] = struct{}{}
	}
	if len(result.avatarParts) == 0 {
		return nil, errors.New("card store requires owned Avatar parts")
	}
	for _, item := range state.Items {
		definition, exists := result.itemDefinitions[item.ItemID]
		if !exists || item.Num < 0 || item.Num > definition.MaxOwned {
			return nil, fmt.Errorf("invalid persisted item %d", item.ItemID)
		}
		result.items[item.ItemID] = item
	}
	if len(result.itemDefinitions) == 0 || len(result.itemShopTabs) != 5 {
		return nil, errors.New("card store requires official item master and five shop tabs")
	}
	for towerID, profile := range result.towerQuestProfiles {
		progress, exists := result.towerQuestProgress[towerID]
		if !exists || progress.Floor < 0 || progress.Floor > len(profile.Floors) ||
			progress.LoseCount < 0 || progress.LoseCount >= profile.LoseCountMax {
			return nil, fmt.Errorf("card store tower profile %d is incomplete", towerID)
		}
		for _, floor := range profile.Floors {
			_, itemExists := result.itemDefinitions[floor.ItemID]
			if !floor.ValidEntryCost() || floor.ItemID > 0 && !itemExists {
				return nil, fmt.Errorf("card store tower floor %d:%d has an invalid cost", towerID, floor.Floor)
			}
		}
	}
	if state.Explore.StartedAtUnix > 0 {
		result.exploreStartedAt = time.Unix(state.Explore.StartedAtUnix, 0)
	}
	if result.ap < result.apMax {
		if state.Explore.APNextRecoveryUnix > 0 {
			result.apNextRecovery = time.Unix(state.Explore.APNextRecoveryUnix, 0)
		} else {
			result.apNextRecovery = time.Now().Add(result.apRecoveryInterval)
		}
	}
	if result.bpRecoveryInterval <= 0 {
		return nil, errors.New("card store requires a positive BP recovery interval")
	}
	if result.bp < result.bpMax {
		if state.BattlePoint.NextRecoveryUnix > 0 {
			result.bpNextRecovery = time.Unix(state.BattlePoint.NextRecoveryUnix, 0)
		} else {
			result.bpNextRecovery = time.Now().Add(result.bpRecoveryInterval)
		}
	}
	if result.cardDevelopmentPolicy.TimeEveryFameSeconds <= 0 ||
		result.cardDevelopmentPolicy.CoinEveryHour <= 0 ||
		result.cardDevelopmentPolicy.HelpPath == "" || result.cardDevelopmentPolicy.HelpPath[0] != '/' ||
		result.stive < 0 {
		return nil, errors.New("card store requires a valid card development policy and state")
	}
	if result.cardFameTraining != nil {
		index := cardIndexByUniqueID(result.cards, result.cardFameTraining.UniqueID)
		if index < 0 || result.cards[index].IsLock != 1 || result.cardFameTraining.BeginAtUnix <= 0 ||
			result.cardFameTraining.Fame <= 0 {
			return nil, errors.New("card store card fame training state is invalid")
		}
		rule, exists := result.cardDevelopmentRules[result.cards[index].CardID]
		if !exists || rule.DevelopmentType != 2 || rule.DevelopRadix <= 0 ||
			result.cards[index].Fame+result.cardFameTraining.Fame > rule.FameMax {
			return nil, errors.New("card store card fame training exceeds the official card rule")
		}
	}
	for _, inventory := range [][]CardInfo{cards, containerCards} {
		for _, card := range inventory {
			loveRule, exists := result.cardLoveRules[card.CardID]
			if card.Love < 0 || !exists || card.Love > loveRule.Max {
				return nil, fmt.Errorf("card %d love is outside its official master range", card.UniqueID)
			}
			developmentRule, exists := result.cardDevelopmentRules[card.CardID]
			if !exists || card.Fame < 1 || card.Fame > developmentRule.FameMax {
				return nil, fmt.Errorf("card %d fame is outside its official master range", card.UniqueID)
			}
			normalized, err := result.normalizeCardLocked(card)
			if err != nil || !EqualCardInfo(normalized, card) {
				return nil, fmt.Errorf("card %d progression fields are invalid", card.UniqueID)
			}
		}
	}
	for cardID := range result.cardCollectionIDs {
		if _, exists := result.cardTemplates[cardID]; !exists {
			if _, stack := result.stackCardTemplates[cardID]; !stack {
				return nil, fmt.Errorf("card store collection references unknown card %d", cardID)
			}
		}
	}
	for _, id := range state.SupportDeck.CardCollectionLoveMaxIDs {
		if _, exists := result.cardCollectionIDs[id]; !exists || result.cardDefinitions[id].LoveMax <= 0 {
			return nil, fmt.Errorf("card collection love maximum references invalid card %d", id)
		}
		result.cardCollectionLoveMaxIDs[id] = struct{}{}
	}
	for _, inventory := range [][]CardInfo{result.cards, result.containerCards} {
		for _, card := range inventory {
			result.recordCollectedCardLocked(card)
		}
	}
	if len(result.gachas) == 0 {
		return nil, errors.New("card store requires a configured CN gacha")
	}
	for _, gacha := range result.gachas {
		if err := result.validateGachaRulesLocked(gacha); err != nil {
			return nil, err
		}
		if gacha.PayType == 4 {
			if definition, exists := result.itemDefinitions[gacha.PayTypeID]; !exists || definition.ItemType != "GACHA_TICKET" {
				return nil, fmt.Errorf("gacha %d references an invalid ticket item", gacha.GachaID)
			}
		}
		for _, cardID := range gacha.CardIDs {
			if _, exists := result.cardTemplates[cardID]; !exists {
				return nil, fmt.Errorf("gacha %d references unknown card %d", gacha.GachaID, cardID)
			}
		}
	}
	for itemID, profile := range result.itemGachaProfiles {
		definition, exists := result.itemDefinitions[itemID]
		if !exists || definition.ItemType != "GACHA" || definition.Function != "GACHA_EXEC" ||
			definition.FunctionValue != profile.FunctionValue || (len(profile.Rewards) == 0 && len(profile.RewardPool) == 0) {
			return nil, fmt.Errorf("item gacha profile %d is invalid", itemID)
		}
		for _, reward := range profile.Rewards {
			if err := result.validateRewardLocked(reward); err != nil {
				return nil, fmt.Errorf("item gacha profile %d: %w", itemID, err)
			}
		}
		if len(profile.RewardPool) > 0 {
			if err := gamestate.ValidateRewardPool(profile.RewardPool); err != nil {
				return nil, err
			}
			for _, entry := range profile.RewardPool {
				if err := result.validateRewardLocked(entry.Reward); err != nil {
					return nil, err
				}
			}
		}
	}
	for _, profile := range state.TeamBattleRewards {
		if err := gamestate.ValidateTeamBattleScorePolicy(profile.ScorePolicy); err != nil {
			return nil, err
		}
	}
	for itemID, profile := range result.itemExchangeProfiles {
		if itemID != profile.ItemID || profile.EventID <= 0 || profile.NeedNum <= 0 ||
			(profile.IsAppearEvent != 0 && profile.IsAppearEvent != 1) {
			return nil, fmt.Errorf("item exchange profile %d is invalid", itemID)
		}
		if _, exists := result.itemDefinitions[itemID]; !exists {
			return nil, fmt.Errorf("item exchange profile %d has no item definition", itemID)
		}
		if err := result.validateLocalTradeRewardLocked(profile.Reward); err != nil {
			return nil, fmt.Errorf("item exchange profile %d: %w", itemID, err)
		}
	}
	lineupStocks := make(map[int]int)
	for eventID, profile := range result.eventShopProfiles {
		if eventID != profile.EventID || eventID <= 0 || profile.PointItemID <= 0 || len(profile.Lineups) == 0 {
			return nil, fmt.Errorf("event shop profile %d is invalid", eventID)
		}
		if _, exists := result.itemDefinitions[profile.PointItemID]; !exists {
			return nil, fmt.Errorf("event shop %d has no point item definition", eventID)
		}
		for _, lineup := range profile.Lineups {
			if lineup.LineupID <= 0 || lineup.Name == "" || lineup.ImageURL == "" || lineup.Price <= 0 ||
				lineup.StockNum == 0 || lineup.StockType < 0 ||
				(lineup.IsFeature != 0 && lineup.IsFeature != 1) {
				return nil, fmt.Errorf("event shop lineup %d is invalid", lineup.LineupID)
			}
			if _, duplicate := lineupStocks[lineup.LineupID]; duplicate {
				return nil, fmt.Errorf("duplicate event shop lineup %d", lineup.LineupID)
			}
			if err := result.validateLocalTradeRewardLocked(lineup.Reward); err != nil {
				return nil, fmt.Errorf("event shop lineup %d: %w", lineup.LineupID, err)
			}
			lineupStocks[lineup.LineupID] = lineup.StockNum
		}
	}
	for _, purchase := range state.EventShopPurchases {
		stock, exists := lineupStocks[purchase.LineupID]
		if !exists || purchase.Count <= 0 || (stock > 0 && purchase.Count > stock) {
			return nil, fmt.Errorf("event shop purchase %d is invalid", purchase.LineupID)
		}
		if _, duplicate := result.eventShopPurchases[purchase.LineupID]; duplicate {
			return nil, fmt.Errorf("duplicate event shop purchase %d", purchase.LineupID)
		}
		result.eventShopPurchases[purchase.LineupID] = purchase.Count
	}
	tradeLineupStocks := make(map[int]int)
	for shopID, profile := range result.tradeShopProfiles {
		if shopID != profile.TradeShopID || profile.Name == "" || len(profile.Lineups) == 0 {
			return nil, fmt.Errorf("trade shop profile %d is invalid", shopID)
		}
		for _, lineup := range profile.Lineups {
			if lineup.LineupID <= 0 || lineup.LineupName == "" || lineup.StockNum == 0 ||
				len(lineup.Prices) != 1 || len(lineup.Rewards) != 1 {
				return nil, fmt.Errorf("trade shop lineup %d is invalid", lineup.LineupID)
			}
			if _, duplicate := tradeLineupStocks[lineup.LineupID]; duplicate {
				return nil, fmt.Errorf("duplicate trade shop lineup %d", lineup.LineupID)
			}
			price := lineup.Prices[0]
			if price.Type != 4 || price.ID <= 0 || price.Num <= 0 || price.PointCardCondition == nil {
				return nil, fmt.Errorf("trade shop lineup %d price is invalid", lineup.LineupID)
			}
			if _, exists := result.itemDefinitions[price.ID]; !exists {
				return nil, fmt.Errorf("trade shop lineup %d price item is unknown", lineup.LineupID)
			}
			if err := result.validateLocalTradeRewardLocked(lineup.Rewards[0]); err != nil {
				return nil, fmt.Errorf("trade shop lineup %d: %w", lineup.LineupID, err)
			}
			tradeLineupStocks[lineup.LineupID] = lineup.StockNum
		}
	}
	for _, purchase := range state.TradeShopPurchases {
		// Counts are durable receipts. A retired offer or a reduced limit must
		// not prevent login or reset what the player has already exchanged.
		if purchase.LineupID <= 0 || purchase.Count <= 0 {
			return nil, fmt.Errorf("persisted trade shop purchase %d is invalid", purchase.LineupID)
		}
		if _, duplicate := result.tradeShopPurchases[purchase.LineupID]; duplicate {
			return nil, fmt.Errorf("duplicate persisted trade shop purchase %d", purchase.LineupID)
		}
		result.tradeShopPurchases[purchase.LineupID] = purchase.Count
	}
	for _, naviID := range state.User.SelectableNaviIDs {
		result.selectableNaviIDs[naviID] = struct{}{}
	}
	for _, naviID := range state.User.NaviCatalogIDs {
		result.naviCatalogIDs[naviID] = struct{}{}
	}
	for _, stampID := range state.Stamps.StampIDs {
		result.stampIDs[stampID] = struct{}{}
	}
	for _, costumeID := range costume.CostumeIDs {
		result.costumeIDs[costumeID] = struct{}{}
	}
	if len(result.avatars) != 4 {
		return nil, errors.New("card store requires one avatar for each Arthur type")
	}
	for index, avatar := range result.avatars {
		if len(avatar.AvatarPartIDs) != avatarDeckSlots {
			return nil, fmt.Errorf("Avatar deck %d has an invalid shape", index+1)
		}
		for slot, partID := range avatar.AvatarPartIDs {
			if partID == 0 {
				continue
			}
			definition, exists := result.avatarDefinitions[partID]
			if !exists {
				return nil, fmt.Errorf("Avatar deck %d references unknown part %d", index+1, partID)
			}
			if _, owned := result.avatarParts[partID]; !owned || !avatarEquipAllowed(definition, index+1, slot, result.avatarDefaultDecks) {
				return nil, fmt.Errorf("Avatar deck %d has an invalid part %d in slot %d", index+1, partID, slot)
			}
		}
	}
	for _, honorID := range state.Honors.HonorIDs {
		result.honorIDs[honorID] = struct{}{}
	}
	if _, exists := result.selectableNaviIDs[result.currentNaviID]; !exists {
		return nil, errors.New("card store default navigator is not selectable")
	}
	for _, inventory := range [][]CardInfo{cards, containerCards} {
		for _, card := range inventory {
			if card.UniqueID >= result.nextUniqueID {
				result.nextUniqueID = card.UniqueID + 1
			}
		}
	}
	repairedDecks, err := result.repairIncompleteMainDecks()
	if err != nil {
		return nil, fmt.Errorf("repair persisted main deck: %w", err)
	}
	featureCount := len(result.unlockedFeatureIDs)
	if err := result.completeTrainingBurstLocked(); err != nil {
		return nil, fmt.Errorf("complete training sword unlocks: %w", err)
	}
	result.initialStateRepair = repairedDecks || len(result.unlockedFeatureIDs) != featureCount
	owned := ownedCardIDs(result.cards)
	for _, deck := range result.decks {
		if err := result.validateStoredDeck(deck, owned); err != nil {
			return nil, fmt.Errorf("validate initial deck: %w", err)
		}
	}
	result.missionCatalog = dependencies.missions
	if result.missionCatalog == nil {
		result.missionCatalog, err = NewMissionCatalog(state.MissionDefinitions)
		if err != nil {
			return nil, err
		}
	}
	result.markAllMissionMetricsLocked()
	if err := result.validateEngagementLocked(); err != nil {
		return nil, fmt.Errorf("validate mission and present state: %w", err)
	}
	if err := result.validateLoginBonusLocked(); err != nil {
		return nil, err
	}
	if result.exploreActive {
		if _, _, _, ok := result.exploreSelectionLocked(result.exploreArthurType, result.exploreDeckIndex); !ok ||
			result.exploreStartedAt.IsZero() || !result.hasExploreStageLocked(result.exploreActiveStage) {
			return nil, errors.New("persisted Explore selection is invalid")
		}
	}
	if activeBattle := teamBattleContextFromRelease(state.ActiveTeamBattle); activeBattle != nil {
		if err := result.validateActiveTeamBattleContextLocked(*activeBattle, state.TeamBattleRewards); err != nil {
			return nil, fmt.Errorf("validate persisted active team battle: %w", err)
		}
		result.activeBattle = activeBattle
	}
	result.refreshDeckRanksLocked()
	return result, nil
}

func cloneExploreStages(stages []gamestate.ExploreStage) []gamestate.ExploreStage {
	cloned := make([]gamestate.ExploreStage, len(stages))
	for index, stage := range stages {
		cloned[index] = stage
		cloned[index].Floors = append([]gamestate.ExploreFloor(nil), stage.Floors...)
	}
	return cloned
}

func (s *Account) Show() ([]CardInfo, []DeckInfo) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return CloneCards(s.cards), s.rankedDecksLocked(s.decks)
}

func (s *Account) ContainerShow() []CardInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return CloneCards(s.containerCards)
}

func (s *Account) CardCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.cards)
}

func (s *Account) ContainerCardCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.containerCards)
}

func (s *Account) StackState() []gamestate.CardStack {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]gamestate.CardStack(nil), s.stackCards...)
}

func (s *Account) GoldState() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gold
}

func (s *Account) FriendPointState() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.friendPoint
}

func (s *Account) FriendPointRewardForState(state int8) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if state == FriendStateFriend {
		return s.playerProgression.Friends.HelperReward.FriendPerPartner
	}
	return s.playerProgression.Friends.HelperReward.OtherPerPartner
}

func (s *Account) CoinState() (int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.coin, s.coinFree
}

type BattlePointStatus struct {
	Current         int
	Max             int
	NextSeconds     int
	IntervalSeconds int
}

func (s *Account) BattlePointState() BattlePointStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.battlePointStatusLocked(time.Now())
}

func (s *Account) refreshBattlePointsLocked(now time.Time) {
	if s.bp >= s.bpMax {
		// Recovery items may exceed the natural cap. Keep the extra points and
		// pause regeneration until spending brings the balance below capacity.
		s.bpNextRecovery = time.Time{}
		return
	}
	if s.bpNextRecovery.IsZero() {
		s.bpNextRecovery = now.Add(s.bpRecoveryInterval)
		return
	}
	if now.Before(s.bpNextRecovery) {
		return
	}
	elapsed := now.Sub(s.bpNextRecovery)
	recovered := 1 + int(elapsed/s.bpRecoveryInterval)
	s.bp += recovered
	if s.bp >= s.bpMax {
		s.bp = s.bpMax
		s.bpNextRecovery = time.Time{}
		return
	}
	s.bpNextRecovery = s.bpNextRecovery.Add(
		time.Duration(recovered) * s.bpRecoveryInterval,
	)
}

func (s *Account) battlePointStatusLocked(now time.Time) BattlePointStatus {
	s.refreshBattlePointsLocked(now)
	// PointTimer.SetTimer(next_sec, heal_sec) consumes a per-point interval,
	// including at capacity: partial BP updates reuse the cached heal_sec.
	status := BattlePointStatus{Current: s.bp, Max: s.bpMax,
		IntervalSeconds: int(s.bpRecoveryInterval / time.Second)}
	if s.bp >= s.bpMax {
		return status
	}
	remaining := s.bpNextRecovery.Sub(now)
	status.NextSeconds = int((remaining + time.Second - 1) / time.Second)
	return status
}

func isLowerSHA256Digest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func cloneReward(reward gamestate.Reward) gamestate.Reward {
	reward.CardSkillLevels = slices.Clone(reward.CardSkillLevels)
	return reward
}
