package game

import (
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func (s *Account) missionAvailableLocked(definition gamestate.MissionDefinition, progress gamestate.MissionProgress, now int64) bool {
	return definition.Published &&
		(definition.StartsAt == 0 || now >= definition.StartsAt) && (definition.EndsAt == 0 || now < definition.EndsAt) &&
		(progress.ClearLimitTime == 0 || int64(progress.ClearLimitTime) >= now) &&
		(definition.PrerequisiteID == 0 || s.missionProgressLocked(s.missionCatalog.definitions[definition.PrerequisiteID], now).State == 2)
}

func projectMission(definition gamestate.MissionDefinition, progress gamestate.MissionProgress) gamestate.MissionInfo {
	progressNow, progressMax := progress.ProgressNow, definition.ProgressMax
	if definition.Rule != nil && definition.Rule.Metric == gamestate.MissionPVPRank {
		progressNow, progressMax = 0, 1
		if progress.State != 0 {
			progressNow = 1
		}
	}
	return gamestate.MissionInfo{
		MissionID: definition.MissionID, TabType: definition.TabType, ViewPriority: definition.ViewPriority,
		Title: definition.Title, Description: definition.Description, Rewards: cloneRewards(definition.Rewards),
		State: progress.State, ProgressShow: definition.ProgressShow, ProgressNow: progressNow,
		ProgressMax: progressMax, ClearLimitTime: progress.ClearLimitTime, OpenURL: missionOpenURL(definition),
	}
}

func missionOpenURL(definition gamestate.MissionDefinition) string {
	if definition.OpenURL == "teambattle" && definition.Rule != nil && definition.Rule.Event == gamestate.MissionQuestClear {
		return "mainquest"
	}
	return definition.OpenURL
}

func (s *Account) missionInfosLocked(includeClaimed bool) []gamestate.MissionInfo {
	result := []gamestate.MissionInfo{}
	if s.missionCatalog == nil {
		return result
	}
	now := s.missionTimeLocked(time.Now()).Unix()
	for _, id := range s.missionCatalog.order {
		definition := s.missionCatalog.definitions[id]
		progress := s.missionProgressLocked(definition, now)
		if s.missionAvailableLocked(definition, progress, now) && (includeClaimed || progress.State != 2) {
			result = append(result, projectMission(definition, progress))
		}
	}
	return result
}

type MissionNotice struct {
	TimedComplete int
	TimedTotal    int
	Claimable     bool
}

func (s *Account) MissionNotice() (notice MissionNotice) {
	const (
		limitedTab = 1
		eventTab   = 3
	)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.missionCatalog == nil {
		return notice
	}
	now := s.missionTimeLocked(time.Now()).Unix()
	for _, definition := range s.missionCatalog.definitions {
		if !definition.Published {
			continue
		}
		progress := s.missionProgressLocked(definition, now)
		timed := progress.ClearLimitTime > 0 && (definition.TabType == limitedTab || definition.TabType == eventTab)
		if (!timed && (progress.State != 1 || notice.Claimable)) || !s.missionAvailableLocked(definition, progress, now) {
			continue
		}
		notice.Claimable = notice.Claimable || progress.State == 1
		if timed {
			notice.TimedTotal++
			if progress.State == 1 || progress.State == 2 {
				notice.TimedComplete++
			}
		}
	}
	return notice
}

func (s *Account) MissionInfos(includeClaimed bool) []gamestate.MissionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.missionInfosLocked(includeClaimed)
}

