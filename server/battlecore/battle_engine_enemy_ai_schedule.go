package battlecore

import "strings"

func nativeEnemyAITurnEnabled(fields []string, turn int) bool {
	if turn < 0 {
		return false
	}
	loopStart := combatParameterInt(fields[18])
	column := 7 + turn // CSV columns7..17 are turns0..10, inclusive.
	if loopStart > 0 && turn >= loopStart {
		column = 19 + (turn-loopStart)%10
	} else if turn > 10 {
		return false
	}
	return int16(combatParameterInt(fields[column])) > 0
}

// 57f16 dispatches to separate predicates. PARTS_n is an indexed child in
// member order, not a broken/alive count. BREAK2/ALIVE may be used by a part
// and resolve its parent first; plain BREAK/FULL/ALL_BREAK only accept a body.
func (engine *BattleEngine) enemyAIPartsCondition(actor *battleEnemy, kind string) bool {
	if combatNullValue(kind) {
		return true
	}
	owner := actor
	includeParent := strings.HasSuffix(kind, "_BREAK2") || strings.HasSuffix(kind, "_ALIVE")
	if actor.Parent > 0 {
		if !includeParent || actor.Parent > engine.enemyCount {
			return false
		}
		owner = &engine.enemies[actor.Parent-1]
	}
	parts := make([]*battleEnemy, 0, 3)
	for i := 0; i < engine.enemyCount; i++ {
		part := &engine.enemies[i]
		if part.Parent > 0 && part.Parent+4 == owner.MemberType {
			parts = append(parts, part)
		}
	}
	switch kind {
	case "PARTS_FULL", "PARTS_ALL_BREAK":
		if len(parts) == 0 {
			return false
		}
		for _, part := range parts {
			if (part.HP > 0) != (kind == "PARTS_FULL") {
				return false
			}
		}
		return true
	case "PARTS_1_BREAK", "PARTS_2_BREAK", "PARTS_3_BREAK", "PARTS_1_BREAK2", "PARTS_2_BREAK2", "PARTS_3_BREAK2",
		"PARTS_1_ALIVE", "PARTS_2_ALIVE", "PARTS_3_ALIVE":
		index := int(kind[len("PARTS_")] - '1')
		return index < len(parts) && (parts[index].HP > 0) == strings.HasSuffix(kind, "_ALIVE")
	default:
		return false
	}
}
