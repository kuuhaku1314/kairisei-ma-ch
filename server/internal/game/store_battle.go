package game

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"kairisei.local/server/battlecore"
	"kairisei.local/server/internal/gamestate"
)

type TeamBattleContext struct {
	ScoreBossID               int
	MissionPlayers            int
	MissionArthur             int8
	StageType                 gamestate.BattleStageType
	EntryRole                 gamestate.BattleEntryRole
	ClearDecks                []gamestate.BattleClearDeck
	ScorePolicy               *gamestate.TeamBattleScorePolicy
	Seed                      int
	DropPlanSet               bool
	DropPlan                  []gamestate.TeamBattleEnemyDrop
	BossID                    int
	BattleEnemyTypes          []int8
	StageQuestAreaID          int
	StageQuestStageID         int
	TowerID                   int
	TowerFloor                int
	ItemID                    int
	ItemUse                   int
	BPUse                     int
	PrepaidRoomID             int64
	FameSeed                  string
	StartTime                 int
	SoloRequestKey            string
	FameRewardsSet            bool
	FameRewards               []gamestate.Reward
	FameSources               []TeamBattleFameSource
	HostBonusMemberType       int
	FriendPointPartners       int
	FriendPointReward         int
	FriendPointRentalCredits  []FriendPointRentalCredit
	FriendPointRentalEventKey string
	SelectedPartners          []TeamBattleResultPartner
	ContinueAllowed           bool
	ContinueReceipts          []string
}

// A single-player battle has no continuously connected transport. This is the
// last known unsettled state, not proof that the client is still running.
func (s *Account) HasUnsettledSoloBattle() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activeBattle != nil && s.activeBattle.EntryRole == gamestate.BattleEntrySolo
}

type TeamBattleResultPartner struct {
	IsBurst       int8
	LastLoginUnix int64
	UserID        int
	IsSelf        bool
	Name          string
	ArthurType    int8
	Level         int
	DeckRank      int8
	LeaderCardID  int
	LeaderLevel   int
	LeaderFame    int
	Comment       string
	PVPPoint      int
	HonorIDs      []int
}

func teamBattleContextFromRelease(source *gamestate.TeamBattleActiveState) *TeamBattleContext {
	if source == nil {
		return nil
	}
	result := &TeamBattleContext{
		ScoreBossID:               source.ScoreBossID,
		StageType:                 source.StageType,
		EntryRole:                 source.EntryRole,
		ClearDecks:                gamestate.CloneBattleClearDecks(source.ClearDecks),
		ScorePolicy:               gamestate.CloneTeamBattleScorePolicy(source.ScorePolicy),
		Seed:                      source.Seed,
		DropPlanSet:               source.DropPlanSet,
		DropPlan:                  cloneTeamBattleDropPlan(source.DropPlan),
		BossID:                    source.BossID,
		BattleEnemyTypes:          append([]int8(nil), source.BattleEnemyTypes...),
		StageQuestAreaID:          source.StageQuestAreaID,
		StageQuestStageID:         source.StageQuestStageID,
		TowerID:                   source.TowerID,
		TowerFloor:                source.TowerFloor,
		ItemID:                    source.ItemID,
		ItemUse:                   source.ItemUse,
		BPUse:                     source.BPUse,
		PrepaidRoomID:             source.PrepaidRoomID,
		FameSeed:                  source.FameSeed,
		StartTime:                 source.StartTime,
		SoloRequestKey:            source.SoloRequestKey,
		FameRewardsSet:            source.FameRewardsSet,
		FameRewards:               cloneRewards(source.FameRewards),
		HostBonusMemberType:       source.HostBonusMemberType,
		FriendPointPartners:       source.FriendPointPartners,
		FriendPointReward:         source.FriendPointReward,
		FriendPointRentalEventKey: source.FriendPointRentalEventKey,
		ContinueAllowed:           source.ContinueAllowed,
		ContinueReceipts:          append([]string(nil), source.ContinueReceipts...),
	}
	result.FameSources = make([]TeamBattleFameSource, len(source.FameSources))
	for index, fame := range source.FameSources {
		result.FameSources[index] = TeamBattleFameSource{
			MemberType: fame.MemberType, LeaderFame: fame.LeaderFame,
		}
	}
	result.FriendPointRentalCredits = make([]FriendPointRentalCredit, len(source.FriendPointRentalCredits))
	for index, credit := range source.FriendPointRentalCredits {
		result.FriendPointRentalCredits[index] = FriendPointRentalCredit{
			OwnerUserID: credit.OwnerUserID, FriendPoint: credit.FriendPoint,
		}
	}
	result.SelectedPartners = make([]TeamBattleResultPartner, len(source.SelectedPartners))
	for index, partner := range source.SelectedPartners {
		result.SelectedPartners[index] = TeamBattleResultPartner{
			IsBurst:       partner.IsBurst,
			LastLoginUnix: partner.LastLoginUnix,
			UserID:        partner.UserID, IsSelf: partner.IsSelf, Name: partner.Name, ArthurType: partner.ArthurType,
			Level: partner.Level, DeckRank: partner.DeckRank, LeaderCardID: partner.LeaderCardID,
			LeaderLevel: partner.LeaderLevel, LeaderFame: partner.LeaderFame,
			Comment: partner.Comment, PVPPoint: partner.PVPPoint,
			HonorIDs: append([]int(nil), partner.HonorIDs...),
		}
	}
	return result
}

