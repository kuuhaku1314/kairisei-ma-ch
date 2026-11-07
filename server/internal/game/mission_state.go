package game

import (
	"encoding/json"
	"slices"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func (s *Account) SetMissionRequestTime(now time.Time) {
	s.mu.Lock()
	s.missionRequestTime = now
	s.mu.Unlock()
}
func (s *Account) missionTimeLocked(fallback time.Time) time.Time {
	if !s.missionRequestTime.IsZero() {
		return s.missionRequestTime
	}
	return fallback
}

func (s *Account) SetMissionPVPRanks(ranks []PVPRankConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pvpMissionRanks = slices.Clone(ranks)
	s.markMissionMetricLocked(gamestate.MissionPVPRank)
}

func (s *Account) markAllMissionMetricsLocked() {
	if s.missionCatalog == nil {
		return
	}
	for metric := range s.missionCatalog.byMetric {
		s.markMissionMetricLocked(metric)
	}
}

func (s *Account) markMissionMetricLocked(metric gamestate.MissionMetric) {
	if s.missionCatalog == nil || len(s.missionCatalog.byMetric[metric]) == 0 {
		return
	}
	if s.missionDirty == nil {
		s.missionDirty = map[gamestate.MissionMetric]bool{}
	}
	s.missionDirty[metric] = true
}

// Catalog changes are reconciled only for the active account. Mutations mark
// their affected metrics, so unrelated requests do not scan inventories/tasks.
func (s *Account) RefreshStateMissions(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refreshStateMissionsLocked(s.missionTimeLocked(now))
}

func (s *Account) refreshStateMissionsLocked(now time.Time) bool {
	if s.missionCatalog == nil {
		return false
	}
	if s.missionNextStart > 0 && now.Unix() >= s.missionNextStart {
		s.markAllMissionMetricsLocked()
	}
	day := (now.Unix() + missionDayOffset) / 86400
	if len(s.missionDirty) == 0 && s.missionDay == day {
		return false
	}
	s.missionNextStart = 0
	changed := false
	values := map[struct {
		metric gamestate.MissionMetric
		id     int
	}]int{}
	read := func(metric gamestate.MissionMetric, id int) int {
		key := struct {
			metric gamestate.MissionMetric
			id     int
		}{metric, id}
		if value, ok := values[key]; ok {
			return value
		}
		value := s.missionMetricLocked(metric, id, now.Unix())
		values[key] = value
		return value
	}
	for metric, ids := range s.missionCatalog.byMetric {
		for _, id := range ids {
			d := s.missionCatalog.definitions[id]
			if d.Published && d.StartsAt > now.Unix() && (s.missionNextStart == 0 || d.StartsAt < s.missionNextStart) {
				s.missionNextStart = d.StartsAt
			}
			if !s.missionDirty[metric] && (!d.Daily && metric != gamestate.MissionLoginStreak || s.missionDay == day) {
				continue
			}
			progress := s.missionProgressLocked(d, now.Unix())
			if progress.State == 2 || !s.missionAvailableLocked(d, progress, now.Unix()) {
				continue
			}
			value := 0
			if d.Rule.Kind == gamestate.MissionTargetSet {
				for _, target := range d.Rule.Targets {
					if read(metric, target) >= max(1, d.Rule.Minimum) {
						value++
					}
				}
			} else {
				value = read(metric, d.Rule.SubjectID)
			}
			if progress.State == 1 {
				value = max(value, progress.ProgressNow)
			} // Completion is latched, even after a card is sold.
			state := progress.State
			if value >= d.Rule.Target {
				state = 1
			}
			if value == progress.ProgressNow && state == progress.State {
				continue
			}
			notify := metric != gamestate.MissionPVPRank || state != progress.State
			progress.ProgressNow, progress.State = value, state
			s.missions[id] = progress
			if notify {
				if s.missionUpdates == nil {
					s.missionUpdates = map[int]struct{}{}
				}
				s.missionUpdates[id] = struct{}{}
			}
			changed = true
		}
	}
	s.missionDirty = nil
	s.missionDay = day
	return changed
}

func (s *Account) missionMetricLocked(metric gamestate.MissionMetric, id int, now int64) int {
	switch metric {
	case gamestate.MissionPlayerLevel:
		return s.currentLevel
	case gamestate.MissionCollectionCount:
		return s.cardCollectionCountLocked()
	case gamestate.MissionLoginDays:
		return s.loginBonusState.TotalClaims
	case gamestate.MissionLoginStreak:
		if s.missionVisits.LastDay < (now+missionDayOffset)/86400-1 {
			return 0
		}
		return s.missionVisits.Streak
	case gamestate.MissionCardFame, gamestate.MissionCardOwned:
		value := 0
		for _, group := range [][]CardInfo{s.cards, s.containerCards} {
			for _, card := range group {
				if card.CardID == id {
					if metric == gamestate.MissionCardOwned {
						return 1
					}
					value = max(value, card.Fame)
				}
			}
		}
		if metric == gamestate.MissionCardOwned {
			for _, card := range s.stackCards {
				if card.CardID == id && card.Num > 0 {
					return 1
				}
			}
		}
		return value
	case gamestate.MissionQuestCleared:
		if s.teamBattleSolo != nil {
			for key, p := range s.teamBattleSolo.progress {
				if key.BossID == id && p.State == 2 {
					return 1
				}
			}
		}
		for _, tower := range s.towerQuestProfiles {
			for _, floor := range tower.Floors {
				var boss struct {
					ID int `json:"0"`
				}
				if json.Unmarshal(floor.Boss, &boss) != nil {
					continue
				}
				if boss.ID == id && slices.Contains(s.towerQuestProgress[tower.TowerID].ClearedFloors, floor.Floor) {
					return 1
				}
			}
		}
	case gamestate.MissionStoryCleared:
		if s.questStoryCleared[id] {
			return 1
		}
		if quest, step, learning := gamestate.FindBurstStory(id); learning && int(s.burstProgress[quest.ArthurType-1]) > step {
			return 1
		}
		for _, parts := range [][]gamestate.StoryMainPart{s.storyMainParts, s.cnStoryMainParts} {
			for _, part := range parts {
				for _, section := range part.Sections {
					for _, story := range section.Stories {
						if story.StoryMainID == id && story.StateFlag&2 != 0 {
							return 1
						}
					}
				}
			}
		}
		for _, character := range s.storySubCharacters {
			for _, section := range character.Sections {
				for _, story := range section.Stories {
					if story.StorySubID == id && story.StateFlag&2 != 0 {
						return 1
					}
				}
			}
		}
		for _, event := range s.storyEvents {
			for _, story := range event.Stories {
				if story.Sub.StorySubID == id && story.Sub.StateFlag&2 != 0 {
					return 1
				}
			}
		}
	case gamestate.MissionPVPRank:
		for _, rank := range s.pvpMissionRanks {
			if s.pvpPoint >= rank.PointMin && s.pvpPoint <= rank.PointMax {
				return rank.RankID
			}
		}
	}
	return 0
}

// Called on successful role Home, independently of login-bonus reward claims.
func (s *Account) ObserveMissionVisit(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now = s.missionTimeLocked(now)
	day := (now.Unix() + missionDayOffset) / 86400
	if s.currentName == "" || s.missionVisits.LastDay >= day {
		return
	}
	if s.missionVisits.LastDay == day-1 {
		s.missionVisits.Streak++
	} else {
		s.missionVisits.Streak = 1
	}
	s.missionVisits.LastDay = day
	s.markMissionMetricLocked(gamestate.MissionLoginDays)
	s.markMissionMetricLocked(gamestate.MissionLoginStreak)
}
