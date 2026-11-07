package gamestate

import (
	"errors"
	"math"
)

type BattleScoreMode int

const (
	BattleScoreSolo BattleScoreMode = iota
	BattleScoreMultiplayer
)

func (mode BattleScoreMode) Valid() bool {
	return mode == BattleScoreSolo || mode == BattleScoreMultiplayer
}

// BossScoreResult is an accepted run's score, not the player's previous best.
type BossScoreResult struct {
	BossID     int
	Score      int64
	Grade      int
	AchievedAt int64
}

// Display and claim slots follow the native nine-grade UI. Economic thresholds
// and the multiplier are explicit local policy, not recovered official prices.
type TeamBattleScorePolicy struct {
	SourceState     string                 `json:"source_state"`
	BaseScore       int64                  `json:"base_score"`
	BestTurn        int                    `json:"best_turn"`
	Grades          []TeamBattleScoreGrade `json:"grades"`
	EndTurn         int                    `json:"end_turn"`                  // 0: no forced turn limit; retain boss AI ending.
	MemberDeadEnd   bool                   `json:"member_dead_end,omitempty"` // Operator rule frozen at entry; independent of the displayed score grade.
	Rate            int                    `json:"rate,omitempty"`
	TeamCostInitial int                    `json:"team_cost_initial,omitempty"` // Initial COST for solo and multiplayer; 0 keeps the replay default.
}

type TeamBattleScoreGrade struct {
	Score   int64    `json:"score"`
	Crystal int      `json:"crystal"`
	Rewards []Reward `json:"rewards,omitempty"`
}

type TeamBattleScoreProgress struct {
	HighScore int64  `json:"high_score"`
	Claimed   uint16 `json:"claimed"`
}

// Body and part HP may exceed one trillion together. Bound accepted damage by
// the actual score multiplication, shared by settlement and durable completion.
func ValidateTeamBattleScoreDamage(p *TeamBattleScorePolicy, damage int64) error {
	rate := 1
	if p != nil && p.SourceState == "LOCAL_POLICY_DAMAGE_SCORE" {
		rate = p.Rate
	}
	if damage < 0 || rate < 1 || damage > math.MaxInt64/int64(rate) {
		return errors.New("score damage is out of range")
	}
	return nil
}

func ValidateTeamBattleScorePolicy(p *TeamBattleScorePolicy) error {
	if p == nil {
		return nil
	}
	if p.SourceState == "LOCAL_POLICY_DAMAGE_SCORE" {
		if p.TeamCostInitial != 0 && (p.TeamCostInitial < 3 || p.TeamCostInitial > 10) {
			return errors.New("起始COST须为3至10")
		}
		if p.EndTurn < 0 || p.EndTurn > 30 || p.Rate < 1 || p.Rate > 1000 || p.BaseScore != 0 || p.BestTurn != 0 || len(p.Grades) != 9 {
			return errors.New("invalid damage score policy")
		}
		var previous int64 = -1
		for _, g := range p.Grades {
			if g.Score <= previous || g.Score > 1000000000000 || g.Crystal != 0 || len(g.Rewards) > 10 {
				return errors.New("score thresholds must increase and each grade allows at most ten rewards")
			}
			previous = g.Score
			for _, r := range g.Rewards {
				if r.Num < 1 || r.Num > 10000000 || r.CardSkillLevels == nil {
					return errors.New("invalid score reward")
				}
			}
		}
		return nil
	}
	if p.SourceState != "PLACEHOLDER_LOCAL_CLEAR_TURN_SCORE" || p.BaseScore < 1 || p.BaseScore > 1000000000 || p.BestTurn < 1 || p.BestTurn > 100 || len(p.Grades) != 9 {
		return errors.New("invalid local battle score policy")
	}
	for i, g := range p.Grades {
		if g.Score != p.BaseScore*int64(i+1) || g.Crystal < 1 || g.Crystal > 10000 {
			return errors.New("invalid local score grade")
		}
	}
	return nil
}

func CloneTeamBattleScorePolicy(p *TeamBattleScorePolicy) *TeamBattleScorePolicy {
	if p == nil {
		return nil
	}
	r := *p
	r.Grades = append([]TeamBattleScoreGrade(nil), p.Grades...)
	for i := range r.Grades {
		r.Grades[i].Rewards = cloneRewards(p.Grades[i].Rewards)
	}
	return &r
}

func (g TeamBattleScoreGrade) RewardList() []Reward {
	if g.Crystal > 0 {
		return []Reward{{Type: 10, Num: g.Crystal, CardSkillLevels: []int16{}}}
	}
	return g.Rewards
}