func snapshotTeamBattleContext(source *TeamBattleContext) *gamestate.TeamBattleActiveState {
	if source == nil {
		return nil
	}
	result := &gamestate.TeamBattleActiveState{
		ScoreBossID:               source.ScoreBossID,
		StageType:                 source.StageType,
		EntryRole:                 source.EntryRole,
		ClearDecks:                gamestate.CloneBattleClearDecks(source.ClearDecks),
		ScorePolicy:               gamestate.CloneTeamBattleScorePolicy(source.ScorePolicy),
		Seed:                      source.Seed,
		DropPlanSet:               source.DropPlanSet,
		DropPlan:                  cloneTeamBattleDropPlan(source.DropPlan),
		BossID:                    source.BossID,
		BattleEnemyTypes:          append([]int8(nil), source.BattleEnemyTypes...),
		StageQuestAreaID:          source.StageQuestAreaID,
		StageQuestStageID:         source.StageQuestStageID,
		TowerID:                   source.TowerID,
		TowerFloor:                source.TowerFloor,
		ItemID:                    source.ItemID,
		ItemUse:                   source.ItemUse,
		BPUse:                     source.BPUse,
		PrepaidRoomID:             source.PrepaidRoomID,
		FameSeed:                  source.FameSeed,
		StartTime:                 source.StartTime,
		SoloRequestKey:            source.SoloRequestKey,
		FameRewardsSet:            source.FameRewardsSet,
		FameRewards:               cloneRewards(source.FameRewards),
		HostBonusMemberType:       source.HostBonusMemberType,
		FriendPointPartners:       source.FriendPointPartners,
		FriendPointReward:         source.FriendPointReward,
		FriendPointRentalEventKey: source.FriendPointRentalEventKey,
		ContinueAllowed:           source.ContinueAllowed,
		ContinueReceipts:          append([]string(nil), source.ContinueReceipts...),
	}
	result.FameSources = make([]gamestate.TeamBattleFameSourceState, len(source.FameSources))
	for index, fame := range source.FameSources {
		result.FameSources[index] = gamestate.TeamBattleFameSourceState{
			MemberType: fame.MemberType, LeaderFame: fame.LeaderFame,
		}
	}
	result.FriendPointRentalCredits = make(
		[]gamestate.TeamBattleRentalCreditState, len(source.FriendPointRentalCredits),
	)
	for index, credit := range source.FriendPointRentalCredits {
		result.FriendPointRentalCredits[index] = gamestate.TeamBattleRentalCreditState{
			OwnerUserID: credit.OwnerUserID, FriendPoint: credit.FriendPoint,
		}
	}
	result.SelectedPartners = make([]gamestate.TeamBattleResultPartnerState, len(source.SelectedPartners))
	for index, partner := range source.SelectedPartners {
		result.SelectedPartners[index] = gamestate.TeamBattleResultPartnerState{
			IsBurst:       partner.IsBurst,
			LastLoginUnix: partner.LastLoginUnix,
			UserID:        partner.UserID, IsSelf: partner.IsSelf, Name: partner.Name, ArthurType: partner.ArthurType,
			Level: partner.Level, DeckRank: partner.DeckRank, LeaderCardID: partner.LeaderCardID,
			LeaderLevel: partner.LeaderLevel, LeaderFame: partner.LeaderFame,
			Comment: partner.Comment, PVPPoint: partner.PVPPoint,
			HonorIDs: append([]int(nil), partner.HonorIDs...),
		}
	}
	return result
}

type TeamBattleFameSource struct {
	MemberType int
	LeaderFame int
}

type TeamBattleFameAward struct {
	MemberType int
	RewardKind int
	Result     PresentReceiveResult
}

type teamBattleFameAwardPlan struct {
	MemberType int
	RewardKind int
	Reward     gamestate.Reward
}

type teamBattleSettlement struct {
	Return        *TeamBattleReturn
	RankingScore  *gamestate.BossScoreResult
	ClearRating   *TeamBattleClearRating
	Score         PresentReceiveResult
	ScoreInfo     []any
	Context       TeamBattleContext
	Result        PresentReceiveResult
	FirstClear    PresentReceiveResult
	Fame          []TeamBattleFameAward
	Host          []TeamBattleFameAward
	WasFirstClear bool
}

func stageQuestConfigurations(
	mainQuest json.RawMessage,
	areas []json.RawMessage,
) (map[int]json.RawMessage, int, error) {
	defaultAreaID, err := stageQuestAreaID(mainQuest)
	if err != nil {
		return nil, 0, err
	}
	result := make(map[int]json.RawMessage, max(1, len(areas)))
	for _, area := range areas {
		areaID, areaErr := stageQuestAreaID(area)
		if areaErr != nil {
			return nil, 0, areaErr
		}
		if _, duplicate := result[areaID]; duplicate {
			return nil, 0, fmt.Errorf("duplicate local stage quest area %d", areaID)
		}
		result[areaID] = append(json.RawMessage(nil), area...)
	}
	if len(areas) > 0 {
		if _, exists := result[defaultAreaID]; !exists {
			return nil, 0, errors.New("local default stage quest is absent from the area catalog")
		}
	}
	result[defaultAreaID] = append(json.RawMessage(nil), mainQuest...)
	return result, defaultAreaID, nil
}

func (s *Account) ClearPendingStageQuest() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := s.pendingStageAreaID != 0
	s.pendingStageAreaID = 0
	return changed
}

