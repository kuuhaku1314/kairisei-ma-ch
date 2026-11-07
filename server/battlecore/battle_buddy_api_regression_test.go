package battlecore

import (
	"reflect"
	"testing"
)

func TestBuddyHandFilterAndSharedCardProjection(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.catalog.Cards[1] = CombatCardDefinition{ID: 1, NormalSkillID: 1, ProfileTags: [8]int{11, 22}}
	engine.catalog.Cards[2] = CombatCardDefinition{ID: 2, NormalSkillID: 1, ProfileTags: [8]int{11}}
	player := &engine.players[0]
	player.Hand = [5]int{1, 2}
	player.Deck[1].CardID = 2
	// 79358 tests each positive include-tag independently and does not reject
	// a sealed card merely because this is a Buddy hand-targeting operation.
	player.Effects = []battleEffect{{Function: "CARD_SEAL", CardType: 1, Remaining: 1}}
	filter := CombatSkillDefinition{ID: 7, Target: "HAND_ALL", HandAttribute: "FIRE", Groups: [3]int{11, 22}}
	cards, err := engine.selectBurstHandCards(player, filter, burstSkillSubmission{})
	if err != nil || !reflect.DeepEqual(cards, []int{1}) {
		t.Fatalf("HAND_ALL filter: cards=%v err=%v", cards, err)
	}
	filter.Target, filter.HandSelectCount = "HAND_SELECT", 1
	if _, err := engine.selectBurstHandCards(player, filter, burstSkillSubmission{CardTypes: [5]int{1}}); err != nil {
		t.Fatalf("same selected sealed card must pass: %v", err)
	}
	filter.Target, filter.Groups[2] = "HAND_ALL", 22
	cards, err = engine.selectBurstHandCards(player, filter, burstSkillSubmission{})
	if err != nil || !reflect.DeepEqual(cards, []int{1}) {
		t.Fatalf("third required group: cards=%v err=%v", cards, err)
	}
	filter.Groups[2] = 33
	cards, err = engine.selectBurstHandCards(player, filter, burstSkillSubmission{})
	if err != nil || len(cards) != 0 {
		t.Fatalf("missing third required group: cards=%v err=%v", cards, err)
	}
	player.CardBurstSkills[1], player.CardCostDown[1] = []battleCardBurstSkill{{SkillID: 7}, {SkillID: 8}}, 2
	if got := playerCardStateResult(player, 1).Args; !reflect.DeepEqual(got, []int64{1, 1, 5, 1}) {
		t.Fatalf("seal and Buddy state must coexist: %v", got)
	}
	if got := engine.playerCardCostResult(player, 1, 1).Args; !reflect.DeepEqual(got, []int64{1, 1, 0, 2, 0}) {
		t.Fatalf("discount is not the clamped cost: %v", got)
	}

	// Original JP 8.0.7 captures retain all three group selectors and allow
	// an ordinary or enemy-generated token when the actual filters match.
	t.Run("token hand filters and discount", func(t *testing.T) {
		engine, _ := coreBattleFixture(t)
		player := &engine.players[0]
		player.Hand = [5]int{23}
		player.Tokens[0] = battleToken{ID: 81000201, SkillID: 91999611, Level: 1}
		engine.catalog.Skills[91999611] = []CombatSkillDefinition{{ID: 91999611, Cost: 3, Kind: "SUPPORT", Attribute: "FIRE"}}
		for _, target := range []string{"HAND_SELECT", "HAND_ALL"} {
			for _, tc := range []struct {
				name   string
				filter CombatSkillDefinition
				want   bool
			}{
				{"unrestricted", CombatSkillDefinition{}, true},
				{"fire", CombatSkillDefinition{HandAttribute: "FIRE"}, true},
				{"ice", CombatSkillDefinition{HandAttribute: "ICE"}, false},
				{"support", CombatSkillDefinition{HandKind: "SUPPORT"}, true},
				{"attack", CombatSkillDefinition{HandKind: "ATTACK"}, false},
				{"minimum4", CombatSkillDefinition{HandMinCost: 4}, false},
				{"maximum2", CombatSkillDefinition{HandMaxCost: 2}, false},
				{"group1", CombatSkillDefinition{Groups: [3]int{11}}, false},
				{"group2", CombatSkillDefinition{Groups: [3]int{0, 11}}, false},
				{"group3", CombatSkillDefinition{Groups: [3]int{0, 0, 11}}, false},
			} {
				t.Run(target+"/"+tc.name, func(t *testing.T) {
					filter := tc.filter
					filter.Target, filter.HandSelectCount = target, 1
					submission := burstSkillSubmission{}
					if target == "HAND_SELECT" {
						submission.CardTypes[0] = 23
					}
					got, err := engine.selectBurstHandCards(player, filter, submission)
					if tc.want {
						if err != nil || !reflect.DeepEqual(got, []int{23}) {
							t.Fatalf("cards=%v err=%v", got, err)
						}
					} else if target == "HAND_SELECT" && err == nil || target == "HAND_ALL" && (err != nil || len(got) != 0) {
						t.Fatalf("filter unexpectedly admitted token: cards=%v err=%v", got, err)
					}
				})
			}
		}
		base := CombatSkillRole{Function: "NEED_COST_DOWN_BURST", Parameters: [10]string{"1", "1", "NULL", "NULL", "0", "0", "0", "9"}}
		for _, tc := range []struct {
			name      string
			parameter int
			value     string
			want      bool
		}{
			{"unrestricted", 2, "NULL", true}, {"fire", 2, "FIRE", true}, {"ice", 2, "ICE", false},
			{"support", 3, "SUPPORT", true}, {"attack", 3, "ATTACK", false},
			{"group1", 4, "11", false}, {"group2", 5, "11", false},
			{"minimum4", 6, "4", false}, {"maximum2", 7, "2", false},
		} {
			role := base
			role.Parameters[tc.parameter] = tc.value
			if got := engine.cardDiscountMatches(player, 23, role); got != tc.want {
				t.Fatalf("role %s: match=%v want=%v", tc.name, got, tc.want)
			}
		}
		action := battleAction{memberType: 1, skill: CombatSkillDefinition{ID: 91999613, Target: "HAND_SELECT"}}
		if _, err := engine.executeBurstRole(action, base, []int{23}); err != nil {
			t.Fatal(err)
		}
		if got := engine.playerCardCostResult(player, 23, 3).Args; !reflect.DeepEqual(got, []int64{1, 23, 2, 1, 0}) {
			t.Fatalf("original token3->2 projection: %v", got)
		}
	})
	for _, cardType := range []int{-1, 0, 1, 10, 11, 22, 23, 32, 33} {
		want := cardType >= 0 && cardType <= 10 || cardType >= 23 && cardType <= 32
		if IsHandCardInput(cardType) != want {
			t.Fatalf("hand input domain %d", cardType)
		}
	}
}

