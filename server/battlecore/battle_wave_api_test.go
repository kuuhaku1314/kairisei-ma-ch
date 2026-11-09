package battlecore

import (
	"reflect"
	"testing"
)

func TestEnemyDefeatObservationGroupsOptionalAwakeBranches(t *testing.T) {
	for _, tc := range []struct {
		types []int8
		bits  []int
		want  bool
	}{
		{nil, nil, false},
		{[]int8{1}, []int{6}, false},
		{[]int8{1}, []int{1}, true},
		{[]int8{1, 4}, []int{1, 0}, true},
		{[]int8{1, 4}, []int{0, 1}, true},
		{[]int8{1, 4, 1}, []int{0, 1, 0}, false},
		{[]int8{1, 4, 1}, []int{0, 1, 1}, true},
		{[]int8{1, 4, 1, 4}, []int{0, 1, 0, 1}, true},
		{[]int8{1, 4}, []int{1}, false},
	} {
		if DefeatedEnemyParties(tc.types, tc.bits) != tc.want {
			t.Fatalf("types=%v bits=%v", tc.types, tc.bits)
		}
	}
}

func TestNextWaveDrawNotificationsAndTotalTurn(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	if _, err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.TurnPhase(); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.UserPhase(); err != nil {
		t.Fatal(err)
	}
	retained := engine.players[1].Hand
	for member := 1; member <= 4; member++ {
		submission := cardPlaySubmission{}
		if member == 1 {
			submission.CardTypes[0], submission.Targets[0] = engine.players[0].Hand[0], 5
		}
		if _, err := engine.Submit(member, submission); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := engine.UserAttack(); err != nil {
		t.Fatal(err)
	}
	if engine.endType != 1 {
		t.Fatal("fixture did not finish its original wave")
	}
	next, err := engine.NextBattle(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := next.Start(); err != nil {
		t.Fatal(err)
	}
	turn, err := next.TurnPhase()
	if err != nil {
		t.Fatal(err)
	}
	logs := 0
	for _, row := range turn {
		if row.Command == CommandPlayLogState {
			logs++
			if row.Args[1] != 2 {
				t.Fatalf("cross-wave total turn was reset: %+v", row)
			}
		}
	}
	rows, err := next.UserPhase()
	if err != nil {
		t.Fatal(err)
	}
	dealt, decks := 0, 0
	for _, row := range rows {
		switch row.Command {
		case CommandCardDeal:
			dealt++
			if row.Args[0] != 1 {
				t.Fatal("retained hand was replayed as a new draw")
			}
		case CommandCardDeck:
			decks++
			if row.Args[1] != 5 {
				t.Fatalf("wrong remaining deck: %+v", row)
			}
		}
	}
	if dealt != 1 || decks != 4 || logs != 4 || next.turn != 1 || next.players[1].Hand != retained {
		t.Fatalf("incomplete wave handover: draws=%d decks=%d logs=%d turn=%d", dealt, decks, logs, next.turn)
	}
}

func TestBaseParameterProjectionSeparatesPassiveAndBurst(t *testing.T) {
	player := battlePlayer{MemberType: 1, HP: 800, BaseMaxHP: 1000, BaseAttack: 100, BaseDefense: 10,
		Effects: []battleEffect{
			{Function: "ATK_UP_FIXED", ListType: 1, Parameter: "ATK", Delta: 30},
			{Function: "DEF_UP_FIXED", ListType: 6, Parameter: "DEF", Delta: 300},
			{Function: "ATK_UP_FIXED", ListType: 6, Parameter: "MAX_HP", Delta: 500},
			{Function: "ATK_UP_FIXED", ListType: 0, Parameter: "INT", Delta: 90},
		}}
	refreshPlayerBattleParameters(&player)
	before := player
	row := playerBaseParameterResult(&player)
	if !equalBattleArgs(row.Args, baseParameterArgs(1, 1000, 130, 0, 0, 10, 0)) || !reflect.DeepEqual(before, player) {
		t.Fatalf("base tuple included temporary/Burst effects or mutated state: %+v", row)
	}
	rows := appendDefaultWavePlayerCleanup(nil, &player, false)
	if !equalBattleArgs(rows[len(rows)-2].Args, baseParameterArgs(1, 1000, 100, 0, 0, 10, 0)) || player.MaxHP != 1500 || player.Defense != 310 {
		t.Fatal("terminal base and retained battle parameters were conflated")
	}
}

func TestResumeCommitsOnlyPublishedDisplayCaches(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.catalog.Spheres = map[int]CombatSphereDefinition{1: {ID: 1, Type: SphereTypeNormal, Count: 1, SkillID: 1}}
	engine.players[0].Spheres[0] = battleSphere{Slot: 1, SphereID: 1, Level: 1, Type: SphereTypeNormal, Count: 1, Maximum: 1,
		Display: battleDisplayPower{Known: true, Power: 999}}
	engine.phase = PhaseStarted
	before := *engine
	rows, err := engine.ResumeResults()
	if err != nil {
		t.Fatal(err)
	}
	var power int64 = -1
	for _, row := range rows {
		if row.Command == CommandSphereUpdate {
			power = row.Args[7]
		}
	}
	if power < 0 || int64(engine.players[0].Spheres[0].Display.Power) != power || power == 999 {
		t.Fatal("published Sphere power was not committed to the native display cache")
	}
	expected := before
	expected.players[0].Spheres[0].Display = engine.players[0].Spheres[0].Display
	if !reflect.DeepEqual(expected, *engine) {
		t.Fatal("resume changed state beyond the published display cache")
	}
	repeated, err := engine.ResumeResults()
	if err != nil || !reflect.DeepEqual(rows, repeated) {
		t.Fatal("repeated snapshot changed", err)
	}
	// A failed snapshot must not partially publish another member's cache.
	engine.players[0].Spheres[0].Display.Power = 999
	engine.players[1].Spheres[0] = battleSphere{Slot: 1, SphereID: 2, Level: 1}
	before = *engine
	if _, err := engine.ResumeResults(); err == nil || !reflect.DeepEqual(before, *engine) {
		t.Fatal("failed snapshot changed display/gameplay state")
	}
}

func baseParameterArgs(memberType int, hp int, attack int, magic int, recovery int, defense int, magicDefense int) []int64 {
	return baseParameterArgsWithLimit(memberType, hp, attack, magic, recovery, defense, magicDefense, 99999)
}
