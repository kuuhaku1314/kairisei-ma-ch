package battlecore

import (
	"reflect"
	"strings"
	"testing"
)

func TestNativeWaveKOVersusRetirement(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.phase, engine.endType = PhaseEnded, 1
	engine.players[0].HP = 0
	engine.players[1].HP, engine.players[1].GameOver = 0, true
	if rows := engine.appendNewPlayerGameOver(nil); len(rows) != 0 || engine.players[0].GameOver {
		t.Fatal("winning action incorrectly retired its KO member")
	}
	next, err := engine.NextBattle(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next.players[0].HP != 1 || next.players[0].GameOver || next.players[1].HP != 0 || !next.players[1].GameOver {
		t.Fatal("wave recovery confused KO with formal retirement")
	}
	rows, err := next.Start()
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Command != CommandHP || !reflect.DeepEqual(rows[0].Args, []int64{1, 1000, 1, 0}) {
		t.Fatalf("a50c4 recovery HP row missing: %+v", rows[0])
	}
	resume, err := next.ResumeResults()
	if err != nil {
		t.Fatal(err)
	}
	var retired []int64
	for _, row := range resume {
		if row.Command == CommandResumeGameOver {
			retired = append(retired, row.Args...)
		}
	}
	if !reflect.DeepEqual(retired, []int64{2}) {
		t.Fatalf("resume retirement rows: %v", retired)
	}
	// Snapshotting an unresolved KO must not manufacture a gameover event.
	next.players[2].HP = 0
	resume, err = next.ResumeResults()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range resume {
		if row.Command == CommandResumeGameOver && row.Args[0] == 3 {
			t.Fatal("snapshot retired a KO member")
		}
	}
}

func TestNativeTerminalCSVUsesInt64Fields(t *testing.T) {
	for _, test := range []struct {
		row BattleResult
		csv string
	}{{battleEndResult(1), "2,1"}, {battleGameOverResult(2), "82,2"}} {
		got, err := encodeBattleResult(test.row)
		if err != nil || got != test.csv {
			t.Fatalf("native CSV = %q, %v; want %q", got, err, test.csv)
		}
	}
}

func TestRetirementWireWaitsForDamagePresentation(t *testing.T) {
	for _, prefix := range [][]BattleResult{
		nil,
		{{Command: CommandHP, Args: []int64{1, 1000, 0, 1}}},
		{{Command: CommandHP, Args: []int64{1, 1000, 0, 1}}, {Command: CommandAttackPartition}},
	} {
		rows := append(append([]BattleResult(nil), prefix...), battleGameOverResult(1), battleGameOverResult(2))
		wire, err := encodeBattleResults(rows)
		if err != nil {
			t.Fatal(err)
		}
		want := "82,1\n82,2"
		if len(prefix) > 0 {
			want = "3,1,1000,0,1\n502\n" + want
		}
		if wire != want || strings.Count(wire, "502") > 1 || rows[len(rows)-2].Command != CommandGameOver {
			t.Fatalf("retirement boundary: %q; want %q", wire, want)
		}
	}
}

func TestNativeEnemyTailExpiryListsAndWireOrder(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.phase, engine.turn = PhaseEnemy, 1
	player := &engine.players[0]
	player.Defense = 100
	player.BurstBreak, player.BurstRecharge[0] = 2, 2
	player.Effects = []battleEffect{
		{Function: "DEF_UP_FIXED", ListType: 0, Parameter: "DEF", Kind: 1, Delta: 100, Remaining: 1, AppliedTurn: 1, Source: 2, SourceSkillID: 222, RoleIndex: 10},
		{Function: "BEGINNING_DRAW", ListType: 1, Remaining: 0},
		{Function: "ATK_UP_BOOST", ListType: 2, Remaining: 99},
		{Function: "STAN", ListType: 3, Remaining: 1},
		{Function: "HEAL_BOOST", ListType: 4, Remaining: 99},
		{Function: "CRITICAL_UP", ListType: 5, Kind: 1, Remaining: 1, Source: 3, SourceSkillID: 333, RoleIndex: 50},
		{Function: "DAMAGE_BOOST", ListType: 6, Remaining: 99},
		{Function: "CARD_SEAL", ListType: 7, Remaining: 1},
	}
	engine.players[2].HP, engine.players[2].GameOver = 0, true
	engine.players[2].Effects = []battleEffect{{Function: "STAN", Remaining: 1}}
	engine.players[3].HP = 0 // Native ticks this KO member, but never heals it.
	if _, err := engine.TurnPhase(); err == nil {
		t.Fatal("TurnPhase skipped the required enemy-tail phase")
	}
	rows, err := engine.ExecuteChaliceEnemyPhase()
	if err != nil {
		t.Fatal(err)
	}
	wantCommands := []int{70, 72, 71, 6, 72, 71, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6}
	var commands []int
	for _, row := range rows {
		commands = append(commands, row.Command)
		if _, err := encodeBattleResult(row); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("native expiry order = %v; want %v", commands, wantCommands)
	}
	for index, member := range []int{10, 10, 11, 11} {
		if !equalBattleArgs(rows[len(rows)-4+index].Args, battleParameterArgs(member, 0, 1, 0, 0, 0, 0, 0, 99999, 99999, 99999)) {
			t.Fatal("native field-member expiry snapshots are missing")
		}
	}
	if !reflect.DeepEqual(rows[1].Args, []int64{1, 0, 4, 32, 1}) ||
		!reflect.DeepEqual(rows[2].Args, []int64{1, 4, 222}) ||
		!reflect.DeepEqual(rows[4].Args, []int64{1, 5, 10, 0, 1}) ||
		!reflect.DeepEqual(rows[5].Args, []int64{1, 10, 333}) {
		t.Fatalf("expiry lost list/source identity: %+v", rows[:7])
	}
	if player.Defense != 0 || player.BurstBreak != 1 || player.BurstRecharge[0] != 1 || len(player.Effects) != 4 {
		t.Fatal("enemy-tail state was not committed")
	}
	for index, listType := range []int{1, 2, 4, 6} {
		wantRemaining := 99
		if listType == 1 {
			wantRemaining = 0
		}
		if effect := player.Effects[index]; effect.ListType != listType || effect.Remaining != wantRemaining {
			t.Fatalf("non-timed list was decremented: %+v", effect)
		}
	}
	if engine.players[2].Effects[0].Remaining != 1 || engine.players[3].HP != 0 {
		t.Fatal("retired member ticked or KO was healed")
	}
	if _, err := engine.ExecuteChaliceEnemyPhase(); err == nil {
		t.Fatal("duplicate enemy-tail executed twice")
	}
	if _, err := engine.TurnPhase(); err != nil {
		t.Fatal(err)
	}
	if player.BurstBreak != 1 || player.BurstRecharge[0] != 1 {
		t.Fatal("next TurnPhase repeated enemy-tail countdown")
	}
}

func TestNativeRegenerationRunsAtUserPhaseWithoutRevivingKO(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.phase, engine.turn = PhaseEnemy, 1
	engine.initialDealPending = false // This fixture begins after the first turn draw.
	for index := range engine.players {
		engine.players[index].HP = 500
		engine.players[index].Effects = []battleEffect{{Function: "REGENERATE_FIXED", Value: 100, Remaining: 2, AppliedTurn: 1}}
	}
	engine.players[0].Effects = append(engine.players[0].Effects,
		battleEffect{Function: "REGENERATE_BY_SELF_PARAM", ListType: 6, Value: 50, Remaining: 99})
	engine.players[1].HP = 0
	engine.players[2].HP, engine.players[2].GameOver = 0, true
	engine.players[3].Effects = append(engine.players[3].Effects,
		battleEffect{Function: "HEAL_REVERSE", Rate: 50, Remaining: 2})
	engine.enemies[0].HP = 50
	engine.enemies[0].Effects = []battleEffect{{Function: "REGENERATE_FIXED", Value: 80, Remaining: 2}}
	if _, err := engine.ExecuteChaliceEnemyPhase(); err != nil {
		t.Fatal(err)
	}
	turn, err := engine.TurnPhase()
	if err != nil {
		t.Fatal(err)
	}
	if battleResultsContainCommand(turn, 61) || engine.players[0].HP != 500 || engine.enemies[0].HP != 50 {
		t.Fatal("regeneration ran before UserPhase")
	}
	// Exercise native's unresolved KO input independently of the local
	// no-continue retirement applied at the preceding phase boundary.
	engine.players[1].GameOver = false
	rows, err := engine.UserPhase()
	if err != nil {
		t.Fatal(err)
	}
	want := []BattleResult{
		{Command: 53, Args: []int64{1, 1, 200}}, {Command: 61, Args: []int64{1, 0, 150, 650}},
		{Command: 53, Args: []int64{4, 4, 200}}, {Command: 61, Args: []int64{4, 0, -50, 450}},
		{Command: 53, Args: []int64{5, 5, 200}}, {Command: 61, Args: []int64{5, 0, 80, 100}},
	}
	// Original 63cc0: base hand display precedes regeneration, which in
	// turn precedes darkness/draw notifications. Keep the exact heal rows.
	if len(rows) < 8 || rows[0].Command != CommandCardUpdate || rows[1].Command != CommandCardUpdate || !reflect.DeepEqual(rows[2:8], want) {
		t.Fatalf("native regeneration must follow base display and aggregate per target: %+v", rows)
	}
	if engine.players[1].HP != 0 || engine.players[1].GameOver || engine.players[2].HP != 0 || !engine.players[2].GameOver {
		t.Fatal("regeneration changed KO/retirement state")
	}
}

func TestNativeExpiryKeepsUIWhileSameCodeAndKindRemain(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.players[0].BaseAttack, engine.players[0].Attack = 100, 130
	engine.players[0].Effects = []battleEffect{
		{Function: "ATK_UP_FIXED", Parameter: "ATK", Kind: 1, Value: 10, Delta: 10, Remaining: 1, RoleIndex: 1},
		{Function: "ATK_UP_FIXED", Parameter: "ATK", Kind: 1, Value: 20, Delta: 20, Remaining: 2, RoleIndex: 2},
	}
	rows, err := engine.tickPersistentEffects()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Command == 72 && row.Args[0] == 1 {
			t.Fatal("one expired stack removed the surviving buff icon")
		}
	}
	if rows[0].Command != 71 || engine.players[0].Attack != 120 {
		t.Fatal("entry loss or surviving parameter recomputation is missing")
	}
}