func (s *Account) CheckMissionOpenURL(openURL string) ([]gamestate.MissionInfo, error) {
	if openURL == "" || !isLocalMissionCommand(openURL) {
		return nil, errors.New("mission URL is outside the local client boundary")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := s.missionTimeLocked(time.Now()).Unix()
	if s.missionCatalog == nil {
		return nil, errors.New("mission catalog unavailable")
	}
	for _, definition := range s.missionCatalog.definitions {
		if missionOpenURL(definition) == openURL && s.missionAvailableLocked(definition, s.missionProgressLocked(definition, now), now) {
			// Navigation is not itself evidence of a completed mission.
			return []gamestate.MissionInfo{}, nil
		}
	}
	return nil, errors.New("mission URL is not published")
}

func (s *Account) ReceiveMissionRewards(missionIDs []int) ([]gamestate.MissionInfo, []gamestate.MissionInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(missionIDs) == 0 || len(missionIDs) > 20 || s.missionCatalog == nil {
		return nil, nil, errors.New("请选择1–20个可领取任务")
	}
	clock := s.missionTimeLocked(time.Now())
	s.refreshStateMissionsLocked(clock)
	now := clock.Unix()
	seen := make(map[int]bool, len(missionIDs))
	for _, id := range missionIDs {
		if seen[id] {
			return nil, nil, errors.New("duplicate mission ID")
		}
		seen[id] = true
		definition, exists := s.missionCatalog.definitions[id]
		progress := s.missionProgressLocked(definition, now)
		if !exists || progress.State != 1 || !s.missionAvailableLocked(definition, progress, now) {
			return nil, nil, errors.New("mission is not claimable")
		}
		for _, reward := range definition.Rewards {
			if err := s.validateRewardLocked(reward); err != nil {
				return nil, nil, err
			}
		}
	}
	claimed := make([]gamestate.MissionInfo, 0, len(missionIDs))
	for _, id := range missionIDs {
		definition := s.missionCatalog.definitions[id]
		progress := s.missionProgressLocked(definition, now)
		progress.State = 2
		s.missions[id] = progress
		claimed = append(claimed, projectMission(definition, progress))
		for _, reward := range definition.Rewards {
			s.appendRewardPresentLocked(reward, definition.Title, "")
		}
		for _, next := range s.missionCatalog.successors[id] {
			if rule := s.missionCatalog.definitions[next].Rule; rule != nil && rule.Metric != "" {
				s.markMissionMetricLocked(rule.Metric)
			}
		}
	}
	s.refreshStateMissionsLocked(clock)
	return claimed, s.missionInfosLocked(false), nil
}

func (s *Account) snapshotMissionsLocked() []gamestate.Mission {
	result := make([]gamestate.Mission, 0, len(s.missions))
	for _, progress := range s.missions {
		progress.MatchedTargets = slices.Clone(progress.MatchedTargets)
		result = append(result, gamestate.Mission{Info: progress})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Info.MissionID < result[j].Info.MissionID })
	return result
}

func (s *Account) validateMissionsLocked() error {
	for _, progress := range s.missions {
		if progress.MissionID <= 0 || progress.State < 0 || progress.State > 2 || progress.ProgressNow < 0 || progress.ClearLimitTime < 0 {
			return errors.New("mission progress is invalid")
		}
	}
	if v := s.missionVisits; v.LastDay < 0 || v.Streak < 0 {
		return errors.New("mission visit state is invalid")
	}
	if s.missionCatalog == nil {
		return nil
	}
	for _, definition := range s.missionCatalog.definitions {
		if definition.Published {
			for _, reward := range definition.Rewards {
				if err := s.validateRewardLocked(reward); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func ValidMissionCommand(command string) bool { return isLocalMissionCommand(command) }

func isLocalMissionCommand(command string) bool {
	if strings.Contains(command, "external") ||
		strings.Contains(command, "http") ||
		strings.Contains(command, "GetUrlToken") ||
		strings.Contains(command, "netease_sprite") ||
		strings.Contains(command, "eventpage") ||
		strings.Contains(command, "trade") ||
		strings.Contains(command, "review") ||
		strings.Contains(command, "monthcard") ||
		strings.Contains(command, "firstpay") ||
		strings.Contains(command, "shop_crystal") {
		return false
	}
	for _, marker := range []string{
		"explore",
		"teambattleone",
		"teambattle_join",
		"teambattle",
		"gacha",
		"arena",
		"mainstory",
		"mainquest",
		"substory",
		"eventstory",
		"exp_fusion",
		"evo_fusion",
		"card_collection",
		"createroom",
		"uplove",
		"sellcard",
		"enterroom",
		"gotoscene",
		"eventboss",
		"pastboss",
		"exchange",
		"mission",
		"towerquest",
	} {
		if strings.Contains(command, marker) {
			return true
		}
	}
	return false
}
