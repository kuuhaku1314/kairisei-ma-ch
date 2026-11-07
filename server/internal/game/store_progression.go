package game

import (
	"kairisei.local/server/internal/gamestate"
	"time"
)

func (s *Account) applyPlayerExperienceLocked(amount int) {
	policy := s.playerProgression
	if amount <= 0 || policy.ConfigVersion <= 0 || s.currentLevel >= policy.MaxLevel {
		return
	}
	remaining := amount
	leveledUp := false
	for remaining > 0 && s.currentLevel < policy.MaxLevel {
		required := policy.ExperienceRequired(s.currentLevel)
		needed := required - s.currentLevelExperience
		if remaining < needed {
			s.currentLevelExperience += remaining
			remaining = 0
			break
		}
		remaining -= needed
		s.currentLevel++
		s.currentLevelExperience = 0
		leveledUp = true
	}
	if s.currentLevel >= policy.MaxLevel {
		s.currentLevel = policy.MaxLevel
		s.currentExperience = policy.CumulativeExperience(policy.MaxLevel)
		s.currentLevelExperience = 0
		s.nextLevelExperience = 0
	} else {
		s.currentExperience = policy.CumulativeExperience(s.currentLevel) + s.currentLevelExperience
		s.nextLevelExperience = policy.ExperienceRequired(s.currentLevel) - s.currentLevelExperience
	}
	if !leveledUp {
		return
	}
	s.markMissionMetricLocked(gamestate.MissionPlayerLevel)
	s.bpMax = policy.BattlePointMaximum(s.currentLevel)
	s.currentFollowMax = policy.FollowMaximum(s.currentLevel)
	s.currentJobs = policy.JobsAtLevel(s.currentLevel)
	// Level-up always refills both point pools and resets their recovery clocks.
	s.ap = s.apMax
	s.apNextRecovery = time.Time{}
	s.bp = max(s.bp, s.bpMax)
	s.bpNextRecovery = time.Time{}
}
