package masterdata

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"kairisei.local/server/internal/multiplayer"
)

func TestNamelessPublishedSequences(t *testing.T) {
	root := os.Getenv("CN602_RUNTIME_SET")
	if root == "" {
		t.Skip("set CN602_RUNTIME_SET to the complete resource directory")
	}
	content, err := os.ReadFile(filepath.Join(root, "resource-set.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Entrypoints map[string]string `json:"entrypoints"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	master, err := LoadBattleRuntimeMaster(filepath.Join(root, manifest.Entrypoints["cn-battle-master"]))
	if err != nil {
		t.Fatal(err)
	}
	view, err := ExpandBattleEntries(master)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := multiplayer.LoadCombatCatalog(filepath.Join(root, "_local/control/server/cn602-card-master/card.csv"), filepath.Join(root, "_local/control/server/cn602-battle-master"))
	if err != nil {
		t.Fatal(err)
	}
	for _, replay := range view.Replays {
		for index, wave := range replay.Battles {
			awakeningProducer := false
			party, ok := catalog.EnemyParties[wave.EnemyPartyID]
			if !ok {
				t.Fatalf("missing party %d", wave.EnemyPartyID)
			}
			for _, slot := range party.Slots {
				if slot.EnemyID == 0 {
					continue
				}
				level := catalog.EnemyLevels[slot.EnemyID]
				skills := []int{level.PassiveSkillID}
				skills = append(skills, level.CallSkillIDs[:]...)
				for _, action := range level.Actions {
					// Native rows can retain an unused awakening skill (e.g.
					// lower Christmas Constantine difficulties). No enabled
					// initial or looping turn means the action never executes.
					if order, ok := catalog.EnemyAIOrders[action.AIConditionID]; ok {
						enabled := false
						for column := 7; column <= 28; column++ {
							if column == 18 {
								continue // loop start, not an enabled-turn flag
							}
							flag, _ := strconv.Atoi(order.Fields[column])
							enabled = enabled || flag > 0
						}
						if !enabled {
							continue
						}
					}
					skills = append(skills, action.SkillID)
				}
				for _, skillID := range skills {
					for _, skill := range catalog.Skills[skillID] {
						for _, role := range catalog.SkillRoles[skill.FunctionID] {
							if value, _ := strconv.Atoi(role.Parameters[0]); role.Function == "ENEMY_AWAKE_FLAG_SET" && value != 0 {
								awakeningProducer = true
							}
						}
					}
				}
			}
			// A declared awake phase needs a real source producer. The
			// converse is false: the original client can finish at the end
			// of its declared list with end type AWAKE as well as WIN.
			if index+1 < len(replay.Battles) && replay.Battles[index+1].EnemyType == 4 && !awakeningProducer {
				t.Fatalf("boss %d party %d declares awakening without a source producer", replay.BossID, wave.EnemyPartyID)
			}
		}
	}
	found := 0
	for _, replay := range view.Replays {
		id := replay.BossID
		if id >= ownDeckBossIDOffset {
			id -= ownDeckBossIDOffset
		}
		if (id < 30920102 || id > 30920108) && id != 30910106 && id != 30910107 {
			continue
		}
		want := 1
		if id >= 30920106 || id == 30910106 || id == 30910107 {
			want = 2
		}
		if len(replay.Battles) != want || replay.Battles[0].EnemyPartyID != id {
			t.Fatalf("bad published phases: %+v", replay)
		}
		if want == 2 && (replay.Battles[1].EnemyPartyID != id+10 || replay.Battles[1].EnemyType != 4) {
			t.Fatalf("missing awake phase: %+v", replay)
		}
		found++
		t.Logf("boss=%d phases=%+v", replay.BossID, replay.Battles)
	}
	if found == 0 {
		t.Fatal("no Nameless or Constantine phases were checked")
	}
	awakeIDs := make(map[int]bool)
	for _, replay := range view.Replays {
		if len(replay.Battles) > 1 {
			for _, wave := range replay.Battles[1:] {
				if wave.EnemyType == 4 {
					awakeIDs[replay.BossID] = true
				}
			}
		}
	}
	for _, listing := range []struct {
		groups []json.RawMessage
		key    string
	}{{view.Groups, "10"}, {view.PastBossGroups, "13"}} {
		checked := 0
		for _, raw := range listing.groups {
			var group map[string]json.RawMessage
			if err := json.Unmarshal(raw, &group); err != nil {
				t.Fatal(err)
			}
			var bosses []map[string]json.RawMessage
			if err := json.Unmarshal(group[listing.key], &bosses); err != nil {
				t.Fatal(err)
			}
			for _, boss := range bosses {
				var id int
				if err := json.Unmarshal(boss["0"], &id); err != nil {
					t.Fatal(err)
				}
				if !awakeIDs[id] {
					continue
				}
				var awake map[string]int
				if err := json.Unmarshal(boss["18"], &awake); err != nil {
					t.Fatal(err)
				}
				if awake["1"] != 1 || awake["4"] != 1 {
					t.Fatalf("listing %s boss %d lacks native solo cost/draw inheritance", listing.key, id)
				}
				checked++
			}
		}
		if checked == 0 && listing.key == "10" {
			t.Fatalf("listing %s has no tested awakening bosses", listing.key)
		}
	}
}
