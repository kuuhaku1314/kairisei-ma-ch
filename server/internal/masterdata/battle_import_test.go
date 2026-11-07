package masterdata

import (
	"encoding/json"
	"os"
	"testing"
)

func TestImportedBossRuntime(t *testing.T) {
	path := os.Getenv("CN602_IMPORT_BOSS_RUNTIME")
	if path == "" {
		t.Skip("set CN602_IMPORT_BOSS_RUNTIME to audit an isolated imported master")
	}
	master, err := LoadBattleRuntimeMaster(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := battleImportedEntries(master)
	if err != nil || len(entries) == 0 {
		t.Fatalf("imported entries missing: %v", err)
	}
	for uid, entry := range entries {
		if entry.DifficultyKind != "LEVEL" {
			continue
		}
		// Removing the scoped source record must not make an arbitrary label
		// acceptable for ordinary CN entries.
		var profile map[string]json.RawMessage
		if err := json.Unmarshal(master.LocalProfile, &profile); err != nil {
			t.Fatal(err)
		}
		delete(profile, "jp_boss_import")
		master.LocalProfile, err = json.Marshal(profile)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateBattleRuntimeMaster(master); err == nil {
			t.Fatalf("level %d accepted without its source metadata", uid)
		}
		return
	}
	t.Fatal("imported fixture has no numbered level")
}
