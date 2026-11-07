package game

import (
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestMissionEventsKeepDailyProgressClaimsAndPrerequisitesSeparate(t *testing.T) {
	now := time.Now()
	end := missionDayEnd(now.Unix())
	if missionDayEnd(int64(end)) != end || missionDayEnd(int64(end)+1) != end+86400 {
		t.Fatal("daily deadline is not inclusive UTC+8 midnight")
	}
	definitions := map[int]gamestate.MissionDefinition{
		1: {MissionID: 1, Published: true, Daily: true, Title: "每日强化", Rule: &gamestate.MissionRule{Event: gamestate.MissionCardFusion, Target: 1}},
		2: {MissionID: 2, Published: true, Daily: true, PrerequisiteID: 1, Title: "后续强化", Rule: &gamestate.MissionRule{Event: gamestate.MissionCardFusion, Target: 2}},
		3: {MissionID: 3, Title: "未开放", Rule: &gamestate.MissionRule{Event: gamestate.MissionCardFusion, Target: 1}},
	}
	s := &Account{missionCatalog: missionCatalogFixture(t, definitions), missions: map[int]gamestate.MissionProgress{
		1: {MissionID: 1, State: 2, ProgressNow: 9, ClearLimitTime: end - 86400},
	}}
	if err := s.validateMissionsLocked(); err != nil {
		t.Fatal(err)
	}
	view := s.MissionInfos(false)
	if len(view) != 1 || view[0].State != 0 || view[0].ProgressNow != 0 || view[0].ClearLimitTime != end || s.missions[1].State != 2 || s.MissionNotice().Claimable {
		t.Fatal("daily reads rewrote history, leaked an old claim or exposed its successor")
	}
	s.recordMissionEventLocked(missionEvent{kind: gamestate.MissionCardFusion}, now)
	s.recordMissionEventLocked(missionEvent{kind: gamestate.MissionCardFusion}, now)
	updates := s.TakeMissionUpdates()
	if len(updates) != 1 || updates[0].MissionID != 1 || updates[0].ProgressNow != 2 || updates[0].State != 1 || len(s.missions) != 1 || !s.MissionNotice().Claimable {
		t.Fatal("events were clamped, shared with a locked successor, or applied to unpublished data")
	}
	if len(s.TakeMissionUpdates()) != 0 {
		t.Fatal("unchanged mission snapshot was returned as another delta")
	}
	if _, _, err := s.ReceiveMissionRewards([]int{1}); err != nil {
		t.Fatal(err)
	}
	s.recordMissionEventLocked(missionEvent{kind: gamestate.MissionCardFusion}, now)
	s.recordMissionEventLocked(missionEvent{kind: gamestate.MissionCardFusion}, now)
	if s.missions[1].ProgressNow != 2 || s.missions[2].ProgressNow != 2 || s.missions[2].State != 1 {
		t.Fatal("claimed mission kept counting or successor inherited its old count")
	}
	tomorrow := now.Add(24 * time.Hour)
	s.recordMissionEventLocked(missionEvent{kind: gamestate.MissionCardFusion}, tomorrow)
	if s.missions[1].ProgressNow != 1 || s.missions[1].ClearLimitTime != end+86400 || s.missionProgressLocked(definitions[2], tomorrow.Unix()).State != 0 {
		t.Fatal("daily completion or prerequisite crossed the period boundary")
	}
	other := &Account{missionCatalog: missionCatalogFixture(t, definitions), missions: map[int]gamestate.MissionProgress{}}
	if other.MissionNotice().Claimable || other.MissionInfos(false)[0].ProgressNow != 0 {
		t.Fatal("one account mutated shared mission definitions")
	}
}