func (s *Account) validateActiveTeamBattleContextLocked(
	context TeamBattleContext,
	profiles []gamestate.TeamBattleRewardProfile,
) error {
	if context.BossID <= 0 || len(context.BattleEnemyTypes) == 0 ||
		len(context.BattleEnemyTypes) > MaxTeamBattleSegments || context.EntryRole < gamestate.BattleEntrySolo || context.EntryRole > gamestate.BattleEntryMultiplayerGuest {
		return errors.New("local team battle segment contract is unavailable")
	}
	for _, enemyType := range context.BattleEnemyTypes {
		if enemyType < 0 || enemyType > 4 {
			return errors.New("local team battle segment enemy type is invalid")
		}
	}
	if strings.TrimSpace(context.FameSeed) == "" || len(context.FameSources) == 0 ||
		len(context.FameSources) > 4 {
		return errors.New("local team battle fame source is unavailable")
	}
	seenFameMemberTypes := make(map[int]struct{}, len(context.FameSources))
	for _, source := range context.FameSources {
		if source.MemberType < 1 || source.MemberType > 4 || source.LeaderFame <= 0 ||
			source.LeaderFame > 100 {
			return errors.New("local team battle fame source is invalid")
		}
		if _, duplicate := seenFameMemberTypes[source.MemberType]; duplicate {
			return errors.New("local team battle fame source is duplicated")
		}
		seenFameMemberTypes[source.MemberType] = struct{}{}
	}
	if context.HostBonusMemberType < 0 || context.HostBonusMemberType > 4 {
		return errors.New("local team battle host-bonus Arthur type is invalid")
	}
	if context.HostBonusMemberType != 0 {
		if _, exists := seenFameMemberTypes[context.HostBonusMemberType]; !exists {
			return errors.New("local team battle host-bonus Arthur is unavailable")
		}
		if context.EntryRole != gamestate.BattleEntryMultiplayerHost {
			return errors.New("local team battle host must consume battle points")
		}
	}
	friendPointPolicy := s.playerProgression.Friends.HelperReward
	if friendPointPolicy.OtherPerPartner <= 0 ||
		friendPointPolicy.FriendPerPartner <= friendPointPolicy.OtherPerPartner ||
		context.FriendPointPartners < 0 || context.FriendPointPartners > teamBattleMaxPartners {
		return errors.New("local friend-point helper policy is invalid")
	}
	minimumReward := friendPointPolicy.OtherPerPartner * context.FriendPointPartners
	maximumReward := friendPointPolicy.FriendPerPartner * context.FriendPointPartners
	if context.FriendPointReward < minimumReward || context.FriendPointReward > maximumReward {
		return errors.New("local friend-point helper reward is invalid")
	}
	seenRentalOwners := make(map[int]struct{}, len(context.FriendPointRentalCredits))
	rentalReward := 0
	for _, credit := range context.FriendPointRentalCredits {
		if credit.OwnerUserID <= 0 || len(context.FriendPointRentalCredits) > context.FriendPointPartners ||
			(credit.FriendPoint != friendPointPolicy.OtherPerPartner &&
				credit.FriendPoint != friendPointPolicy.FriendPerPartner) {
			return errors.New("local friend-point rental owner is invalid")
		}
		if _, duplicate := seenRentalOwners[credit.OwnerUserID]; duplicate {
			return errors.New("local friend-point rental owner is duplicated")
		}
		seenRentalOwners[credit.OwnerUserID] = struct{}{}
		rentalReward += credit.FriendPoint
	}
	if rentalReward > context.FriendPointReward ||
		(len(context.FriendPointRentalCredits) == 0) !=
			(strings.TrimSpace(context.FriendPointRentalEventKey) == "") {
		return errors.New("local friend-point rental event key is invalid")
	}
	if len(context.SelectedPartners) != 0 &&
		(len(context.SelectedPartners) > 3 || RentalPartnerCount(context.SelectedPartners) != context.FriendPointPartners) {
		return errors.New("local team battle selected-partner projection is invalid")
	}
	seenPartnerUsers := make(map[int]struct{}, len(context.SelectedPartners))
	for _, partner := range context.SelectedPartners {
		if partner.UserID <= 0 || partner.ArthurType < 1 || partner.ArthurType > 4 ||
			partner.Level <= 0 || partner.LeaderCardID <= 0 || partner.LeaderLevel <= 0 ||
			partner.LeaderFame <= 0 || partner.LeaderFame > 100 || partner.PVPPoint < 0 {
			return errors.New("local team battle selected partner is invalid")
		}
		if _, duplicate := seenPartnerUsers[partner.UserID]; duplicate && !partner.IsSelf {
			return errors.New("local team battle selected partner user is duplicated")
		}
		seenPartnerUsers[partner.UserID] = struct{}{}
	}
	stageBattle := context.StageQuestAreaID != 0 || context.StageQuestStageID != 0
	towerBattle := context.TowerID != 0 || context.TowerFloor != 0 ||
		context.ItemID != 0 || context.ItemUse != 0
	if (context.StageQuestAreaID == 0) != (context.StageQuestStageID == 0) ||
		stageBattle && towerBattle || context.BPUse < 0 {
		return errors.New("local team battle context locator is invalid")
	}
	if context.PrepaidRoomID != 0 {
		bpUse, found := s.prepaidTeamBattleCostLocked(context.PrepaidRoomID, context.BossID)
		if !found || context.EntryRole != gamestate.BattleEntryMultiplayerHost || bpUse != context.BPUse || towerBattle {
			return errors.New("multiplayer prepaid start receipt is unavailable")
		}
	}
	switch {
	case stageBattle:
		stageQuest, exists := s.stageQuests[context.StageQuestAreaID]
		if !exists {
			return errors.New("local StageQuest battle context is unavailable")
		}
		stageID, _, found, err := stageQuestBattleForBoss(
			stageQuest, context.StageQuestAreaID, context.BossID,
		)
		if err != nil {
			return err
		}
		if !found || stageID != context.StageQuestStageID || (!context.EntryRole.PaysEntryCost() && context.BPUse != 0) {
			return errors.New("local StageQuest battle context differs from the published stage")
		}
	case towerBattle:
		tower, floor, found := s.towerQuestBattleForBossLocked(context.BossID)
		if !found || tower.TowerID != context.TowerID || floor.Floor != context.TowerFloor ||
			context.ItemID < 0 || context.ItemUse < 0 {
			return errors.New("local tower battle context differs from the published floor")
		}
	default:
		if !context.EntryRole.PaysEntryCost() && context.BPUse != 0 {
			return errors.New("local non-host team battle must not consume battle points")
		}
	}
	if _, found := TeamBattleRewardProfileForContext(profiles, context); !found {
		return errors.New("local team battle reward profile is unavailable")
	}
	return nil
}

func (s *Account) AbandonSoloBattle() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeBattle != nil && s.activeBattle.EntryRole == gamestate.BattleEntrySolo {
		s.activeBattle = nil
	}
}

func (s *Account) ActiveTeamBattleEnemyTypes(bossID int) ([]int8, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.activeBattle == nil || s.activeBattle.BossID != bossID || len(s.activeBattle.BattleEnemyTypes) == 0 {
		return nil, false
	}
	return append([]int8(nil), s.activeBattle.BattleEnemyTypes...), true
}

