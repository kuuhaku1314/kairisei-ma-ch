package game

import (
	"encoding/json"
	"maps"
	"sort"
	"time"

	"kairisei.local/server/internal/gamestate"
)

// StatePersister is called after a successful local state mutation. The CN
// profile uses it to commit its SQLite save snapshot.
type StatePersister func(gamestate.State) error

// Snapshot prepares a coherent save, including natural recovery and deck-rank
// progress. It is commit preparation, not a pure display query; callers hold
// the account transaction lock and publish only after persistence succeeds.
func (s *Account) Snapshot(base gamestate.State) gamestate.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked(base)
}

func (s *Account) snapshotLocked(base gamestate.State) gamestate.State {
	s.refreshDeckRanksLocked()
	now := time.Now()
	s.refreshStateMissionsLocked(s.missionTimeLocked(now))
	s.refreshAPLocked(now)
	s.refreshBattlePointsLocked(now)

	state := base
	state.InventorySequence = gamestate.InventorySequenceState{Card: s.nextUniqueID, Sphere: s.nextSphereUniqueID, Buddy: s.nextBuddyUniqueID}
	state.BurstProgress = s.burstProgress
	state.StoryTeamBattleSession = s.storyTeamBattleSession
	state.Story.QuestClearedIDs = s.questStoryClearedIDsLocked()
	state.Navigation = gamestate.NavigationState{
		MainStoryID: s.activeMainStoryID, MainStoryCN: s.activeMainStoryCN,
		SubStoryID: s.activeSubStoryID, StageAreaID: s.pendingStageAreaID,
		SubStoryTowerID: s.activeSubStoryTowerID, SubStoryTowerFloor: s.activeSubStoryTowerFloor,
	}
	state.LocalShop = cloneLocalShop(s.localShop)
	state.TeamBattleScores = cloneTeamBattleScores(s.teamBattleScores)
	state.TeamBattleSoloScores = cloneTeamBattleScores(s.teamBattleSoloScores)
	state.User.ActiveArthurType = int(s.currentActiveArthur)
	state.User.ArthurRank = s.highestDeckRank
	state.User.LastHomeDeckRank = s.lastHomeDeckRank
	if leaderUniqueID, leaderCardID, found := s.activeArthurLeaderLocked(); found {
		state.User.LeaderCardUniqueID = leaderUniqueID
		state.User.LeaderCardID = leaderCardID
	}
	state.User.Level = s.currentLevel
	state.User.Experience = s.currentExperience
	state.User.NowLevelExperience = s.currentLevelExperience
	state.User.NextLevelExperience = s.nextLevelExperience
	state.User.Jobs = append([]gamestate.JobParameter(nil), s.currentJobs...)
	state.User.FollowMax = s.currentFollowMax
	state.User.Name = s.currentName
	state.User.Comment = s.currentComment
	state.User.NaviID = s.currentNaviID
	state.User.NaviUnlockFlag = s.naviUnlockFlag
	state.User.SelectableNaviIDs = make([]int8, 0, len(s.selectableNaviIDs))
	for id := range s.selectableNaviIDs {
		state.User.SelectableNaviIDs = append(state.User.SelectableNaviIDs, id)
	}
	sort.Slice(state.User.SelectableNaviIDs, func(left, right int) bool {
		return state.User.SelectableNaviIDs[left] < state.User.SelectableNaviIDs[right]
	})
	state.User.Gold = s.gold
	state.User.FriendPoint = s.friendPoint
	state.FriendPointInboxCursor = s.friendPointInboxCursor
	state.User.Coin = s.coin
	state.User.CoinFree = s.coinFree
	state.User.CardMax = s.cardMax
	state.User.CardContainerMax = s.cardContainerMax
	state.User.PVPPoint = s.pvpPoint
	state.PVP = ClonePVPState(s.pvp)
	state.Spheres = append([]gamestate.Sphere{}, s.spheres...)
	state.Buddies = append([]gamestate.Buddy{}, s.buddies...)
	state.User.BP = s.bp
	state.User.BPMax = s.bpMax
	state.BattlePoint.RecoverySeconds = int(s.bpRecoveryInterval / time.Second)
	state.BattlePoint.NextRecoveryUnix = 0
	if !s.bpNextRecovery.IsZero() {
		state.BattlePoint.NextRecoveryUnix = s.bpNextRecovery.Unix()
	}
	state.Items = make([]gamestate.Item, 0, len(s.items))
	for _, item := range s.items {
		state.Items = append(state.Items, item)
	}
	sort.Slice(state.Items, func(left, right int) bool {
		return state.Items[left].ItemID < state.Items[right].ItemID
	})
	state.ItemShopTabs = cloneItemShopTabs(s.itemShopTabs)
	s.snapshotItemShopProgress(&state)
	state.Gachas = CloneGachaProfiles(s.gachas)
	state.OperatorGachaPlays = maps.Clone(s.operatorGachaPlays)
	state.GachaSelections = make([]gamestate.GachaSelection, 0, len(s.gachaSelections))
	for gachaID, rewards := range s.gachaSelections {
		state.GachaSelections = append(state.GachaSelections, gamestate.GachaSelection{
			GachaID: gachaID,
			Rewards: cloneRewards(rewards),
		})
	}
	sort.Slice(state.GachaSelections, func(left, right int) bool {
		return state.GachaSelections[left].GachaID < state.GachaSelections[right].GachaID
	})
	state.GachaDailyClaims = make([]gamestate.GachaDailyClaim, 0, len(s.gachaDailyClaims))
	gachaDay := gachaLocalDayKey(now)
	for _, claim := range s.gachaDailyClaims {
		if claim.Day == gachaDay {
			state.GachaDailyClaims = append(state.GachaDailyClaims, claim)
		}
	}
	sort.Slice(state.GachaDailyClaims, func(left, right int) bool {
		return state.GachaDailyClaims[left].GachaID < state.GachaDailyClaims[right].GachaID
	})
	state.EventShopPurchases = make([]gamestate.EventShopPurchase, 0, len(s.eventShopPurchases))
	for lineupID, count := range s.eventShopPurchases {
		state.EventShopPurchases = append(state.EventShopPurchases, gamestate.EventShopPurchase{
			LineupID: lineupID,
			Count:    count,
		})
	}
	sort.Slice(state.EventShopPurchases, func(left, right int) bool {
		return state.EventShopPurchases[left].LineupID < state.EventShopPurchases[right].LineupID
	})
	state.TradeShopPurchases = make([]gamestate.TradeShopPurchase, 0, len(s.tradeShopPurchases))
	for lineupID, count := range s.tradeShopPurchases {
		state.TradeShopPurchases = append(state.TradeShopPurchases, gamestate.TradeShopPurchase{
			LineupID: lineupID,
			Count:    count,
		})
	}
	sort.Slice(state.TradeShopPurchases, func(left, right int) bool {
		return state.TradeShopPurchases[left].LineupID < state.TradeShopPurchases[right].LineupID
	})
	state.User.TutorialFlag = s.tutorialFlag
	state.User.UnlockedFeatureIDs = make([]uint, 0, len(s.unlockedFeatureIDs))
	for featureID := range s.unlockedFeatureIDs {
		state.User.UnlockedFeatureIDs = append(state.User.UnlockedFeatureIDs, featureID)
	}
	sort.Slice(state.User.UnlockedFeatureIDs, func(left, right int) bool {
		return state.User.UnlockedFeatureIDs[left] < state.User.UnlockedFeatureIDs[right]
	})
	state.Onboarding = s.onboarding
	state.User.AP = s.ap
	state.Explore.Active = s.exploreActive
	state.Explore.ActiveRewards = s.exploreActiveRewards
	state.Explore.ArthurType = s.exploreArthurType
	state.Explore.DeckIndex = s.exploreDeckIndex
	state.Explore.Stages = cloneExploreStages(s.exploreStages)
	state.Explore.StageCursor = s.exploreStageCursor
	state.Explore.ActiveStageID = s.exploreActiveStage
	state.Explore.StartedAtUnix = 0
	if !s.exploreStartedAt.IsZero() {
		state.Explore.StartedAtUnix = s.exploreStartedAt.Unix()
	}
	state.Explore.APNextRecoveryUnix = 0
	if !s.apNextRecovery.IsZero() {
		state.Explore.APNextRecoveryUnix = s.apNextRecovery.Unix()
	}

	state.Cards = snapshotCards(s.cards, s.cardDefinitions)
	state.ContainerCards = snapshotCards(s.containerCards, s.cardDefinitions)
	state.StackCards = append([]gamestate.CardStack(nil), s.stackCards...)
	state.CardDevelopment = gamestate.CardDevelopmentState{Stive: s.stive}
	if s.cardFameTraining != nil {
		training := *s.cardFameTraining
		state.CardDevelopment.Training = &training
	}
	state.SupportDeck.UnlockSlotNums = append([]int8(nil), s.supportUnlockedSlots...)
	state.SupportDeck.CardCollectionIDs = make([]int, 0, len(s.cardCollectionIDs))
	for cardID := range s.cardCollectionIDs {
		state.SupportDeck.CardCollectionIDs = append(state.SupportDeck.CardCollectionIDs, cardID)
	}
	sort.Ints(state.SupportDeck.CardCollectionIDs)
	state.SupportDeck.CardCollectionLoveMaxIDs = make([]int, 0, len(s.cardCollectionLoveMaxIDs))
	for id := range s.cardCollectionLoveMaxIDs {
		state.SupportDeck.CardCollectionLoveMaxIDs = append(state.SupportDeck.CardCollectionLoveMaxIDs, id)
	}
	sort.Ints(state.SupportDeck.CardCollectionLoveMaxIDs)
	state.Decks = make([]gamestate.Deck, len(s.decks))
	for index, deck := range s.decks {
		state.Decks[index] = gamestate.Deck{
			ArthurType:           deck.ArthurType,
			Index:                deck.Index,
			JobType:              deck.JobType,
			LeaderCardIndex:      deck.LeaderCardIndex,
			CardUniqueIDs:        append([]int64(nil), deck.CardUniqueIDs...),
			SupportCardUniqueIDs: append([]int64(nil), deck.SupportCardUniqueIDs...),
			SphereUniqueIDs:      append([]int64(nil), deck.SphereUniqueIDs...),
			BuddyUniqueIDs:       append([]int64(nil), deck.BuddyUniqueIDs...),
			Name:                 deck.Name,
			IsActive:             deck.IsActive,
			IsRental:             deck.IsRental,
			DeckRank:             deck.DeckRank,
		}
	}
	state.Avatars = cloneAvatars(s.avatars)
	state.Costume = s.costumeStateLocked()
	state.AvatarParts = make([]int, 0, len(s.avatarParts))
	for partID := range s.avatarParts {
		state.AvatarParts = append(state.AvatarParts, partID)
	}
	sort.Ints(state.AvatarParts)
	state.Stamps.StampIDs = make([]int, 0, len(s.stampIDs))
	for stampID := range s.stampIDs {
		state.Stamps.StampIDs = append(state.Stamps.StampIDs, stampID)
	}
	sort.Ints(state.Stamps.StampIDs)
	state.Stamps.DeckStampIDs = append([]int(nil), s.stampDeck...)
	state.Honors.HonorIDs = make([]int, 0, len(s.honorIDs))
	for honorID := range s.honorIDs {
		state.Honors.HonorIDs = append(state.Honors.HonorIDs, honorID)
	}
	sort.Ints(state.Honors.HonorIDs)
	state.Honors.DeckHonorIDs = append([]int(nil), s.deckHonorIDs...)
	state.Story.MainParts = cloneStoryMainParts(s.storyMainParts)
	state.Story.CNMainParts = cloneStoryMainParts(s.cnStoryMainParts)
	state.Story.SubCharacters = cloneStorySubCharacters(s.storySubCharacters)
	state.Story.Events = cloneStoryEvents(s.storyEvents)
	state.Engagement.Initialized = true
	state.Engagement.Missions = s.snapshotMissionsLocked()
	state.Engagement.Visits = s.missionVisits
	state.Engagement.Presents = clonePresents(s.presents)
	state.Engagement.Histories = clonePresents(s.presentHistories)
	state.Engagement.PopupReadIDs = make([]int, 0, len(s.popupReadIDs))
	for popupID := range s.popupReadIDs {
		state.Engagement.PopupReadIDs = append(state.Engagement.PopupReadIDs, popupID)
	}
	sort.Ints(state.Engagement.PopupReadIDs)
	state.LoginBonus = s.loginBonusState
	state.Options.GameEnableFlag = s.gameOptionFlag
	state.Options.PushEnableFlag = s.pushOptionFlag
	defaultStageQuest, exists := s.stageQuests[s.defaultStageQuestAreaID]
	if !exists {
		defaultStageQuest = s.mainQuest
	}
	state.MainQuest = append([]byte(nil), defaultStageQuest...)
	state.StageQuestAreas = nil
	if len(s.stageQuests) > 1 {
		areaIDs := make([]int, 0, len(s.stageQuests))
		for areaID := range s.stageQuests {
			areaIDs = append(areaIDs, areaID)
		}
		sort.Ints(areaIDs)
		state.StageQuestAreas = make([]json.RawMessage, 0, len(areaIDs))
		for _, areaID := range areaIDs {
			state.StageQuestAreas = append(
				state.StageQuestAreas,
				append(json.RawMessage(nil), s.stageQuests[areaID]...),
			)
		}
	}
	state.TeamBattleSolo = append([]byte(nil), s.teamBattleSolo.saved()...)
	state.ActiveTeamBattle = snapshotTeamBattleContext(s.activeBattle)
	state.TeamBattleStartReceipts = append([]gamestate.TeamBattleStartReceipt(nil), s.teamBattleStartReceipts...)
	state.TeamBattleContinueReceipts = append([]gamestate.TeamBattleContinueReceipt(nil), s.teamBattleContinueReceipts...)
	state.TeamBattleSchedule.SoloPushGroupIDs = make([]int, 0, len(s.teamBattleScheduleSoloPush))
	for groupID := range s.teamBattleScheduleSoloPush {
		state.TeamBattleSchedule.SoloPushGroupIDs = append(state.TeamBattleSchedule.SoloPushGroupIDs, groupID)
	}
	sort.Ints(state.TeamBattleSchedule.SoloPushGroupIDs)
	state.TeamBattleSchedule.MultiPushGroupIDs = make([]int, 0, len(s.teamBattleScheduleMultiPush))
	for groupID := range s.teamBattleScheduleMultiPush {
		state.TeamBattleSchedule.MultiPushGroupIDs = append(state.TeamBattleSchedule.MultiPushGroupIDs, groupID)
	}
	sort.Ints(state.TeamBattleSchedule.MultiPushGroupIDs)
	state.TeamBattleResultReceipts = make([]gamestate.TeamBattleResultReceipt, 0, len(s.teamBattleReceipts))
	for _, receipt := range s.teamBattleReceipts {
		receipt.Snapshot = append([]byte(nil), receipt.Snapshot...)
		state.TeamBattleResultReceipts = append(state.TeamBattleResultReceipts, receipt)
	}
	sort.Slice(state.TeamBattleResultReceipts, func(left, right int) bool {
		return state.TeamBattleResultReceipts[left].RoomID < state.TeamBattleResultReceipts[right].RoomID
	})
	state.TeamBattleSoloResultReceipts = make(
		[]gamestate.TeamBattleSoloResultReceipt,
		0,
		len(s.teamBattleSoloReceipts),
	)
	for _, receipt := range s.teamBattleSoloReceipts {
		receipt.Snapshot = append([]byte(nil), receipt.Snapshot...)
		state.TeamBattleSoloResultReceipts = append(state.TeamBattleSoloResultReceipts, receipt)
	}
	sort.Slice(state.TeamBattleSoloResultReceipts, func(left, right int) bool {
		return state.TeamBattleSoloResultReceipts[left].RequestSHA256 <
			state.TeamBattleSoloResultReceipts[right].RequestSHA256
	})
	state.ExploreResultReceipt = nil
	if s.lastExploreResultReceipt != nil {
		receipt := *s.lastExploreResultReceipt
		receipt.Response = append(json.RawMessage(nil), receipt.Response...)
		state.ExploreResultReceipt = &receipt
	}
	state.PVPResultReceipts = make([]gamestate.PVPResultReceipt, 0, len(s.pvpResultReceipts))
	for _, receipt := range s.pvpResultReceipts {
		receipt.Response = append(json.RawMessage(nil), receipt.Response...)
		state.PVPResultReceipts = append(state.PVPResultReceipts, receipt)
	}
	sort.Slice(state.PVPResultReceipts, func(left, right int) bool {
		return state.PVPResultReceipts[left].BattleID < state.PVPResultReceipts[right].BattleID
	})
	state.TowerQuestProgress = make([]gamestate.TowerQuestProgress, 0, len(s.towerQuestProgress))
	for _, progress := range s.towerQuestProgress {
		state.TowerQuestProgress = append(state.TowerQuestProgress, cloneTowerQuestProgress(progress))
	}
	sort.Slice(state.TowerQuestProgress, func(left, right int) bool {
		return state.TowerQuestProgress[left].TowerID < state.TowerQuestProgress[right].TowerID
	})
	return state
}

func snapshotCards(
	cards []CardInfo,
	definitions map[int]gamestate.Card,
) []gamestate.Card {
	result := make([]gamestate.Card, len(cards))
	for index, card := range cards {
		persisted := definitions[card.CardID]
		persisted.UniqueID = card.UniqueID
		persisted.CreateTime = card.CreateTime
		persisted.CardID = card.CardID
		persisted.Level = card.Level
		persisted.LevelMax = card.LevelMax
		persisted.Experience = card.Experience
		persisted.NowLevelExperience = card.NowLevelEXP
		persisted.Love = card.Love
		persisted.SkillLevels = append([]int16(nil), card.SkillLevels...)
		persisted.HP = card.HP
		persisted.Attack = card.Attack
		persisted.Magic = card.Magic
		persisted.Mind = card.Mind
		persisted.NextLevelExperience = card.NextLevelEXP
		persisted.AddExperience = card.AddExperience
		persisted.BaseAddPrice = card.BaseAddPrice
		persisted.IsLock = card.IsLock
		persisted.Fame = card.Fame
		result[index] = persisted
	}
	return result
}