func TestNativeGutsDoesNotReviveRetiredUser(t *testing.T) {
	player := battlePlayer{MemberType: 1, MaxHP: 1000, GameOver: true,
		Effects: []battleEffect{{Function: "GUTS", Rate: 50, Uses: 1, Remaining: 2}}}
	if rows := resolvePlayerGuts(&player); len(rows) != 0 || player.HP != 0 || player.Effects[0].Uses != 1 {
		t.Fatal("GUTS resurrected a formally retired user")
	}
	player.GameOver = false
	player.Effects = []battleEffect{
		{Function: "GUTS", ListType: 5, Rate: 80, Uses: 1, Remaining: 2},
		{Function: "GUTS", ListType: 0, Rate: 30, Uses: 1, Remaining: 2},
		{Function: "GUTS", ListType: 0, Rate: 60, Uses: 1, Remaining: 2},
	}
	rows := resolvePlayerGuts(&player)
	if player.HP != 300 || len(rows) != 4 || len(player.Effects) != 1 || player.Effects[0].ListType != 5 {
		t.Fatal("GUTS must prefer native list order and remove the consumed list's GUTS entries")
	}
	player.HP = 0
	rows = resolvePlayerGuts(&player)
	if player.HP != 800 || len(rows) != 4 || rows[3].Args[1] != 5 || len(player.Effects) != 0 {
		t.Fatal("burst-list GUTS released the wrong list")
	}
}

