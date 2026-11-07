package battlecore

import (
	"reflect"
	"testing"
)

func TestNativeBurstTransitionsPrecedeGlobalBuddyOrdering(t *testing.T) {
	catalog := &CombatCatalog{
		BurstGauge: CombatBurstGaugeConfig{NormalThreshold: 100, Maximum: 200, BreakTurns: 2},
		Buddies:    make(map[int]CombatBuddyDefinition), Skills: make(map[int][]CombatSkillDefinition),
		SkillRoles: make(map[int][]CombatSkillRole),
	}
	engine := &BattleEngine{catalog: catalog, rng: newXorShift128(1)}
	for i := range engine.players {
		engine.players[i] = battlePlayer{MemberType: i + 1, HP: 100, MaxHP: 100, Burst: 100, BurstState: burstGaugeNormal, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	}
	// Two buddies each on two users: ties within the same user must also roll.
	for i := 1; i <= 4; i++ {
		catalog.Buddies[i] = CombatBuddyDefinition{ID: i, PassiveSkillID: i}
		catalog.Skills[i] = []CombatSkillDefinition{{ID: i, FunctionID: i, Target: "SELF", PriorityPVE: 120}}
		catalog.SkillRoles[i] = []CombatSkillRole{{Function: "ATK_UP_FIXED", Target: "SELF", Parameters: [10]string{"1", "ATK", "1000", "10"}}}
		engine.players[(i-1)/2].Buddies[(i-1)%2] = BattleBuddy{BuddyType: (i-1)%2 + 1, BuddyID: i, Level: 1}
	}
	engine.players[2].HP = 0
	engine.players[3].HP, engine.players[3].BurstState, engine.players[3].BurstBreak = 0, burstGaugeBreak, 0
	rows, err := engine.transitionBurstStates()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 2 || rows[0].Command != CommandBurstStateChange || rows[1].Command != CommandBurstStateChange {
		t.Fatalf("all entry transitions must precede passives: %+v", rows)
	}
	var skillIDs []int64
	for _, row := range rows {
		if row.Command == CommandBurstSkill {
			skillIDs = append(skillIDs, row.Args[2])
		}
	}
	if !reflect.DeepEqual(skillIDs, []int64{2, 1, 4, 3}) || engine.rng.next() != 3556250659 {
		t.Fatalf("buddy sort must use six native tie rolls: %v", skillIDs)
	}
	if engine.players[2].BurstState != burstGaugeNormal || engine.players[3].BurstState != burstGaugeBreak {
		t.Fatal("KO members must not transition burst state")
	}
	for i := 0; i < 2; i++ {
		if engine.players[i].Attack != 20 || len(engine.players[i].Effects) != 2 {
			t.Fatalf("user %d lost a buddy passive", i+1)
		}
	}
}

func TestNativeBurstBreakClearsPassiveAndCardListsBefore504(t *testing.T) {
	engine := &BattleEngine{catalog: &CombatCatalog{BurstGauge: CombatBurstGaugeConfig{BreakTurns: 2}}}
	player := &engine.players[0]
	*player = battlePlayer{MemberType: 1, HP: 100, MaxHP: 100, BaseMaxHP: 100, BurstState: burstGaugeBurst, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	player.Effects = []battleEffect{
		{Function: "ATK_UP_FIXED", Parameter: "ATK", Delta: 10, Value: 10, Kind: 1, ListType: 6},
		{Function: "ATK_UP_FIXED", Parameter: "ATK", Delta: 20, Value: 20, Kind: 1, ListType: 6},
		{Function: "CARD_SEAL", CardType: 1, ListType: 7},
		{Function: "ATK_UP_FIXED", Parameter: "ATK", Delta: 5, Value: 5, Kind: 1, ListType: 5},
	}
	refreshPlayerBattleParameters(player)
	rows, err := engine.transitionBurstStates()
	if err != nil {
		t.Fatal(err)
	}
	var commands []int
	for _, row := range rows {
		commands = append(commands, row.Command)
	}
	if !reflect.DeepEqual(commands, []int{72, CommandBaseParam, CommandBattleParam, CommandBurstStateChange}) || rows[0].Args[1] != 6 {
		t.Fatalf("native BREAK release wire is 72(list6),5,6,504: %+v", rows)
	}
	if len(player.Effects) != 1 || player.Effects[0].ListType != 5 || player.Attack != 5 || player.BurstBreak != 2 {
		t.Fatalf("break cleared wrong lists or retained parameters: %+v", player)
	}
	player.BurstState = burstGaugeBurst
	rows, err = engine.transitionBurstStates()
	if err != nil || len(rows) != 3 || rows[0].Command != CommandBaseParam || rows[1].Command != CommandBattleParam || rows[2].Command != CommandBurstStateChange {
		t.Fatalf("empty BURST_PASSIVE still emits 5/6 before 504: %+v, %v", rows, err)
	}
}

func TestNativeBurstPassiveChecksReceivingState(t *testing.T) {
	engine := &BattleEngine{catalog: &CombatCatalog{}}
	for i := range engine.players {
		engine.players[i] = battlePlayer{MemberType: i + 1, HP: 100, MaxHP: 100, BurstState: burstGaugeNormal, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	}
	engine.players[1].BurstState = burstGaugeBurst
	role := CombatSkillRole{Function: "DEF_UP_FIXED", Target: "USER_ALL", Parameters: [10]string{"1", "DEF", "1000", "10"}}
	rows, err := engine.executeBurstRoleWithListType(battleAction{memberType: 1, cardLevel: 1}, role, nil, 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Command != CommandPassiveBuff || rows[0].Args[0] != 2 || rows[0].Args[2] != 6 {
		t.Fatalf("only BURST recipient accepts list6: %+v", rows)
	}
	for i := range engine.players {
		want := 0
		if i == 1 {
			want = 10
		}
		if engine.players[i].Defense != want {
			t.Fatalf("member %d defense=%d", i+1, engine.players[i].Defense)
		}
	}
}
