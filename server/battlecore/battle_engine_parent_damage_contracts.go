package battlecore

import "fmt"

func validateParentDamageContracts() error {
	// Official party 40084001 is one body (40008411) plus three child parts
	// (40008412..40008414). This compact probe uses the same body/part identity
	// with a two-hit ATTACK_AA and ENCHANT to lock FUN_000838d0's parent path:
	// every ResultCmd60 keeps the original pre-commit part HP, the part commits
	// once, then the body receives one aggregate post-consumer commit.
	engine := &BattleEngine{rng: newXorShift128(1), enemyCount: 2}
	engine.players[0] = battlePlayer{
		MemberType: 1, ArthurType: 1, HP: 1000, MaxHP: 1000,
		Effects: []battleEffect{{Function: "ENCHANT", Value: 40, Attribute: "FIRE", Remaining: 2}},
	}
	engine.enemies[0] = battleEnemy{
		MemberType: 5, EnemyID: 40008411, HP: 1000, MaxHP: 1000,
		Effects: []battleEffect{{Function: "ENDURE", Value: 50, Remaining: 2}},
	}
	partLevel := CombatEnemyLevel{}
	partLevel.AttributeRates[combatAttributeIndex("FIRE")] = 150
	engine.enemies[1] = battleEnemy{
		MemberType: 6, EnemyID: 40008412, Parent: 1,
		HP: 1000, MaxHP: 1000, Defense: 20, Level: partLevel,
	}
	role := CombatSkillRole{RoleIndex: 9, Function: "ATTACK_AA", Target: "SELECT"}
	role.Parameters[0] = "100"
	role.Parameters[4] = "2"
	role.Parameters[7] = "FIRE"
	role.Parameters[8] = "PHYSICS"
	results, err := engine.executePlayerAttack(battleAction{memberType: 1, target: 6}, role, 1)
	if err != nil {
		return fmt.Errorf("parent damage attack: %w", err)
	}
	wantDamage := [][5]int64{
		{6, 9, -130, 1000, 50},
		{6, 9, -60, 1000, 20},
		{6, 9, -130, 1000, 50},
		{6, 9, -60, 1000, 20},
	}
	if len(results) != len(wantDamage)+2 {
		return fmt.Errorf("parent damage result count is %d, want %d: %+v", len(results), len(wantDamage)+2, results)
	}
	for index, want := range wantDamage {
		result := results[index]
		if result.Command != 60 || len(result.Args) != 10 {
			return fmt.Errorf("parent damage row %d is %+v", index, result)
		}
		for arg := range want {
			if result.Args[arg] != want[arg] {
				return fmt.Errorf("parent damage row %d arg %d is %d, want %d: %+v", index, arg, result.Args[arg], want[arg], result)
			}
		}
	}
	partHP := results[len(wantDamage)]
	parentHP := results[len(wantDamage)+1]
	if partHP.Command != CommandHP || len(partHP.Args) != 4 || partHP.Args[0] != 6 || partHP.Args[2] != 620 ||
		parentHP.Command != CommandHP || len(parentHP.Args) != 4 || parentHP.Args[0] != 5 || parentHP.Args[2] != 620 {
		return fmt.Errorf("parent damage HP commit order is part=%+v parent=%+v", partHP, parentHP)
	}
	if engine.enemies[1].HP != 620 || engine.enemies[1].DamageTaken != 380 ||
		engine.enemies[0].HP != 620 || engine.enemies[0].DamageTaken != 380 {
		return fmt.Errorf("parent damage durable state is body=%+v part=%+v", engine.enemies[0], engine.enemies[1])
	}
	if len(engine.enemies[0].Effects) != 1 || engine.enemies[0].Effects[0].Function != "ENDURE" || engine.enemies[0].Effects[0].Remaining != 2 {
		return fmt.Errorf("parent damage unexpectedly consumed body effects: %+v", engine.enemies[0].Effects)
	}
	return nil
}