func (s *Account) towerQuestBattleForBossLocked(
	bossID int,
) (gamestate.TowerQuestProfile, gamestate.TowerQuestFloorProfile, bool) {
	for _, profile := range s.towerQuestProfiles {
		for _, floor := range profile.Floors {
			var identity struct {
				BossID int `json:"0"`
			}
			if json.Unmarshal(floor.Boss, &identity) == nil && identity.BossID == bossID {
				return profile, floor, true
			}
		}
	}
	return gamestate.TowerQuestProfile{}, gamestate.TowerQuestFloorProfile{}, false
}

func (s *Account) TowerQuestHasBoss(bossID int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, _, found := s.towerQuestBattleForBossLocked(bossID)
	return found
}

// continueTeamBattle applies the active local paid solo-continue policy.
// The managed client contract handles CONTINUE_MODE.FREE entirely on-device;
// both COIN and ROOKIE send pay_type 3. The current SoloStart profile advertises
// no rookie or unlimited-free continues. The fifty-crystal price is therefore an
// explicit local policy, consumed from the free balance before the paid balance.
func (s *Account) ContinueTeamBattle(report TeamBattleContinueReport, base gamestate.State, persist StatePersister) (int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeBattle == nil {
		return s.coin, s.coinFree, errors.New("team battle continue has no active battle")
	}
	if report.PayType != 3 {
		return s.coin, s.coinFree, errors.New("unsupported team battle continue payment type")
	}
	if s.activeBattle.EntryRole != gamestate.BattleEntrySolo {
		return s.coin, s.coinFree, errors.New("team battle continue requires an active solo battle")
	}
	if !s.activeBattle.ContinueAllowed {
		return s.coin, s.coinFree, errors.New("this battle does not allow continues")
	}
	if err := validateNativeTeamBattleContinueReport(report.Progress, report.InputCommands, report.EnemyDeadBits, s.activeBattle.BattleEnemyTypes); err != nil {
		return s.coin, s.coinFree, err
	}
	// A retry carries the same accumulated native report. Commit its receipt
	// in this battle's in-memory session together with the currency debit.
	encoded, err := json.Marshal(report)
	if err != nil {
		return s.coin, s.coinFree, err
	}
	receipt := fmt.Sprintf("%x", sha256.Sum256(encoded))
	for _, previous := range s.activeBattle.ContinueReceipts {
		if previous == receipt {
			return s.coin, s.coinFree, nil
		}
	}
	if s.coin+s.coinFree < continueCrystalCost {
		return s.coin, s.coinFree, errors.New("insufficient crystals for team battle continue")
	}
	previousCoin, previousFree := s.coin, s.coinFree
	previousReceipts := s.activeBattle.ContinueReceipts
	freeSpend := min(s.coinFree, continueCrystalCost)
	s.coinFree -= freeSpend
	s.coin -= continueCrystalCost - freeSpend
	s.activeBattle.ContinueReceipts = append(append([]string(nil), previousReceipts...), receipt)
	if persist != nil {
		if err := persist(s.snapshotLocked(base)); err != nil {
			s.coin, s.coinFree = previousCoin, previousFree
			s.activeBattle.ContinueReceipts = previousReceipts
			return s.coin, s.coinFree, fmt.Errorf("persist team battle continue: %w", err)
		}
	}
	return s.coin, s.coinFree, nil
}

func planTeamBattleFameAwards(profile gamestate.TeamBattleRewardProfile, context TeamBattleContext) ([]teamBattleFameAwardPlan, error) {
	if strings.TrimSpace(context.FameSeed) == "" || len(context.FameSources) == 0 {
		return nil, errors.New("team battle fame-bonus context is invalid")
	}
	pool := context.FameRewards
	if !context.FameRewardsSet {
		pool = TeamBattleFamePool(profile)
	}
	if len(pool) == 0 {
		// The local onboarding/training profile has no inventory-bearing drop.
		// Keeping that one route fame-empty is safer than inventing a second pool.
		return nil, nil
	}
	sources := append([]TeamBattleFameSource(nil), context.FameSources...)
	sort.Slice(sources, func(left, right int) bool {
		return sources[left].MemberType < sources[right].MemberType
	})
	plans := make([]teamBattleFameAwardPlan, 0, len(sources)+1)
	for _, source := range sources {
		effectiveFame := min(teamBattleFameMaximum, source.LeaderFame)
		if effectiveFame <= 0 {
			continue
		}
		if teamBattleFameRoll(context.FameSeed, source.MemberType) <= effectiveFame {
			plans = append(plans, teamBattleFameAwardPlan{
				MemberType: source.MemberType,
				RewardKind: TeamBattleRewardFame,
				Reward:     teamBattleFameReward(pool, context.FameSeed, source.MemberType, 0),
			})
		}
		if effectiveFame >= teamBattleFameMaximum {
			plans = append(plans, teamBattleFameAwardPlan{
				MemberType: source.MemberType,
				RewardKind: TeamBattleRewardFullFame,
				Reward:     teamBattleFameReward(pool, context.FameSeed, source.MemberType, 1),
			})
		}
	}
	return plans, nil
}

func teamBattleFameRoll(seed string, memberType int) int {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|member=%d|fame-normal", seed, memberType)))
	return int(binary.BigEndian.Uint64(sum[:8])%uint64(teamBattleFameMaximum)) + 1
}

func planTeamBattleHostAwards(profile gamestate.TeamBattleRewardProfile, context TeamBattleContext) ([]teamBattleFameAwardPlan, error) {
	if context.HostBonusMemberType == 0 {
		return nil, nil
	}
	if context.EntryRole != gamestate.BattleEntryMultiplayerHost || context.HostBonusMemberType < 1 || context.HostBonusMemberType > 4 {
		return nil, errors.New("local team battle host-bonus context is invalid")
	}
	for _, reward := range profile.ResultRewards {
		if teamBattleBonusRewardType(reward.Type) {
			return []teamBattleFameAwardPlan{{
				MemberType: context.HostBonusMemberType,
				RewardKind: TeamBattleRewardHost,
				Reward:     reward,
			}}, nil
		}
	}
	// The local onboarding/training profile has no inventory-bearing drop.
	return nil, nil
}