func TestNativeHealingHateUsesRestoredHP(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	player := &engine.players[0]
	player.HP = 900
	action := battleAction{memberType: 2, target: 1, skill: CombatSkillDefinition{HateRatio: 100}}
	role := CombatSkillRole{Target: "SELECT", HateLimit: 1000}
	rows, err := engine.healPlayerTargets(action, role, 200)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Args[2] != 200 || player.HP != 1000 || engine.players[1].HateHistory[0] != 10 || engine.turnStats.Heal != 200 {
		t.Fatal("overheal must keep its requested result amount but only restored HP generates hate")
	}
	player.HP = 950
	player.Effects = []battleEffect{{Function: "REGENERATE_FIXED", Value: 200, Source: 2, Remaining: 2, HateRate: 100, HateLimit: 1000}}
	rows = engine.regenerateMembers()
	if len(rows) != 2 || rows[1].Args[2] != 200 || player.HP != 1000 || engine.players[1].HateHistory[0] != 15 || engine.turnStats.Heal != 200 {
		t.Fatal("regen hate or direct-heal-only AI total diverged from native")
	}
	player.HP = 500
	player.Effects = append(player.Effects, battleEffect{Function: "HEAL_REVERSE", Rate: 50, Remaining: 2})
	engine.regenerateMembers()
	if player.HP != 400 || player.DamageTaken != 100 || player.TurnDamage != 100 || engine.players[1].HateHistory[0] != 15 {
		t.Fatal("reversed regeneration must record damage without positive healing hate")
	}
	producer, _ := coreBattleFixture(t)
	_, err = producer.executePersistentEffect(battleAction{memberType: 1, target: 1, skill: CombatSkillDefinition{HateRatio: 123}},
		CombatSkillRole{Function: "REGENERATE_FIXED", Target: "SELF", HateLimit: 456, Parameters: [10]string{"2", "200"}}, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if effects := producer.players[0].Effects; len(effects) != 1 || effects[0].HateRate != 123 || effects[0].HateLimit != 456 {
		t.Fatal("regeneration did not freeze the casting skill's hate parameters")
	}
}
