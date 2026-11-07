package masterdata

import (
	"encoding/json"
	"fmt"
)

type battleImportedEntry struct {
	BossID         int    `json:"boss_id"`
	EnemyPartyID   int    `json:"enemy_party_id"`
	GroupID        int    `json:"group_id"`
	Region         string `json:"region"`
	Name           string `json:"name"`
	Difficulty     string `json:"difficulty"`
	DifficultyKind string `json:"difficulty_kind"`
	RenderMode     string `json:"render_mode"`
}

// A source party's numbered variant is not the native BEGINNER difficulty.
// Only explicitly imported, complete parties may use a numbered Lv label.
func battleImportedEntries(master BattleRuntimeMaster) (map[int]battleImportedEntry, error) {
	var profile struct {
		Import struct {
			Entries []battleImportedEntry `json:"entries"`
		} `json:"jp_boss_import"`
	}
	if err := json.Unmarshal(master.LocalProfile, &profile); err != nil {
		return nil, fmt.Errorf("decode imported BOSS metadata: %w", err)
	}
	parties := make(map[int]int, len(master.Replays))
	for _, replay := range master.Replays {
		parties[replay.BossID] = replay.EnemyPartyID
	}
	entries := make(map[int]battleImportedEntry, len(profile.Import.Entries))
	for _, entry := range profile.Import.Entries {
		if entry.BossID <= 0 || entry.EnemyPartyID <= 0 ||
			entry.GroupID <= battleGeneratedGroupIDMin || entry.GroupID >= battleGeneratedGroupIDMax ||
			entry.Region != "JP" || entry.Name == "" || entry.Difficulty == "" ||
			parties[entry.BossID] != entry.EnemyPartyID ||
			(entry.RenderMode != "2d" && entry.RenderMode != "3d") {
			return nil, fmt.Errorf("invalid imported BOSS identity %d", entry.BossID)
		}
		if _, exists := entries[entry.BossID]; exists {
			return nil, fmt.Errorf("duplicate imported BOSS identity %d", entry.BossID)
		}
		switch entry.DifficultyKind {
		case "SOURCE_FAMILY_SEQUENCE", "LOCAL_VARIANT", "SCORE":
		case "LEVEL":
			suffix := entry.BossID % 100
			if suffix < 11 || suffix%10 != 1 || entry.Difficulty != fmt.Sprintf("Lv%d", suffix/10) {
				return nil, fmt.Errorf("invalid imported BOSS level label %d", entry.BossID)
			}
		default:
			return nil, fmt.Errorf("unsupported imported BOSS difficulty kind %d", entry.BossID)
		}
		entries[entry.BossID] = entry
	}
	return entries, nil
}