func (s *Account) CompleteTeamBattle(
	bossID int,
	isClear bool,
	profiles []gamestate.TeamBattleRewardProfile,
	dropReports ...TeamBattleDropReport,
) (teamBattleSettlement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeBattle == nil || s.activeBattle.BossID != bossID {
		return teamBattleSettlement{}, errors.New("local team battle result has no matching start")
	}
	context := *s.activeBattle
	profile, found := TeamBattleRewardProfileForContext(profiles, context)
	if !found {
		return teamBattleSettlement{}, errors.New("local team battle reward profile is unavailable")
	}
	report := TeamBattleDropReport{}
	if len(dropReports) > 0 {
		report = dropReports[0]
	}
	completedAt := s.missionTimeLocked(time.Now())
	settlement, err := s.completeTeamBattleLocked(context, profile, isClear, report, completedAt)
	if err == nil {
		if context.TowerID != 0 {
			floor := s.towerQuestProgress[context.TowerID].Floor
			if floor == 0 {
				// Completed towers retain a positive locator for the tower page.
				floor = context.TowerFloor
			}
			settlement.Return = &TeamBattleReturn{BossID: context.BossID, TowerID: context.TowerID,
				TowerFloor: floor, StageType: context.StageType}
		}
		s.activeBattle = nil
	}
	return settlement, err
}

// CompleteMultiplayerBattle settles an accepted room without entering or
// replacing the account's independent solo run or navigation state.
func (s *Account) CompleteMultiplayerBattle(context TeamBattleContext, plan gamestate.TeamBattleSettlementPlan, isClear bool, report TeamBattleDropReport, completedAt time.Time) (teamBattleSettlement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if context.EntryRole != gamestate.BattleEntryMultiplayerHost && context.EntryRole != gamestate.BattleEntryMultiplayerGuest {
		return teamBattleSettlement{}, errors.New("multiplayer settlement role is invalid")
	}
	if plan.Profile.BossID != context.BossID || len(plan.EnemyTypes) == 0 {
		return teamBattleSettlement{}, errors.New("multiplayer settlement plan is unavailable")
	}
	context.BattleEnemyTypes = plan.EnemyTypes
	if context.EntryRole == gamestate.BattleEntryMultiplayerHost {
		cost, paid := s.prepaidTeamBattleCostLocked(context.PrepaidRoomID, context.BossID)
		if !paid {
			return teamBattleSettlement{}, errors.New("multiplayer prepaid start receipt is unavailable")
		}
		context.BPUse = cost
	}
	return s.completeTeamBattleLocked(context, plan.Profile, isClear, report, completedAt)
}

