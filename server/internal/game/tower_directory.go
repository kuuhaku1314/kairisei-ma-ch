package game

import (
	"encoding/json"
	"slices"
	"sort"
	"time"

	"kairisei.local/server/internal/gamestate"
)

// TowerQuestDirectoryEntry is a read-only root entry, not a floor selection or
// a result acknowledgement. Floor detail and entry still use the tower service.
type TowerQuestDirectoryEntry struct {
	TowerID     int
	Name        string
	State       int
	PictureBoss json.RawMessage
	Stories     []gamestate.TowerQuestStoryProfile
}

func (s *Account) TowerQuestDirectory() []TowerQuestDirectoryEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries := make([]TowerQuestDirectoryEntry, 0, len(s.towerQuestProfiles))
	now := time.Now().Unix()
	for id, profile := range s.towerQuestProfiles {
		progress, exists := s.towerQuestProgress[id]
		if !exists || !s.towerAdmission.Allows(id, now) {
			continue
		}
		entry := TowerQuestDirectoryEntry{TowerID: id, Name: profile.Name}
		if progress.Floor == 0 {
			entry.State = 2
		} else if progress.LastBattleFloor != 0 || len(progress.ClearedFloors) != 0 {
			entry.State = 1
		}
		for _, floor := range profile.Floors {
			// The last battle is the tower's final boss. A leading story floor
			// is not a portrait source for the tower directory.
			if floor.Story.TalkID == 0 && len(floor.Boss) != 0 {
				entry.PictureBoss = floor.Boss
			}
			if floor.Story.TalkID != 0 && slices.Contains(progress.ClearedFloors, floor.Floor) {
				entry.Stories = append(entry.Stories, floor.Story)
			}
		}
		entry.PictureBoss = append(json.RawMessage(nil), entry.PictureBoss...)
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].TowerID < entries[j].TowerID })
	return entries
}
