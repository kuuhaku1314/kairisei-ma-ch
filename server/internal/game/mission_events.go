package game

import (
	"math"
	"slices"
	"sort"
	"time"

	"kairisei.local/server/internal/gamestate"
)

// All configured missions use the server business day, UTC+8, inclusive end.
const missionDayOffset = 8 * 60 * 60

func missionDayEnd(now int64) int {
	return int(((now+missionDayOffset)/86400+1)*86400 - missionDayOffset - 1)
}

// Reads project the current period without rewriting every account at login.
// The next completed business event or claim persists its real progress.
func (s *Account) missionProgressLocked(definition gamestate.MissionDefinition, now int64) gamestate.MissionProgress {
	return missionProgressAt(definition, s.missions[definition.MissionID], now)
}

func missionProgressAt(definition gamestate.MissionDefinition, progress gamestate.MissionProgress, now int64) gamestate.MissionProgress {
	end := int(definition.EndsAt)
	if end > 0 {
		end--
	}
	if definition.Daily {
		dayEnd := missionDayEnd(now)
		if end == 0 || dayEnd < end {
			end = dayEnd
		}
	}
	if definition.Daily && progress.ClearLimitTime != end {
		return gamestate.MissionProgress{MissionID: definition.MissionID, ClearLimitTime: end}
	}
	progress.MissionID = definition.MissionID
	if end > 0 {
		progress.ClearLimitTime = end
	}
	return progress
}

type missionEvent struct {
	kind       gamestate.MissionEventKind
	stageType  gamestate.BattleStageType
	entryRole  gamestate.BattleEntryRole
	bossID     int
	arthurType int8
	ownDecks   bool
	players    int
	scoreGrade int
	skipped    bool
	subjectID  int
	amount     int
}

func (event missionEvent) matches(rule gamestate.MissionRule) bool {
	if rule.Event != event.kind || rule.SubjectID != 0 && rule.SubjectID != event.subjectID {
		return false
	}
	if len(rule.BossIDs) != 0 && !slices.Contains(rule.BossIDs, event.bossID) || rule.ArthurType != 0 && rule.ArthurType != event.arthurType || rule.OwnDecksOnly && !event.ownDecks || event.players < rule.MinPlayers || event.scoreGrade < rule.ScoreGrade || event.skipped && !rule.IncludeSkip {
		return false
	}
	switch rule.QuestScope {
	case gamestate.MissionQuestSolo:
		return event.entryRole == gamestate.BattleEntrySolo
	case gamestate.MissionQuestMultiplayer:
		return event.entryRole.IsMultiplayer()
	case gamestate.MissionQuestHost:
		return event.entryRole == gamestate.BattleEntryMultiplayerHost
	case gamestate.MissionQuestAlchemist:
		return event.stageType == gamestate.BattleStageAlchemist || event.stageType == gamestate.BattleStageAlchemistSP
	default:
		return true
	}
}

func (s *Account) recordMissionEventLocked(event missionEvent, now time.Time) {
	s.recordMissionEventAtLocked(event, s.missionTimeLocked(now))
}

func (s *Account) recordMissionEventAtLocked(event missionEvent, occurredAt time.Time) {
	if s.missionCatalog == nil {
		return
	}
	now := occurredAt.Unix()
	for _, id := range s.missionCatalog.byEvent[event.kind] {
		definition := s.missionCatalog.definitions[id]
		if definition.Rule == nil || !event.matches(*definition.Rule) {
			continue
		}
		saved := s.missions[id]
		progress := missionProgressAt(definition, saved, now)
		if progress.State == 2 || !s.missionAvailableLocked(definition, progress, now) {
			continue
		}
		// A delayed completion cannot replace progress already saved for a later day.
		if definition.Daily && saved.ClearLimitTime > progress.ClearLimitTime {
			continue
		}
		progress.MissionID = id
		if definition.Rule.Kind == gamestate.MissionTargetSet {
			if !slices.Contains(definition.Rule.Targets, event.subjectID) || slices.Contains(progress.MatchedTargets, event.subjectID) {
				continue
			}
			progress.MatchedTargets = append(slices.Clone(progress.MatchedTargets), event.subjectID)
			slices.Sort(progress.MatchedTargets)
			progress.ProgressNow = len(progress.MatchedTargets)
		} else {
			progress.ProgressNow = int(min(int64(math.MaxInt32), int64(progress.ProgressNow)+int64(max(1, event.amount))))
		}
		if progress.ProgressNow >= definition.Rule.Target {
			progress.State = 1
		}
		s.missions[id] = progress
		if s.missionUpdates == nil {
			s.missionUpdates = make(map[int]struct{})
		}
		s.missionUpdates[id] = struct{}{}
	}
}

// This is a transient client notification, not a second progress store or an
// award receipt. The account transaction lock also serializes HTTP delivery.
func (s *Account) TakeMissionUpdates() []gamestate.MissionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]gamestate.MissionInfo, 0, len(s.missionUpdates))
	now := s.missionTimeLocked(time.Now()).Unix()
	for id := range s.missionUpdates {
		definition := s.missionCatalog.definitions[id]
		saved := s.missions[id]
		progress := missionProgressAt(definition, saved, now)
		if (!definition.Daily || saved.ClearLimitTime == progress.ClearLimitTime) && s.missionAvailableLocked(definition, progress, now) {
			result = append(result, projectMission(definition, progress))
		}
	}
	s.missionUpdates = nil
	sort.Slice(result, func(i, j int) bool { return result[i].MissionID < result[j].MissionID })
	return result
}
