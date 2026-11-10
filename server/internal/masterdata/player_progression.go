package masterdata

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"kairisei.local/server/internal/gamestate"
)

const maxPlayerProgressionRuntimeBytes = 64 * 1024

type playerProgressionRuntimeMaster struct {
	gamestate.PlayerProgressionPolicy
}

func LoadPlayerProgressionRuntimeMaster(path string) (gamestate.PlayerProgressionPolicy, error) {
	if path == "" {
		return gamestate.PlayerProgressionPolicy{}, errors.New("CN player progression runtime path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return gamestate.PlayerProgressionPolicy{}, fmt.Errorf("resolve CN player progression runtime: %w", err)
	}
	file, err := os.Open(absolute)
	if err != nil {
		return gamestate.PlayerProgressionPolicy{}, fmt.Errorf("open CN player progression runtime: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return gamestate.PlayerProgressionPolicy{}, fmt.Errorf("stat CN player progression runtime: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxPlayerProgressionRuntimeBytes {
		return gamestate.PlayerProgressionPolicy{}, errors.New("CN player progression runtime must be a non-empty regular JSON file within the size limit")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxPlayerProgressionRuntimeBytes+1))
	var master playerProgressionRuntimeMaster
	if err := decoder.Decode(&master); err != nil {
		return gamestate.PlayerProgressionPolicy{}, fmt.Errorf("decode CN player progression runtime: %w", err)
	}
	if err := RequireJSONEOF(decoder); err != nil {
		return gamestate.PlayerProgressionPolicy{}, err
	}
	if err := validatePlayerProgressionRuntimeMaster(master); err != nil {
		return gamestate.PlayerProgressionPolicy{}, err
	}
	return master.PlayerProgressionPolicy, nil
}

func validatePlayerProgressionRuntimeMaster(master playerProgressionRuntimeMaster) error {
	policy := master.PlayerProgressionPolicy
	if policy.MaxLevel < 2 || policy.MaxLevel > 1000 ||
		policy.Experience.Base <= 0 || policy.Experience.PerLevel < 0 ||
		(policy.Experience.HigherLevels.FromLevel != 0 &&
			(policy.Experience.HigherLevels.FromLevel < 1 || policy.Experience.HigherLevels.FromLevel >= policy.MaxLevel ||
				policy.Experience.HigherLevels.Base <= 0 || policy.Experience.HigherLevels.PerLevel < 0)) ||
		policy.BattlePoints.Base <= 0 ||
		policy.BattlePoints.LevelsPerPoint <= 0 || policy.BattlePoints.Maximum < policy.BattlePoints.Base ||
		policy.Friends.Base <= 0 || policy.Friends.Maximum < policy.Friends.Base ||
		policy.Friends.IncreaseFromLevel < 1 || policy.Friends.IncreaseFromLevel > policy.MaxLevel || policy.Friends.FollowerDisplayLimit <= 0 ||
		policy.Friends.HelperReward.OtherPerPartner <= 0 ||
		policy.Friends.HelperReward.FriendPerPartner <= policy.Friends.HelperReward.OtherPerPartner ||
		policy.Friends.HelperReward.MaximumPartners != 3 ||
		policy.JobParameters.MaxStatusLevel <= 0 || len(policy.JobParameters.Maximum) != 6 {
		return errors.New("CN player progression runtime identity or policy is invalid")
	}
	if policy.ExperienceRequired(policy.MaxLevel-1) <= 0 || policy.CumulativeExperience(policy.MaxLevel) <= 0 ||
		policy.BattlePointMaximum(policy.MaxLevel) != policy.BattlePoints.Maximum ||
		policy.FollowMaximum(policy.MaxLevel) != policy.Friends.Maximum {
		return errors.New("CN player progression runtime curve is invalid")
	}
	for index, value := range policy.JobParameters.Maximum {
		if value.HP < 0 || value.Attack < 0 || value.Magic < 0 || value.Mind < 0 ||
			(index > 0 && value.HP == 0) {
			return fmt.Errorf("CN player progression job slot %d is invalid", index)
		}
	}
	zero := gamestate.JobParameter{}
	if policy.JobParameters.Maximum[0] != zero {
		return errors.New("CN player progression job slot zero must remain empty")
	}
	return nil
}

func ApplyPlayerProgressionRuntimeMaster(
	state *gamestate.State,
	policy gamestate.PlayerProgressionPolicy,
) (bool, error) {
	state.PlayerProgressionPolicy = policy
	before := state.User
	if state.User.Level < 1 || state.User.Level > policy.MaxLevel {
		return false, errors.New("CN player progression has an invalid level")
	}
	oldBPMax := state.User.BPMax
	wasBPFull := oldBPMax > 0 && state.User.BP >= oldBPMax
	preservedWithinLevelExperience := state.User.NowLevelExperience
	if state.User.Level == policy.MaxLevel {
		preservedWithinLevelExperience = 0
	} else {
		required := policy.ExperienceRequired(state.User.Level)
		if preservedWithinLevelExperience >= required {
			preservedWithinLevelExperience = required - 1
		}
	}
	state.User.Experience = policy.CumulativeExperience(state.User.Level) + preservedWithinLevelExperience
	state.User.NowLevelExperience = preservedWithinLevelExperience
	state.User.NextLevelExperience = policy.ExperienceRequired(state.User.Level) - preservedWithinLevelExperience
	state.User.BPMax = policy.BattlePointMaximum(state.User.Level)
	if wasBPFull {
		state.User.BP = max(state.User.BP, state.User.BPMax)
	}
	state.User.Jobs = policy.JobsAtLevel(state.User.Level)
	if err := ValidatePlayerProgressionState(state.User, policy); err != nil {
		return false, err
	}
	return before.Experience != state.User.Experience ||
		before.NowLevelExperience != state.User.NowLevelExperience ||
		before.NextLevelExperience != state.User.NextLevelExperience ||
		before.BPMax != state.User.BPMax || before.BP != state.User.BP ||
		!slices.Equal(before.Jobs, state.User.Jobs), nil
}

func ValidatePlayerProgressionState(user gamestate.User, policy gamestate.PlayerProgressionPolicy) error {
	if user.Level < 1 || user.Level > policy.MaxLevel || user.Experience < 0 || user.NowLevelExperience < 0 ||
		user.BPMax != policy.BattlePointMaximum(user.Level) ||
		!slices.Equal(user.Jobs, policy.JobsAtLevel(user.Level)) {
		return errors.New("CN player progression state does not match the runtime policy")
	}
	cumulative := policy.CumulativeExperience(user.Level)
	if user.Level == policy.MaxLevel {
		if user.Experience != cumulative || user.NowLevelExperience != 0 || user.NextLevelExperience != 0 {
			return errors.New("CN maximum-level progression state is invalid")
		}
		return nil
	}
	required := policy.ExperienceRequired(user.Level)
	if user.NowLevelExperience >= required || user.Experience != cumulative+user.NowLevelExperience ||
		user.NextLevelExperience != required-user.NowLevelExperience {
		return errors.New("CN player EXP state is invalid")
	}
	return nil
}
