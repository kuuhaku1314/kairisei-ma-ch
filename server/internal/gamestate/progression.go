package gamestate

// ExperienceRequired returns the EXP needed to advance from level to level+1.
// A maximum-level account has no next threshold.
func (policy PlayerProgressionPolicy) ExperienceRequired(level int) int {
	if level < 1 || level >= policy.MaxLevel {
		return 0
	}
	higher := policy.Experience.HigherLevels
	if higher.FromLevel > 0 && level >= higher.FromLevel {
		return higher.Base + (level-higher.FromLevel)*higher.PerLevel
	}
	return policy.Experience.Base + (level-1)*policy.Experience.PerLevel
}

// CumulativeExperience returns the total EXP at the start of level.
func (policy PlayerProgressionPolicy) CumulativeExperience(level int) int {
	if level <= 1 {
		return 0
	}
	if level > policy.MaxLevel {
		level = policy.MaxLevel
	}
	levels := level - 1
	higher := policy.Experience.HigherLevels
	higherLevels := 0
	if higher.FromLevel > 0 && level > higher.FromLevel {
		higherLevels = level - higher.FromLevel
		levels -= higherLevels
	}
	return levels*policy.Experience.Base +
		levels*(levels-1)/2*policy.Experience.PerLevel +
		higherLevels*higher.Base + higherLevels*(higherLevels-1)/2*higher.PerLevel
}

func (policy PlayerProgressionPolicy) BattlePointMaximum(level int) int {
	value := policy.BattlePoints.Base
	if level > 0 {
		value += level / policy.BattlePoints.LevelsPerPoint
	}
	if value > policy.BattlePoints.Maximum {
		return policy.BattlePoints.Maximum
	}
	return value
}

func (policy PlayerProgressionPolicy) FollowMaximum(level int) int {
	return min(policy.Friends.Maximum, policy.Friends.Base+max(0, level-policy.Friends.IncreaseFromLevel+1))
}

func (policy PlayerProgressionPolicy) JobsAtLevel(level int) []JobParameter {
	result := make([]JobParameter, len(policy.JobParameters.Maximum))
	if level < 1 {
		level = 1
	}
	if level > policy.JobParameters.MaxStatusLevel {
		level = policy.JobParameters.MaxStatusLevel
	}
	for index, maximum := range policy.JobParameters.Maximum {
		result[index] = JobParameter{
			HP:     maximum.HP * level / policy.JobParameters.MaxStatusLevel,
			Attack: maximum.Attack * level / policy.JobParameters.MaxStatusLevel,
			Magic:  maximum.Magic * level / policy.JobParameters.MaxStatusLevel,
			Mind:   maximum.Mind * level / policy.JobParameters.MaxStatusLevel,
		}
	}
	return result
}
