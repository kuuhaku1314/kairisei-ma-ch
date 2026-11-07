package game

import (
	"time"

	"kairisei.local/server/internal/gamestate"
)

func (settlement *teamBattleSettlement) setSoloRankingScore(policy *gamestate.TeamBattleScorePolicy, report TeamBattleDropReport, bossID int) {
	if !report.SoloChallenge || !report.ScoreVerified || policy == nil || policy.SourceState != "LOCAL_POLICY_DAMAGE_SCORE" {
		return
	}
	for _, partner := range settlement.Context.SelectedPartners {
		if !partner.IsSelf {
			return
		}
	}
	score, grade := VerifiedTeamBattleDamageScore(policy, report.ScoreDamage)
	if score > 0 {
		settlement.RankingScore = &gamestate.BossScoreResult{BossID: bossID, Score: score, Grade: grade, AchievedAt: time.Now().Unix()}
	}
}

// VerifiedTeamBattleDamageScore uses the same frozen score policy as the
// settlement. The caller accepts either a completed authoritative room or a
// validated solo report; the client never chooses the scoring multiplier.
func VerifiedTeamBattleDamageScore(p *gamestate.TeamBattleScorePolicy, damage int64) (int64, int) {
	score := damage * int64(p.Rate) / 100
	return score, teamBattleScoreGrade(p, score)
}

func teamBattleScoreGrade(p *gamestate.TeamBattleScorePolicy, score int64) int {
	grade := 0
	for i, g := range p.Grades {
		if score < g.Score {
			break
		}
		grade = i
	}
	return grade
}

func cloneTeamBattleScores(source map[int]gamestate.TeamBattleScoreProgress) map[int]gamestate.TeamBattleScoreProgress {
	result := make(map[int]gamestate.TeamBattleScoreProgress, len(source))
	for id, progress := range source {
		result[id] = progress
	}
	return result
}

func planTeamBattleScore(p *gamestate.TeamBattleScorePolicy, progress gamestate.TeamBattleScoreProgress, report TeamBattleDropReport) (gamestate.TeamBattleScoreProgress, []gamestate.Reward, []any) {
	if p == nil {
		return progress, nil, nil
	}
	turns := report.Turns
	// Missing legacy room turn metadata earns the lowest grade, not the maximum.
	if turns <= 0 {
		turns = p.BestTurn + 8
	}
	rate := max(100, min(900, 900-(turns-p.BestTurn)*100))
	damage := p.BaseScore
	if p.SourceState == "LOCAL_POLICY_DAMAGE_SCORE" {
		if report.ScoreVerified {
			damage, rate = report.ScoreDamage, p.Rate
		} else {
			return progress, nil, nil
		}
	}
	score := damage * int64(rate) / 100
	previousHighScore := progress.HighScore
	progress.HighScore = max(progress.HighScore, score)
	rewards := []gamestate.Reward{}
	grade := teamBattleScoreGrade(p, score)
	for i, g := range p.Grades {
		if score < g.Score {
			break
		}
		mask := uint16(1) << i
		if progress.Claimed&mask == 0 {
			rewards = append(rewards, g.RewardList()...)
			progress.Claimed |= mask
		}
	}
	return progress, rewards, []any{map[string]any{"damage": damage, "rate": rate, "grade": grade, "high_score": previousHighScore}}
}

func (s *Account) battleScoresLocked(solo bool) map[int]gamestate.TeamBattleScoreProgress {
	if solo {
		if s.teamBattleSoloScores == nil {
			s.teamBattleSoloScores = map[int]gamestate.TeamBattleScoreProgress{}
		}
		return s.teamBattleSoloScores
	}
	if s.teamBattleScores == nil {
		s.teamBattleScores = map[int]gamestate.TeamBattleScoreProgress{}
	}
	return s.teamBattleScores
}

func (s *Account) ScoreLineup(bossID int, profiles []gamestate.TeamBattleRewardProfile, mode gamestate.BattleScoreMode) (int64, []any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	progress := s.battleScoresLocked(mode == gamestate.BattleScoreSolo)[bossID]
	var policy *gamestate.TeamBattleScorePolicy
	for _, profile := range profiles {
		if profile.BossID == bossID && profile.StageQuestAreaID == 0 && profile.TowerID == 0 {
			policy = profile.ScorePolicy
			break
		}
	}
	rows := make([]any, 9)
	for i := range rows {
		score := int64(0)
		rewards := []any{}
		if policy != nil {
			g := policy.Grades[i]
			score = g.Score
			for _, reward := range g.RewardList() {
				rewards = append(rewards, map[string]any{"is_fixed": 0, "is_new": BoolInt(progress.Claimed&(uint16(1)<<i) == 0), "reward": reward})
			}
		}
		rows[i] = map[string]any{"grade": i, "score": score, "reward": rewards}
	}
	return progress.HighScore, rows
}