func TestBuddyGaugeDamageAndUnavailableStates(t *testing.T) {
	engine := &BattleEngine{catalog: &CombatCatalog{BurstGauge: CombatBurstGaugeConfig{Maximum: 300, DamageReduction: 50}}}
	for _, tc := range []struct {
		name                                   string
		state, gauge, damage, breakTurns, want int
		gameOver                               bool
		rows                                   int
	}{
		{"normal does not lose gauge", burstGaugeNormal, 100, 100, 0, 100, false, 0},
		{"positive damage minimum", burstGaugeBurst, 100, 1, 0, 99, false, 1},
		{"role total damage", burstGaugeBurst, 100, 199, 0, 91, false, 1},
		{"zero gauge is not revived", burstGaugeBurst, 0, 100, 0, 0, false, 0},
		{"break cooldown", burstGaugeBurst, 100, 100, 1, 100, false, 0},
		{"retired player", burstGaugeBurst, 100, 100, 0, 100, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := battlePlayer{MemberType: 1, MaxHP: 1000, BurstState: tc.state, Burst: tc.gauge, BurstBreak: tc.breakTurns, GameOver: tc.gameOver}
			rows := engine.burstDamageGaugeResults(&p, tc.damage)
			if p.Burst != tc.want || len(rows) != tc.rows {
				t.Fatalf("gauge=%d rows=%v", p.Burst, rows)
			}
		})
	}
	engine.players[0] = battlePlayer{MemberType: 1, BurstState: burstGaugeBurst}
	if rows := engine.addPlayerBurstGauge(1, 0, 30, true); len(rows) != 0 || engine.players[0].Burst != 0 {
		t.Fatalf("zero BURST gauge accepted quick-up: %v", rows)
	}
}

func TestHeldCardConsumesBurstModifierBeforeRelease(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.enemies[0].HP, engine.enemies[0].MaxHP = 10000, 10000
	skill := engine.catalog.Skills[1][0]
	skill.DamageKind = "PHYSICS"
	engine.catalog.Skills[1][0] = skill
	engine.catalog.Skills[2] = []CombatSkillDefinition{{ID: 2, FunctionID: 2}, {ID: 2, FunctionID: 3}}
	engine.catalog.SkillRoles[2] = []CombatSkillRole{{Function: "ATTACK_MULTISTAGE", Parameters: [10]string{"", "PHYSICS", "1"}}}
	engine.catalog.SkillRoles[3] = []CombatSkillRole{{Function: "ATTACK_MULTISTAGE", Parameters: [10]string{"", "PHYSICS", "3"}}}

	player := &engine.players[0]
	player.CardBurstSkills[1], player.CardCostDown[1] = []battleCardBurstSkill{{SkillID: 2, BranchIndex: 1}}, 1
	action := battleAction{memberType: 1, cardType: 1, cardID: 1, cardLevel: 1, target: 5, skill: skill}
	player.CardHolds = []battleCardHold{{Action: action, Remaining: 1}}
	// 664ee releases to the trash first, executes with modifiers, then clears
	// them through 45726. Redrawing must restore the original cost and hit count.
	rows, err := engine.executeCardHolds("")
	if err != nil {
		t.Fatal(err)
	}
	if damage := 10000 - engine.enemies[0].HP; damage != 600 {
		t.Fatalf("held skill lost its modifier before execution: damage=%d rows=%v", damage, rows)
	}
	if len(player.CardHolds) != 0 || !reflect.DeepEqual(player.Discard, []int{0}) {
		t.Fatal("held card did not return to the discard pool")
	}
	player.Hand[0], player.Discard = 1, nil
	if cost := engine.effectiveCardCost(player, 1, skill.Cost); cost != 1 {
		t.Fatalf("redrawn held card retained its discount: cost=%d", cost)
	}
	if _, err := engine.executePlayerAction(action, nil); err != nil {
		t.Fatal(err)
	}
	if damage := 9400 - engine.enemies[0].HP; damage != 200 {
		t.Fatalf("redrawn held card retained its hit modifier: damage=%d", damage)
	}
}
