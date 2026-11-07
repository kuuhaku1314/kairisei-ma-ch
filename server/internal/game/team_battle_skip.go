package game

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"kairisei.local/server/internal/gamestate"
)

const maxTeamBattleSkipCount = 99

// SkipTeamBattle completes the whole request under the account lock. Its
// caller saves costs and grants in the existing account transaction before
// returning success. The native skip flow has no resumable result receipt.
func (s *Account) SkipTeamBattle(bossID, count int, profiles []gamestate.TeamBattleRewardProfile, enemyTypes []int8) ([]PresentReceiveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if count < 1 || count > maxTeamBattleSkipCount {
		return nil, errors.New("skip count must be between 1 and 99")
	}
	if s.activeBattle != nil {
		return nil, &BusinessError{Code: -1, Message: "请先结束当前战斗，再使用跳过券。"}
	}
	tower, floor, found := s.towerQuestBattleForBossLocked(bossID)
	if !found || !s.towerOpenLocked(tower.TowerID) || s.disabledTeamBattleBossIDs[bossID] {
		return nil, &BusinessError{Code: -1, Message: "这个关卡暂不可快速通关。"}
	}
	progress := s.towerQuestProgress[tower.TowerID]
	if !slices.Contains(progress.ClearedFloors, floor.Floor) || progress.ClearRatings[floor.Floor]&7 != 7 {
		return nil, &BusinessError{Code: -1, Message: "请先获得本关的全部三星评价。"}
	}
	var boss struct {
		SkipItemID int `json:"skip_itemid"`
	}
	if err := json.Unmarshal(floor.Boss, &boss); err != nil || boss.SkipItemID <= 0 {
		return nil, errors.New("tower floor has no skip ticket")
	}
	context := TeamBattleContext{BossID: bossID, TowerID: tower.TowerID, TowerFloor: floor.Floor}
	profile, found := TeamBattleRewardProfileForContext(profiles, context)
	if !found {
		return nil, errors.New("tower skip reward profile is unavailable")
	}
	// The native control charges the entry item OR BP, plus one ticket per run.
	// Combine identical item IDs before checking, so a shared price cannot be
	// checked twice against the same pre-debit balance.
	costs := map[int]int{boss.SkipItemID: count}
	bpUse := floor.BPUse * count
	if floor.ItemID != 0 {
		costs[floor.ItemID] += floor.ItemUse * count
		bpUse = 0
	}
	now := time.Now()
	for id, amount := range costs {
		item := s.items[id]
		if item.LimitTime > 0 && now.Unix() >= int64(item.LimitTime) {
			return nil, ErrItemExpired
		}
		if item.Num < amount {
			return nil, ErrInsufficientMaterials
		}
	}
	s.refreshBattlePointsLocked(now)
	if s.bp < bpUse {
		return nil, &BusinessError{Code: -1, Message: "体力不足，请补充后再试。"}
	}
	// LOCAL_POLICY: each skip rolls the configured normal enemy drops once,
	// treating all eligible enemies as defeated. First-clear, fame, host and
	// score awards are not part of the original skip response.
	seed := NewBattleSeed()
	plans := make([][]gamestate.Reward, count)
	var allRewards []gamestate.Reward
	for index := range plans {
		plan, err := PlanTeamBattleDrops(profile, enemyTypes, fmt.Sprintf("%d|skip=%d", seed, index))
		if err != nil {
			return nil, err
		}
		plans[index] = SettledTeamBattleRewards(profile, context, TeamBattleDropReport{Authoritative: true, ReleasedDrops: plan})
		allRewards = append(allRewards, plans[index]...)
	}
	if err := s.validateSettlementRewardsLocked(allRewards); err != nil {
		return nil, err
	}
	for id, amount := range costs {
		item := s.items[id]
		item.Num -= amount
		s.items[id] = item
	}
	s.bp -= bpUse
	if bpUse > 0 && s.bp < s.bpMax && s.bpNextRecovery.IsZero() {
		s.bpNextRecovery = now.Add(s.bpRecoveryInterval)
	}
	results := make([]PresentReceiveResult, count)
	for index, rewards := range plans {
		for _, reward := range rewards {
			if err := s.applySettlementRewardLocked(reward, &results[index]); err != nil {
				return nil, err
			}
		}
	}
	s.recordMissionEventLocked(missionEvent{kind: gamestate.MissionQuestClear, stageType: tower.StageType, entryRole: gamestate.BattleEntrySolo, bossID: bossID, skipped: true, amount: count}, now)
	return results, nil
}