func (s *Account) completeTeamBattleLocked(context TeamBattleContext, profile gamestate.TeamBattleRewardProfile, isClear bool, report TeamBattleDropReport, completedAt time.Time) (teamBattleSettlement, error) {
	bossID := context.BossID
	settlement := teamBattleSettlement{Context: context}
	// Freeze operator policy at entry; multiplayer carries the room's snapshot.
	if context.ScorePolicy != nil {
		profile.ScorePolicy = context.ScorePolicy
	}
	if report.ScorePolicy != nil {
		profile.ScorePolicy = report.ScorePolicy
	}
	if context.ScorePolicy == nil && report.ScorePolicy == nil && profile.ScorePolicy != nil && profile.ScorePolicy.IsDamageScore() {
		// A pre-upgrade ordinary fight has no accepted score contract.
		profile.ScorePolicy = nil
	}
	if profile.ScorePolicy != nil && profile.ScorePolicy.IsDamageScore() {
		if err := gamestate.ValidateTeamBattleScoreDamage(profile.ScorePolicy, report.ScoreDamage); err != nil {
			return teamBattleSettlement{}, err
		}
		if !report.ScoreVerified {
			return teamBattleSettlement{}, errors.New("score battle requires verified damage")
		}
	}
	scoreBossID := context.ScoreBossID
	if scoreBossID == 0 {
		scoreBossID = bossID
	}
	scores := s.battleScoresLocked(report.SoloChallenge)
	if !isClear {
		if context.TowerID != 0 {
			if err := s.settleTowerQuestLocked(context, false); err != nil {
				return teamBattleSettlement{}, err
			}
		}
		if report.ScoreVerified && profile.ScorePolicy != nil && profile.ScorePolicy.IsDamageScore() {
			progress, rewards, info := planTeamBattleScore(profile.ScorePolicy, scores[scoreBossID], report)
			if err := s.validateSettlementRewardsLocked(rewards); err != nil {
				return teamBattleSettlement{}, err
			}
			for _, reward := range rewards {
				if err := s.applySettlementRewardLocked(reward, &settlement.Score); err != nil {
					return teamBattleSettlement{}, err
				}
			}
			scores[scoreBossID], settlement.ScoreInfo = progress, info
			settlement.setSoloRankingScore(profile.ScorePolicy, report, scoreBossID)
		}
		return settlement, nil
	}
	if context.DropPlanSet && !report.Authoritative && len(report.EnemyDeadBits) != len(context.BattleEnemyTypes) {
		return teamBattleSettlement{}, errors.New("team battle drop report does not match its started plan")
	}
	clearRating, err := s.planTowerQuestRatingLocked(context, report.Turns)
	if err != nil {
		return teamBattleSettlement{}, err
	}
	settlement.ClearRating = clearRating
	profile.ResultRewards = SettledTeamBattleRewards(profile, context, report)
	if report.FameRewardsSet {
		context.FameRewardsSet, context.FameRewards = true, report.FameRewards
	}
	scoreProgress, scoreRewards, scoreInfo := planTeamBattleScore(profile.ScorePolicy, scores[scoreBossID], report)
	settlement.ScoreInfo = scoreInfo

	updatedMainQuest := append(json.RawMessage(nil), s.mainQuest...)
	updatedStageQuestAreaID := 0
	completedStageQuestArea := false
	if context.StageQuestAreaID != 0 {
		stageQuest, exists := s.stageQuests[context.StageQuestAreaID]
		if !exists {
			return teamBattleSettlement{}, errors.New("local StageQuest area is unavailable")
		}
		var err error
		updatedMainQuest, settlement.WasFirstClear, err = markStageQuestClear(
			stageQuest,
			context.StageQuestAreaID,
			context.StageQuestStageID,
		)
		if err != nil {
			return teamBattleSettlement{}, err
		}
		_, err = s.teamBattleSolo.firstClear(context.BossID, context.StageQuestAreaID)
		if err != nil {
			return teamBattleSettlement{}, err
		}
		completedStageQuestArea, err = stageQuestAllStagesCleared(updatedMainQuest)
		if err != nil {
			return teamBattleSettlement{}, err
		}
		updatedStageQuestAreaID = context.StageQuestAreaID
	} else if context.TowerID != 0 {
		progress, exists := s.towerQuestProgress[context.TowerID]
		if !exists {
			return teamBattleSettlement{}, errors.New("local tower progress is unavailable")
		}
		settlement.WasFirstClear = !slices.Contains(progress.ClearedFloors, context.TowerFloor)
	} else {
		var err error
		settlement.WasFirstClear, err = s.teamBattleSolo.firstClear(context.BossID, 0)
		if err != nil {
			return teamBattleSettlement{}, err
		}
	}
	rewards := append([]gamestate.Reward(nil), profile.ResultRewards...)
	rewards = append(rewards, scoreRewards...)
	if settlement.WasFirstClear {
		rewards = append(rewards, profile.FirstClearRewards...)
	}
	famePlans, err := planTeamBattleFameAwards(profile, context)
	if err != nil {
		return teamBattleSettlement{}, err
	}
	hostPlans, err := planTeamBattleHostAwards(profile, context)
	if err != nil {
		return teamBattleSettlement{}, err
	}
	for _, plan := range famePlans {
		rewards = append(rewards, plan.Reward)
	}
	for _, plan := range hostPlans {
		rewards = append(rewards, plan.Reward)
	}
	helperReward := gamestate.Reward{}
	if context.FriendPointReward > 0 {
		helperReward = gamestate.Reward{
			Type: 9, Num: context.FriendPointReward,
			CardSkillLevels: []int16{},
		}
		rewards = append(rewards, helperReward)
	}
	if err := s.validateSettlementRewardsLocked(rewards); err != nil {
		return teamBattleSettlement{}, err
	}
	for _, reward := range scoreRewards {
		if err := s.applySettlementRewardLocked(reward, &settlement.Score); err != nil {
			return teamBattleSettlement{}, err
		}
	}
	for _, reward := range profile.ResultRewards {
		if err := s.applySettlementRewardLocked(reward, &settlement.Result); err != nil {
			return teamBattleSettlement{}, err
		}
	}
	if helperReward.Num > 0 {
		if err := s.applySettlementRewardLocked(helperReward, &settlement.Result); err != nil {
			return teamBattleSettlement{}, err
		}
	}
	if settlement.WasFirstClear {
		for _, reward := range profile.FirstClearRewards {
			if err := s.applySettlementRewardLocked(reward, &settlement.FirstClear); err != nil {
				return teamBattleSettlement{}, err
			}
		}
	}
	for _, plan := range famePlans {
		award := TeamBattleFameAward{
			MemberType: plan.MemberType,
			RewardKind: plan.RewardKind,
		}
		if err := s.applySettlementRewardLocked(plan.Reward, &award.Result); err != nil {
			return teamBattleSettlement{}, err
		}
		settlement.Fame = append(settlement.Fame, award)
	}
	for _, plan := range hostPlans {
		award := TeamBattleFameAward{
			MemberType: plan.MemberType,
			RewardKind: plan.RewardKind,
		}
		if err := s.applySettlementRewardLocked(plan.Reward, &award.Result); err != nil {
			return teamBattleSettlement{}, err
		}
		settlement.Host = append(settlement.Host, award)
	}
	if updatedStageQuestAreaID != 0 {
		s.stageQuests[updatedStageQuestAreaID] = append(json.RawMessage(nil), updatedMainQuest...)
		if updatedStageQuestAreaID == s.defaultStageQuestAreaID {
			s.mainQuest = append(json.RawMessage(nil), updatedMainQuest...)
		}
	}
	if profile.ScorePolicy != nil {
		scores[scoreBossID] = scoreProgress
		settlement.setSoloRankingScore(profile.ScorePolicy, report, scoreBossID)
	}
	if context.TowerID == 0 {
		s.teamBattleSolo.markClear(context.BossID, context.StageQuestAreaID)
	}
	if context.TowerID != 0 {
		if err := s.settleTowerQuestLocked(context, true); err != nil {
			return teamBattleSettlement{}, err
		}
		s.recordTowerQuestRatingLocked(context, clearRating, report.Turns)
	}
	onboardingEventKind := "activity"
	if context.StageQuestAreaID != 0 {
		onboardingEventKind = "battle"
		if completedStageQuestArea {
			onboardingEventKind = "battle_area"
		}
	}
	if err := s.advanceOnboardingLocked(onboardingEvent{
		kind: onboardingEventKind, areaID: context.StageQuestAreaID,
		stageID:      context.StageQuestStageID,
		activityBoss: context.StageQuestAreaID == 0 && context.TowerID == 0,
	}); err != nil {
		return teamBattleSettlement{}, err
	}
	// Ordinary scripted victories remain clears. Score challenges additionally
	// require body defeat, as stated by the native quest mission descriptions.
	scoreChallenge := profile.ScorePolicy != nil || context.StageType == gamestate.BattleStageScore || context.StageType == gamestate.BattleStageMainScore
	if !scoreChallenge || battlecore.DefeatedEnemyParties(context.BattleEnemyTypes, report.EnemyDeadBits) {
		event := missionEvent{kind: gamestate.MissionQuestClear, stageType: context.StageType, entryRole: context.EntryRole, bossID: context.BossID, players: context.MissionPlayers, arthurType: context.MissionArthur}
		if context.EntryRole == gamestate.BattleEntrySolo {
			event.players = 1
			if len(context.ClearDecks) > 0 {
				event.arthurType = int8(context.ClearDecks[0].ArthurType)
				event.ownDecks = len(context.ClearDecks) == 4
				for _, deck := range context.ClearDecks {
					if deck.UserID != context.ClearDecks[0].UserID {
						event.ownDecks = false
					}
				}
			}
		}
		if len(scoreInfo) > 0 {
			event.scoreGrade = scoreInfo[0].(map[string]any)["grade"].(int)
		}
		s.markMissionMetricLocked(gamestate.MissionQuestCleared)
		s.recordMissionEventAtLocked(event, completedAt)
	}
	return settlement, nil
}

