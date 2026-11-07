package game

import (
	"errors"
	"strings"
	"time"

	"kairisei.local/server/internal/gamestate"
)

// TeamBattleEntry contains caller-owned admission facts. Begin resolves the
// accepted cost and content plan into a separate immutable battle context.
type TeamBattleEntry struct {
	ScoreBossID               int
	BossID                    int
	StageType                 gamestate.BattleStageType
	EntryRole                 gamestate.BattleEntryRole
	EnemyTypes                []int8
	Profiles                  []gamestate.TeamBattleRewardProfile
	FameSeed                  string
	FameSources               []TeamBattleFameSource
	HostBonusMemberType       int
	FriendPointPartners       int
	FriendPointReward         int
	FriendPointRentalCredits  []FriendPointRentalCredit
	FriendPointRentalEventKey string
	SelectedPartners          []TeamBattleResultPartner
	PrepaidRoomID             int64
}

func (s *Account) BeginTeamBattle(entry TeamBattleEntry) (TeamBattleContext, BattlePointStatus, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var catalog *battleCatalogState
	if entry.EntryRole == gamestate.BattleEntrySolo {
		catalog = s.teamBattleSolo
	}
	return s.beginTeamBattleLocked(entry, catalog)
}

func (s *Account) beginTeamBattleLocked(entry TeamBattleEntry, entryCatalog *battleCatalogState) (TeamBattleContext, BattlePointStatus, bool, error) {
	if entry.EntryRole < gamestate.BattleEntrySolo || entry.EntryRole > gamestate.BattleEntryMultiplayerGuest {
		return TeamBattleContext{}, BattlePointStatus{}, false, errors.New("battle admission role is invalid")
	}
	now := time.Now()
	s.refreshBattlePointsLocked(now)
	if s.activeBattle != nil {
		// Retrying the same start or completed room must not debit again.
		if s.activeBattle.FameSeed == entry.FameSeed && s.activeBattle.BossID == entry.BossID {
			return *teamBattleContextFromRelease(snapshotTeamBattleContext(s.activeBattle)), s.battlePointStatusLocked(now), true, nil
		}
		// An explicit new solo start replaces an abandoned solo run only after
		// all validation/payment below succeeds. Keep multiplayer settlement
		// retries separate from this single-client lifecycle.
		if entry.EntryRole != gamestate.BattleEntrySolo || s.activeBattle.EntryRole != gamestate.BattleEntrySolo {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("a local team battle is already active")
		}
	}
	if len(entry.EnemyTypes) == 0 || len(entry.EnemyTypes) > MaxTeamBattleSegments {
		return TeamBattleContext{}, BattlePointStatus{}, false,
			errors.New("local team battle segment contract is unavailable")
	}
	for _, enemyType := range entry.EnemyTypes {
		if enemyType < 0 || enemyType > 4 {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("local team battle segment enemy type is invalid")
		}
	}
	if strings.TrimSpace(entry.FameSeed) == "" || len(entry.FameSources) == 0 {
		return TeamBattleContext{}, BattlePointStatus{}, false,
			errors.New("local team battle fame source is unavailable")
	}
	seenFameMemberTypes := make(map[int]struct{}, len(entry.FameSources))
	for _, source := range entry.FameSources {
		if source.MemberType < 1 || source.MemberType > 4 || source.LeaderFame <= 0 ||
			source.LeaderFame > 100 {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("local team battle fame source is invalid")
		}
		if _, duplicate := seenFameMemberTypes[source.MemberType]; duplicate {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("local team battle fame source is duplicated")
		}
		seenFameMemberTypes[source.MemberType] = struct{}{}
	}
	if entry.HostBonusMemberType < 0 || entry.HostBonusMemberType > 4 {
		return TeamBattleContext{}, BattlePointStatus{}, false,
			errors.New("local team battle host-bonus Arthur type is invalid")
	}
	if entry.HostBonusMemberType != 0 {
		if _, exists := seenFameMemberTypes[entry.HostBonusMemberType]; !exists {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("local team battle host-bonus Arthur is unavailable")
		}
		if entry.EntryRole != gamestate.BattleEntryMultiplayerHost {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("local team battle host bonus requires room-host admission")
		}
	}
	friendPointPolicy := s.playerProgression.Friends.HelperReward
	if friendPointPolicy.OtherPerPartner <= 0 ||
		friendPointPolicy.FriendPerPartner <= friendPointPolicy.OtherPerPartner ||
		friendPointPolicy.MaximumPartners != 3 ||
		entry.FriendPointPartners < 0 || entry.FriendPointPartners > friendPointPolicy.MaximumPartners {
		return TeamBattleContext{}, BattlePointStatus{}, false,
			errors.New("local friend-point helper policy is invalid")
	}
	minimumReward := friendPointPolicy.OtherPerPartner * entry.FriendPointPartners
	maximumReward := friendPointPolicy.FriendPerPartner * entry.FriendPointPartners
	if entry.FriendPointReward < minimumReward || entry.FriendPointReward > maximumReward {
		return TeamBattleContext{}, BattlePointStatus{}, false,
			errors.New("local friend-point helper reward is invalid")
	}
	seenRentalOwners := make(map[int]struct{}, len(entry.FriendPointRentalCredits))
	rentalReward := 0
	for _, credit := range entry.FriendPointRentalCredits {
		if credit.OwnerUserID <= 0 || len(entry.FriendPointRentalCredits) > entry.FriendPointPartners ||
			(credit.FriendPoint != friendPointPolicy.OtherPerPartner &&
				credit.FriendPoint != friendPointPolicy.FriendPerPartner) {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("local friend-point rental owner is invalid")
		}
		if _, duplicate := seenRentalOwners[credit.OwnerUserID]; duplicate {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("local friend-point rental owner is duplicated")
		}
		seenRentalOwners[credit.OwnerUserID] = struct{}{}
		rentalReward += credit.FriendPoint
	}
	if rentalReward > entry.FriendPointReward ||
		(len(entry.FriendPointRentalCredits) == 0) != (strings.TrimSpace(entry.FriendPointRentalEventKey) == "") {
		return TeamBattleContext{}, BattlePointStatus{}, false,
			errors.New("local friend-point rental event key is invalid")
	}
	if len(entry.SelectedPartners) != 0 && (len(entry.SelectedPartners) > 3 || RentalPartnerCount(entry.SelectedPartners) != entry.FriendPointPartners) {
		return TeamBattleContext{}, BattlePointStatus{}, false,
			errors.New("local team battle selected-partner projection is invalid")
	}
	selectedPartnerCopies := make([]TeamBattleResultPartner, len(entry.SelectedPartners))
	for index, partner := range entry.SelectedPartners {
		if partner.UserID <= 0 || partner.ArthurType < 1 || partner.ArthurType > 4 ||
			partner.Level <= 0 || partner.LeaderCardID <= 0 || partner.LeaderLevel <= 0 || partner.LeaderFame <= 0 {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("local team battle selected partner is invalid")
		}
		partner.HonorIDs = append([]int(nil), partner.HonorIDs...)
		selectedPartnerCopies[index] = partner
	}
	context := TeamBattleContext{
		Seed:   NewBattleSeed(),
		BossID: entry.BossID, BattleEnemyTypes: append([]int8(nil), entry.EnemyTypes...),
		StageType: entry.StageType, EntryRole: entry.EntryRole,
		FameSeed: entry.FameSeed, FameSources: append([]TeamBattleFameSource(nil), entry.FameSources...),
		HostBonusMemberType: entry.HostBonusMemberType, FriendPointPartners: entry.FriendPointPartners,
		FriendPointReward:         entry.FriendPointReward,
		FriendPointRentalCredits:  append([]FriendPointRentalCredit(nil), entry.FriendPointRentalCredits...),
		FriendPointRentalEventKey: entry.FriendPointRentalEventKey,
		SelectedPartners:          selectedPartnerCopies,
	}
	context.PrepaidRoomID = entry.PrepaidRoomID
	if s.pendingStageAreaID != 0 {
		stageQuest, exists := s.stageQuests[s.pendingStageAreaID]
		if !exists {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("selected StageQuest area is unavailable")
		}
		published, err := projectCNStageQuestPublication(stageQuest)
		if err != nil {
			return TeamBattleContext{}, BattlePointStatus{}, false, err
		}
		stageID, bpUse, found, err := releasedStageQuestBattleForBoss(
			published,
			s.pendingStageAreaID,
			entry.BossID,
		)
		if err != nil {
			return TeamBattleContext{}, BattlePointStatus{}, false, err
		}
		if !found {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("selected StageQuest boss is unavailable")
		}
		context.StageQuestAreaID = s.pendingStageAreaID
		context.StageQuestStageID = stageID
		context.BPUse = bpUse
	} else if areaID := entry.BossID / 100; areaID >= cnNormalQuestAreaMin && areaID <= cnNormalQuestAreaMax {
		stageQuest, exists := s.stageQuests[areaID]
		if !exists {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("selected normal quest area is unavailable")
		}
		published, err := projectCNStageQuestPublication(stageQuest)
		if err != nil {
			return TeamBattleContext{}, BattlePointStatus{}, false, err
		}
		stageID, bpUse, found, err := releasedStageQuestBattleForBoss(
			published, areaID, entry.BossID,
		)
		if err != nil {
			return TeamBattleContext{}, BattlePointStatus{}, false, err
		}
		if !found {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("selected normal quest boss is locked or unavailable")
		}
		context.StageQuestAreaID = areaID
		context.StageQuestStageID = stageID
		context.BPUse = bpUse
	} else if tower, floor, found := s.towerQuestBattleForBossLocked(entry.BossID); found {
		if !s.towerOpenLocked(tower.TowerID) {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				&BusinessError{Code: -1, Message: "炼金副本暂未开放。"}
		}
		progress, exists := s.towerQuestProgress[tower.TowerID]
		if !exists || !towerFloorAccessible(progress, floor.Floor) {
			return TeamBattleContext{}, BattlePointStatus{}, false, errors.New("selected tower floor is locked")
		}
		context.TowerID = tower.TowerID
		context.TowerFloor = floor.Floor
		context.ItemID = floor.ItemID
		context.ItemUse = floor.ItemUse
		context.BPUse = floor.BPUse
	}
	if !context.EntryRole.PaysEntryCost() {
		context.BPUse = 0
	}
	if context.PrepaidRoomID != 0 {
		bpUse, found := s.prepaidTeamBattleCostLocked(context.PrepaidRoomID, context.BossID)
		if !found || context.EntryRole != gamestate.BattleEntryMultiplayerHost {
			return TeamBattleContext{}, BattlePointStatus{}, false, errors.New("multiplayer prepaid start receipt is unavailable")
		}
		context.BPUse = bpUse
	}
	// One current catalog per entry transaction; the HTTP view cannot replace
	// this locked validation of prices and rules against the account state.
	if entry.EntryRole == gamestate.BattleEntrySolo {
		rules, found := s.teamBattleEntryRulesForBossLocked(entry.BossID, entryCatalog)
		if !found || !rules.AllowsSolo() {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("solo battle entry is unavailable")
		}
		if rules.OnlyMyDeck != 0 && RentalPartnerCount(entry.SelectedPartners) != 0 {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("this battle requires the player's own decks")
		}
		// Keep the accepted rule with the entry debit and in-memory battle state.
		// Publishing a new schedule or rule must not change an ongoing battle.
		context.ContinueAllowed = rules.Continue != 0
		context.StageType = rules.StageType
		if context.StageQuestAreaID == 0 && context.TowerID == 0 {
			context.BPUse = rules.BPUse
		}
	}
	if err := s.validateActiveTeamBattleContextLocked(context, entry.Profiles); err != nil {
		return TeamBattleContext{}, BattlePointStatus{}, false, err
	}
	profile, _ := TeamBattleRewardProfileForContext(entry.Profiles, context)
	context.ScorePolicy = gamestate.CloneTeamBattleScorePolicy(profile.ScorePolicy)
	context.ScoreBossID = entry.ScoreBossID
	if context.ScoreBossID == 0 {
		context.ScoreBossID = context.BossID
	}
	plan, err := PlanTeamBattleDrops(profile, context.BattleEnemyTypes, context.FameSeed)
	if err != nil {
		return TeamBattleContext{}, BattlePointStatus{}, false, err
	}
	context.DropPlanSet, context.DropPlan = true, plan
	context.FameRewardsSet, context.FameRewards = true, TeamBattleFamePool(profile, s.teamBattleFameBonus)
	for _, reward := range context.FameRewards {
		if err := s.validateRewardLocked(reward); err != nil {
			return TeamBattleContext{}, BattlePointStatus{}, false, err
		}
	}
	for _, drop := range plan {
		if err := s.validateRewardLocked(drop.Reward); err != nil {
			return TeamBattleContext{}, BattlePointStatus{}, false, err
		}
	}
	if context.TowerID != 0 {
		paid, err := s.spendTowerFloorEntryLocked(gamestate.TowerQuestFloorProfile{
			ItemID: context.ItemID, ItemUse: context.ItemUse, BPUse: context.BPUse,
		}, now)
		if err != nil || !paid {
			return context, s.battlePointStatusLocked(now), false, err
		}
	} else if context.EntryRole.PaysEntryCost() && context.PrepaidRoomID == 0 {
		if s.bp < context.BPUse {
			return context, s.battlePointStatusLocked(now), false, nil
		}
		s.bp -= context.BPUse
		if s.bp < s.bpMax && s.bpNextRecovery.IsZero() {
			s.bpNextRecovery = now.Add(s.bpRecoveryInterval)
		}
	} else if !context.EntryRole.PaysEntryCost() {
		context.BPUse = 0
	}
	s.pendingStageAreaID = 0
	if entry.EntryRole == gamestate.BattleEntrySolo {
		// The original result request has no start ID. Receipts belong to the
		// most recent run; two immediate retreats produce identical reports.
		clear(s.teamBattleSoloReceipts)
	}
	active := context
	s.activeBattle = &active
	return context, s.battlePointStatusLocked(now), true, nil
}
