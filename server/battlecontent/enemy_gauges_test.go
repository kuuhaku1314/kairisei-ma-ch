package battlecontent

import (
	"reflect"
	"strings"
	"testing"

	"kairisei.local/server/battlemodel"
)

func reactionRow(id, kind string, parameters ...string) string {
	row := make([]string, 18)
	row[0], row[1], row[6], row[15], row[16] = id, "ALL", kind, "-3", "7"
	copy(row[7:15], parameters)
	return strings.Join(row, ",") + "\n"
}

func TestCanonicalGaugeGroupsPreserveOrderAndParameterSlots(t *testing.T) {
	text := reactionRow("10000001", "COST_FIXED", "0", "", "5") +
		reactionRow("10000001", "ARTHUR_TYPE", "NULL", "", "ALCHEMIST")
	groups, err := ParseEnemyGaugeReactions(text)
	if err != nil {
		t.Fatal(err)
	}
	values := groups[10000001]
	if len(values) != 2 || values[0].Kind != "COST_FIXED" || values[1].Kind != "ARTHUR_TYPE" ||
		!reflect.DeepEqual(values[0].Parameters, []string{"0", "-1"}) ||
		!reflect.DeepEqual(values[1].Parameters, []string{"NULL", "-1"}) ||
		values[0].NormalRate != -3 || values[0].TranceRate != 7 {
		t.Fatalf("original duplicate/slot semantics lost: %+v", values)
	}
	if _, err := ParseEnemyGaugeReactions(reactionRow("1", "UNKNOWN")); err == nil {
		t.Fatal("unknown reaction kind accepted")
	}
	if err := readCombatData(strings.NewReader("#ENEMY_GAUGES_V1,1\n1,1\n"), true, func([]string) error { return nil }); err == nil {
		t.Fatal("retired required extension silently ignored")
	}
}

func TestCanonicalGaugeRequiresReferencedGroupAndLevel(t *testing.T) {
	text := reactionRow("10000001", "COST_FIXED", "3")
	for _, test := range []struct {
		name                   string
		group                  int
		level, canonical, fail bool
	}{
		{"complete", 10000001, true, true, false},
		{"missing-group", 10000002, true, true, true},
		{"missing-level", 10000001, false, true, true},
		{"missing-table", 10000001, true, false, true},
		{"legacy-no-gauge", 0, true, false, false},
		{"canonical-no-reactions", 0, true, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			defs := &battlemodel.Definitions{
				Enemies:     map[int]battlemodel.CombatEnemyDefinition{1: {ID: 1, GaugeReactionID: test.group, InitialGaugeState: 2, InitialGaugeRate: 50}},
				EnemyLevels: map[int]battlemodel.CombatEnemyLevel{},
			}
			if test.level {
				defs.EnemyLevels[1] = battlemodel.CombatEnemyLevel{ID: 1}
			}
			err := applyEnemyGaugeTables(defs, text, test.canonical)
			if (err != nil) != test.fail {
				t.Fatalf("load error = %v", err)
			}
			if test.name == "complete" {
				gauge := defs.Enemies[1].ReactiveGauge
				if gauge == nil || gauge.InitialState != 2 || gauge.InitialRate != 50 || len(gauge.Reactions) != 1 {
					t.Fatal("canonical gauge definition not attached")
				}
			}
			if test.name == "legacy-no-gauge" && defs.Enemies[1].ReactiveGauge != nil {
				t.Fatal("legacy input invented a gauge")
			}
			if test.name == "canonical-no-reactions" {
				gauge := defs.Enemies[1].ReactiveGauge
				if gauge == nil || gauge.InitialState != 2 || gauge.InitialRate != 50 || len(gauge.Reactions) != 0 {
					t.Fatal("canonical gauge initialization was lost without reactions")
				}
			}
		})
	}
}

func TestCanonicalEnemyLevelStateSkillsDoNotShiftOtherActions(t *testing.T) {
	row := make([]string, 335)
	row[0], row[1] = "123", "2"
	row[30], row[31], row[32] = "9001", "SELF", "12"
	row[44], row[45] = "9003", "RANDOM"
	row[51] = "1001"
	row[61], row[64] = "2001", "SELF"
	row[256], row[259] = "3001", "RANDOM"
	row[321], row[324] = "4001", "SELF"
	modern, err := ParseEnemyLevel(row)
	if err != nil {
		t.Fatal(err)
	}
	if modern.GaugeActions[0].SkillID != 9001 || modern.GaugeActions[0].TargetParams[0] != "12" ||
		modern.GaugeActions[1].SkillID != 0 || modern.GaugeActions[2].SkillID != 9003 ||
		modern.CallSkillIDs[0] != 1001 || len(modern.Actions) != 3 ||
		modern.Actions[0].SkillID != 2001 || modern.Actions[1].SkillID != 3001 || modern.Actions[2].SkillID != 4001 {
		t.Fatalf("state/call/ordinary/special/death columns changed: %+v", modern)
	}
	legacy, err := ParseEnemyLevel(append(append([]string{}, row[:30]...), row[51:]...))
	if err != nil {
		t.Fatal(err)
	}
	modern.GaugeActions = legacy.GaugeActions
	if !reflect.DeepEqual(modern, legacy) {
		t.Fatal("unchanged enemy fields differ between explicit layouts")
	}
	short, err := parseEnemyLevel(row[:51], true)
	if err != nil || short.GaugeActions[2].SkillID != 9003 || len(short.Actions) != 0 {
		t.Fatalf("original short modern row rejected or shifted: %+v %v", short, err)
	}
}