func (s *Account) settleTowerQuestLocked(context TeamBattleContext, isClear bool) error {
	profile, exists := s.towerQuestProfiles[context.TowerID]
	if !exists || context.TowerFloor <= 0 || context.TowerFloor > len(profile.Floors) ||
		profile.Floors[context.TowerFloor-1].Floor != context.TowerFloor {
		return errors.New("local tower battle context is invalid")
	}
	progress, exists := s.towerQuestProgress[context.TowerID]
	if !exists {
		return errors.New("local tower progress is unavailable")
	}
	progress.LastBattleFloor = context.TowerFloor
	progress.LastRankUp = false
	if context.TowerFloor != progress.Floor {
		// Replays still settle drops and best ratings through the caller. They
		// do not advance the current challenge or produce its failure effects.
		progress.LastResult = ""
		progress.LastResultLoseCount = 0
		s.towerQuestProgress[context.TowerID] = progress
		return nil
	}
	if isClear {
		previousLoseCount := progress.LoseCount
		previousRank := profile.Floors[context.TowerFloor-1].Rank
		progress.LastResult = "win"
		progress.LastResultLoseCount = previousLoseCount
		if !slices.Contains(progress.ClearedFloors, context.TowerFloor) {
			progress.ClearedFloors = append(progress.ClearedFloors, context.TowerFloor)
			sort.Ints(progress.ClearedFloors)
		}
		if context.TowerFloor == len(profile.Floors) {
			progress.Floor = 0
			progress.LoseCount = 0
		} else {
			progress.Floor = context.TowerFloor + 1
			progress.LastRankUp = profile.Floors[progress.Floor-1].Rank > previousRank
			if progress.LastRankUp {
				progress.LoseCount = 0
			}
		}
	} else {
		progress.LastResult = "lose"
		progress.LoseCount++
		progress.LastResultLoseCount = progress.LoseCount
		if progress.LoseCount >= profile.LoseCountMax {
			rank := profile.Floors[context.TowerFloor-1].Rank
			progress.Floor = context.TowerFloor
			for _, floor := range profile.Floors {
				if floor.Rank == rank {
					progress.Floor = floor.Floor
					break
				}
			}
			progress.LoseCount = 0
		}
	}
	s.towerQuestProgress[context.TowerID] = progress
	return nil
}

func (s *Account) TeamBattleSoloState() json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	catalog, err := s.teamBattleCatalogLocked()
	if err == nil {
		if projected, err := catalog.MarshalJSON(); err == nil {
			return projected
		}
	}
	return filterBattleCatalog(append(json.RawMessage(nil), s.teamBattleSolo.saved()...), s.battlePublicationDisabledBossesLocked())
}

// TeamBattleCatalog gives protocol adapters a request-owned view without an
// intermediate JSON encode/decode. Saved progression remains account-owned.
func (s *Account) TeamBattleCatalog() (*TeamBattleCatalog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.teamBattleCatalogLocked()
}

func (s *Account) teamBattleCatalogLocked() (*TeamBattleCatalog, error) {
	s.teamBattleSolo.expire(time.Now().Unix())
	catalog, err := s.teamBattleSolo.view()
	if err != nil {
		return nil, err
	}
	tutorialNormalQuest := s.onboarding.Step == 0
	tutorialActivity := s.onboarding.Step == gamestate.OnboardingStepCount-1
	if err := catalog.project(s.stageQuests, s.teamBattleLimitedGroupIDs, tutorialNormalQuest, tutorialActivity); err != nil {
		return nil, err
	}
	catalog.filterDisabled(s.battlePublicationDisabledBossesLocked())
	return catalog, nil
}

type userBuffExecResult struct {
	Bosses []json.RawMessage
	Item   gamestate.Item
}

