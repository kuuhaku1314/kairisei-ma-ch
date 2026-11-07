package game

import (
	"time"

	"kairisei.local/server/internal/gamestate"
)

func inventoryReward(reward gamestate.Reward) bool {
	switch reward.Type {
	case 6, 8, 15, 19:
		return true
	default:
		return false
	}
}

// Settlement cannot be undone by freeing bag space. Validate definitions and
// direct balances first; full inventory rewards are delivered through presents.
func (s *Account) validateSettlementRewardsLocked(rewards []gamestate.Reward) error {
	direct := make([]gamestate.Reward, 0, len(rewards))
	for _, reward := range rewards {
		if err := s.validateRewardLocked(reward); err != nil {
			return err
		}
		if !inventoryReward(reward) {
			direct = append(direct, reward)
		}
	}
	return s.validateRewardBatchCapacityLocked(direct)
}

func (s *Account) applySettlementRewardLocked(reward gamestate.Reward, result *PresentReceiveResult) error {
	return s.applyRewardOrPresentLocked(reward, result, "结算奖励")
}

func (s *Account) applyRewardOrPresentLocked(reward gamestate.Reward, result *PresentReceiveResult, title string) error {
	if !inventoryReward(reward) || s.validateRewardBatchCapacityLocked([]gamestate.Reward{reward}) == nil {
		return s.applyRewardLocked(reward, result)
	}
	stored := s.appendRewardPresentLocked(reward, title, "")
	result.Rewards = append(result.Rewards, ReceivedReward{Reward: stored, UniqueID: []int64{}, InPresentBox: true})
	result.InPresentBox = true
	return nil
}

func (s *Account) appendRewardPresentLocked(reward gamestate.Reward, title, comment string) gamestate.Reward {
	// Account request serialization and the store lock keep allocated mail and
	// the triggering business change in the same saved state.
	used := make(map[int64]bool, len(s.presents)+len(s.presentHistories))
	for _, group := range [][]gamestate.Present{s.presents, s.presentHistories} {
		for _, present := range group {
			used[present.PresentID] = true
		}
	}
	presentID := time.Now().UnixNano()
	for used[presentID] {
		presentID++
	}
	stored := cloneReward(reward)
	if stored.CardSkillLevels == nil {
		stored.CardSkillLevels = []int16{}
	}
	empty := gamestate.Reward{CardSkillLevels: []int16{}}
	s.presents = append(s.presents, gamestate.Present{
		PresentID: presentID, IssuedAtUnix: time.Now().Unix(), Title: title, Comment: comment, Reward: stored,
		Reward0: empty, Reward1: empty, Reward2: empty,
	})
	return stored
}
