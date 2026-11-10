package masterdata

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"kairisei.local/server/internal/gamestate"
)

const maxLoginBonusRuntimeBytes = 64 * 1024

var LoginBonusDayPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

type loginBonusRuntimeMaster struct {
	gamestate.LoginBonusPolicy
}

func LoadLoginBonusRuntimeMaster(path string) (gamestate.LoginBonusPolicy, error) {
	if path == "" {
		return gamestate.LoginBonusPolicy{}, errors.New("CN login bonus runtime path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return gamestate.LoginBonusPolicy{}, fmt.Errorf("resolve CN login bonus runtime: %w", err)
	}
	file, err := os.Open(absolute)
	if err != nil {
		return gamestate.LoginBonusPolicy{}, fmt.Errorf("open CN login bonus runtime: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return gamestate.LoginBonusPolicy{}, fmt.Errorf("stat CN login bonus runtime: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxLoginBonusRuntimeBytes {
		return gamestate.LoginBonusPolicy{}, errors.New("CN login bonus runtime must be a non-empty regular JSON file within the size limit")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxLoginBonusRuntimeBytes+1))
	var master loginBonusRuntimeMaster
	if err := decoder.Decode(&master); err != nil {
		return gamestate.LoginBonusPolicy{}, fmt.Errorf("decode CN login bonus runtime: %w", err)
	}
	if err := RequireJSONEOF(decoder); err != nil {
		return gamestate.LoginBonusPolicy{}, err
	}
	if err := validateLoginBonusRuntimeMaster(master); err != nil {
		return gamestate.LoginBonusPolicy{}, err
	}
	return cloneLoginBonusPolicy(master.LoginBonusPolicy), nil
}

func validateLoginBonusRuntimeMaster(master loginBonusRuntimeMaster) error {
	policy := master.LoginBonusPolicy
	if policy.DayBoundaryOffsetMinutes < -12*60 ||
		policy.DayBoundaryOffsetMinutes > 14*60 || len(policy.Cycle) == 0 || len(policy.Cycle) > 31 ||
		len(policy.Beginner) == 0 || len(policy.Beginner) > 30 ||
		len(policy.TotalMilestones) == 0 || len(policy.TotalMilestones) > 100 {
		return errors.New("CN login bonus runtime identity or policy is invalid")
	}
	if err := validateLoginBonusSchedule("daily", policy.Cycle, true); err != nil {
		return err
	}
	if err := validateLoginBonusSchedule("beginner", policy.Beginner, true); err != nil {
		return err
	}
	if err := validateLoginBonusSchedule("total", policy.TotalMilestones, false); err != nil {
		return err
	}
	return nil
}

func validateLoginBonusSchedule(name string, schedule []gamestate.LoginBonusDay, consecutive bool) error {
	previousDay := 0
	for index, day := range schedule {
		if day.Day <= previousDay || (consecutive && day.Day != index+1) || day.Comment == "" ||
			day.Reward.Num <= 0 || (day.Reward.Type != 4 && day.Reward.Type != 10) ||
			day.Reward.RewardTypeID != 0 || day.Reward.CardLevel != 0 || day.Reward.CardFame != 0 ||
			day.Reward.CardLove != 0 || day.Reward.CardSkillLevels == nil {
			return fmt.Errorf("CN %s login bonus entry %d is invalid", name, index+1)
		}
		previousDay = day.Day
	}
	return nil
}

func ApplyLoginBonusRuntimeMaster(
	state *gamestate.State,
	policy gamestate.LoginBonusPolicy,
) error {
	state.LoginBonusPolicy = cloneLoginBonusPolicy(policy)
	return validateLoginBonusState(state.LoginBonus, policy)
}

func validateLoginBonusState(state gamestate.LoginBonusState, policy gamestate.LoginBonusPolicy) error {
	if state.CycleDay < 0 ||
		state.CycleDay > len(policy.Cycle) || state.BeginnerDay < 0 ||
		state.BeginnerDay > len(policy.Beginner) || state.TotalClaims < state.CycleDay ||
		state.TotalClaims < state.BeginnerDay ||
		(state.LastClaimDay == "" && (state.CycleDay != 0 || state.BeginnerDay != 0 || state.TotalClaims != 0)) ||
		(state.LastClaimDay != "" && !LoginBonusDayPattern.MatchString(state.LastClaimDay)) {
		return errors.New("CN login bonus account state is invalid")
	}
	return nil
}

func cloneLoginBonusPolicy(policy gamestate.LoginBonusPolicy) gamestate.LoginBonusPolicy {
	policy.Cycle = cloneLoginBonusSchedule(policy.Cycle)
	policy.Beginner = cloneLoginBonusSchedule(policy.Beginner)
	policy.TotalMilestones = cloneLoginBonusSchedule(policy.TotalMilestones)
	return policy
}

func cloneLoginBonusSchedule(schedule []gamestate.LoginBonusDay) []gamestate.LoginBonusDay {
	cloned := append([]gamestate.LoginBonusDay(nil), schedule...)
	for index := range cloned {
		cloned[index].Reward.CardSkillLevels = append(
			[]int16{}, cloned[index].Reward.CardSkillLevels...,
		)
	}
	return cloned
}