func (s *Account) ExecUserBuff(userBuffID int, now time.Time) (userBuffExecResult, error) {
	if userBuffID <= 0 {
		return userBuffExecResult{}, errors.New("invalid user-buff ID")
	}
	nowUnix := now.Unix()
	if nowUnix <= 0 {
		return userBuffExecResult{}, errors.New("invalid user-buff execution time")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	profile, exists := s.userBuffProfiles[userBuffID]
	if !exists {
		return userBuffExecResult{}, errors.New("unknown user-buff ID")
	}
	expiresUnix := nowUnix + int64(profile.DurationSeconds)
	if expiresUnix <= nowUnix || expiresUnix > int64(^uint32(0)>>1) {
		return userBuffExecResult{}, errors.New("invalid user-buff expiration")
	}

	if s.teamBattleSolo == nil || s.teamBattleSolo.definitions == nil {
		return userBuffExecResult{}, errors.New("user-buff catalog is unavailable")
	}
	targets := s.teamBattleSolo.definitions.buffs[userBuffID]
	if len(targets) == 0 {
		return userBuffExecResult{}, errors.New("user-buff ID is not referenced by a published boss")
	}
	for _, id := range targets {
		progress := s.teamBattleSolo.progress[id]
		if progress.State != 0 && (progress.Until == 0 || progress.Until > nowUnix) {
			return userBuffExecResult{}, errors.New("user-buff boss is already unlocked")
		}
	}
	item, exists := s.items[profile.ItemID]
	if !exists || item.Num < profile.RequiredNum ||
		item.LimitTime > 0 && nowUnix >= int64(item.LimitTime) {
		return userBuffExecResult{}, ErrInsufficientMaterials
	}
	changedBosses := make([]json.RawMessage, 0, len(targets))
	ids := append([]battleBossKey(nil), targets...)
	sort.Slice(ids, func(i, j int) bool { return ids[i].BossID < ids[j].BossID })
	seen := map[int]bool{}
	progress := battleBossProgress{State: 1, Until: expiresUnix}
	for _, key := range ids {
		id := key.BossID
		if seen[id] {
			continue
		}
		seen[id] = true
		fields := cloneCatalogFields(s.teamBattleSolo.definitions.bosses[id].fields)
		applyBossProgress(fields, progress)
		encoded, err := json.Marshal(fields)
		if err != nil {
			return userBuffExecResult{}, err
		}
		changedBosses = append(changedBosses, encoded)
	}
	item.Num -= profile.RequiredNum
	s.items[item.ItemID] = item
	for _, id := range ids {
		s.teamBattleSolo.progress[id] = progress
	}
	s.teamBattleSolo.encoded = nil
	return userBuffExecResult{Bosses: changedBosses, Item: item}, nil
}

func (s *Account) TeamBattleSchedulePushState(isSolo int) []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := s.teamBattleScheduleMultiPush
	if isSolo == 1 {
		values = s.teamBattleScheduleSoloPush
	}
	result := make([]int, 0, len(values))
	for groupID := range values {
		result = append(result, groupID)
	}
	sort.Ints(result)
	return result
}

func (s *Account) ToggleTeamBattleSchedulePush(isSolo int, groupID int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	values := s.teamBattleScheduleMultiPush
	if isSolo == 1 {
		values = s.teamBattleScheduleSoloPush
	}
	if _, enabled := values[groupID]; enabled {
		delete(values, groupID)
		return false
	}
	values[groupID] = struct{}{}
	return true
}

func (s *Account) TeamBattleResultReceipt(roomID int64) (json.RawMessage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	receipt, exists := s.teamBattleReceipts[roomID]
	if !exists {
		return nil, false
	}
	return append(json.RawMessage(nil), receipt.Snapshot...), true
}

func (s *Account) RecordTeamBattleResultReceipt(roomID int64, snapshot json.RawMessage, claimedAt time.Time) error {
	if roomID <= 0 || claimedAt.IsZero() || len(snapshot) == 0 || len(snapshot) > MaxRequestBytes || !json.Valid(snapshot) {
		return errors.New("team battle result receipt is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, exists := s.teamBattleReceipts[roomID]; exists {
		if string(existing.Snapshot) != string(snapshot) {
			return errors.New("team battle result receipt conflicts with the completed room")
		}
		return nil
	}
	if len(s.teamBattleReceipts) >= maxBattleReceipts {
		var oldestRoomID int64
		var oldestClaimedAt int64
		for existingRoomID, receipt := range s.teamBattleReceipts {
			if oldestRoomID == 0 || receipt.ClaimedAtUnix < oldestClaimedAt ||
				(receipt.ClaimedAtUnix == oldestClaimedAt && existingRoomID < oldestRoomID) {
				oldestRoomID = existingRoomID
				oldestClaimedAt = receipt.ClaimedAtUnix
			}
		}
		delete(s.teamBattleReceipts, oldestRoomID)
	}
	s.teamBattleReceipts[roomID] = gamestate.TeamBattleResultReceipt{
		RoomID:        roomID,
		ClaimedAtUnix: claimedAt.Unix(),
		Snapshot:      append(json.RawMessage(nil), snapshot...),
	}
	return nil
}

func (s *Account) TeamBattleSoloResultReceipt(requestSHA256 string, bossID int) (json.RawMessage, bool) {
	if !isLowerSHA256Digest(requestSHA256) || bossID <= 0 {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	receipt, exists := s.teamBattleSoloReceipts[requestSHA256]
	if !exists || receipt.BossID != bossID {
		return nil, false
	}
	return append(json.RawMessage(nil), receipt.Snapshot...), true
}

func (s *Account) RecordTeamBattleSoloResultReceipt(
	requestSHA256 string,
	bossID int,
	snapshot json.RawMessage,
	claimedAt time.Time,
) error {
	if !isLowerSHA256Digest(requestSHA256) || bossID <= 0 || claimedAt.IsZero() ||
		len(snapshot) == 0 || len(snapshot) > MaxRequestBytes || !json.Valid(snapshot) {
		return errors.New("solo team battle result receipt is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, exists := s.teamBattleSoloReceipts[requestSHA256]; exists {
		if existing.BossID != bossID || string(existing.Snapshot) != string(snapshot) {
			return errors.New("solo team battle result receipt conflicts with the completed report")
		}
		return nil
	}
	if len(s.teamBattleSoloReceipts) >= maxBattleReceipts {
		var oldestDigest string
		var oldestClaimedAt int64
		for digest, receipt := range s.teamBattleSoloReceipts {
			if oldestDigest == "" || receipt.ClaimedAtUnix < oldestClaimedAt ||
				(receipt.ClaimedAtUnix == oldestClaimedAt && digest < oldestDigest) {
				oldestDigest = digest
				oldestClaimedAt = receipt.ClaimedAtUnix
			}
		}
		delete(s.teamBattleSoloReceipts, oldestDigest)
	}
	s.teamBattleSoloReceipts[requestSHA256] = gamestate.TeamBattleSoloResultReceipt{
		RequestSHA256: requestSHA256,
		BossID:        bossID,
		ClaimedAtUnix: claimedAt.Unix(),
		Snapshot:      append(json.RawMessage(nil), snapshot...),
	}
	return nil
}
