package battlecore

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

func SupportsEnemyActionTarget(kind string) bool {
	kind = strings.ToUpper(strings.TrimSpace(kind))
	if order := enemyFixedUserOrder(kind); order[0] != 0 {
		return true
	}
	if kind == "" || kind == "NULL" || kind == "SELF" || kind == "SELECT" || kind == "USER_ONE" || kind == "FRIEND_ONE" ||
		kind == "USER_ALL" || kind == "FRIEND_ALL" || kind == "ENEMY_ONE" || kind == "ENEMY_ALL" || kind == "ALL" ||
		kind == "RANDOM" || kind == "RANDOM_INVOLVE_DEAD" || kind == "RANDOM_EXCEPT_HATE1" || kind == "HATE1_OR_RANDOM" ||
		kind == "BULLY" || kind == "DEAD_ENEMY_RANDOM" || kind == "TRIGGER_TARGET" || kind == "USER_DEBUFF" ||
		kind == "USER_PARAM_BUFF" || kind == "USER_PARAM_DEBUFF" || kind == "USER_HP" || kind == "USER_HP_RATE" ||
		kind == "USER_HAND_NUM" || kind == "USER_DEBUFF_DUPLICATE_SELECT" || kind == "SUMMONS" ||
		kind == "WEAKNESS_USER" || kind == "WEAKNESS_USER_NOT_FOR_RANDOM" || kind == "WEAKNESS_USER_INVOLVE_DEAD_NOT_FOR_RANDOM" {
		return true
	}
	if _, ok := map[string]struct{}{
		"MERCENARY": {}, "MILLIONAIRE": {}, "THIEF": {}, "SINGER": {},
		"MERCENARY_INVOLVE_DEAD": {}, "MILLIONAIRE_INVOLVE_DEAD": {}, "THIEF_INVOLVE_DEAD": {}, "SINGER_INVOLVE_DEAD": {},
		"RANDOM_EXCEPT_MERCENARY": {}, "RANDOM_EXCEPT_MILLIONAIRE": {}, "RANDOM_EXCEPT_THIEF": {}, "RANDOM_EXCEPT_SINGER": {},
	}[kind]; ok {
		return true
	}
	if strings.HasPrefix(kind, "HATE") && len(kind) == len("HATE1") {
		rank := combatParameterInt(strings.TrimPrefix(kind, "HATE"))
		return rank >= 1 && rank <= 4
	}
	if strings.HasPrefix(kind, "ENEMY") && len(kind) == len("ENEMY1") {
		index := combatParameterInt(strings.TrimPrefix(kind, "ENEMY"))
		return index >= 1 && index <= 4
	}
	if strings.HasSuffix(kind, "_USER") {
		name := strings.TrimSuffix(kind, "_USER")
		name = strings.TrimPrefix(strings.TrimPrefix(name, "HIGH_"), "LOW_")
		switch name {
		case "MAX_HP", "HP", "ATK", "INT", "MND", "DEF", "MDEF", "CRITICAL", "CURSE_NUM", "BLESS_NUM":
			return true
		}
	}
	if strings.HasSuffix(kind, "_ENEMY") {
		name := strings.TrimSuffix(kind, "_ENEMY")
		name = strings.TrimPrefix(strings.TrimPrefix(name, "HIGH_"), "LOW_")
		switch name {
		case "MAX_HP", "HP", "ATK", "INT", "MND", "DEF", "MDEF":
			return true
		}
	}
	return false
}

func SupportsEnemyAITrigger(trigger string) bool {
	switch trigger {
	case "TRANCE_GAUGE_STATE", "TRANCE_GAUGE_VALUE", "TRANCE_GAUGE_STATE_CHANGE":
		return true
	case "HEAL", "DRAIN", "ATTR", "TURN_SKILL_ATTACK_DAMAGE", "ATTACK_AND_ENCHANT_DAMAGE", "ATTACK_AND_ENCHANT_DAMAGE_TURN_APPOINT":
		return true
	case "USER_BLESS", "USER_CURSE", "USER_EXEC_BLESS", "USER_EXEC_CURSE", "USER_BURST_GAUGE_STATE",
		"USER_BURST_GAUGE_VALUE", "USER_BURST_BUFF", "USER_BURST_BUFF_AND", "USER_CARD_BUFF":
		return true
	case "", "NULL", "PHYSIC_DAMAGE", "MAGIC_DAMAGE", "BAD_STATUS", "NOT_TARGET", "DEAD_COUNT",
		"SKILL_ROLE_KIND_DEBUFF_NOW_TURN", "SKILL_ROLE_KIND_DEBUFF", "SKILL_ROLE_KIND_BUFF", "DAMAGE",
		"AI_FLAG", "HEAL_TOTAL", "PHYSIC_DAMAGE_LARGE", "MAGIC_DAMAGE_LARGE",
		"SKILL_ROLE_KIND_BUFF_NOW_TURN_BY_USER", "SKILL_ROLE_KIND_DEBUFF_BY_USER", "SKILL_ROLE_KIND_BUFF_BY_USER",
		"ENEMY_DEAD", "DECK_COMBO_COUNT", "DAMAGE_NUM", "SKILL_KIND", "ENCHANT_DAMAGE", "USER_PARAM_ONE",
		"DEAL_NUM_BY_HIGH_USER", "USER_HAND_NUM_ONE", "USER_HAND_NUM", "SKILL_ROLE_KIND_DEBUFF_BY_ENEMY_ONE",
		"SKILL_ROLE_KIND_DEBUFF_BY_ENEMY", "SKILL_ROLE_KIND_DEBUFF_BY_ENEMY_ONE_AND", "USER_DEAD",
		"SKILL_ROLE_KIND_DEBUFF_BY_USER_ONE", "SKILL_ROLE_KIND_BUFF_BY_USER_ONE", "ALL_DAMAGE",
		"ALL_DAMAGE_TURN_APPOINT", "ENEMY_ALIVE", "AI_FLAG_NONE", "AI_VAR", "USER_PLAY_CARD_NUM":
		return true
	case "DEAL_NUM_BY_LOW_USER", "DEAL_NUM_ONE", "ENEMY_ALIVE_OR", "USER_HP_RATE", "USER_HP_RATE_ONE",
		"USER_PARAM_RANK", "USER_CRITICAL", "USER_PLAY_CARD_NUM_TOTAL", "SKILL_ROLE_KIND_DEBUFF_BY_ENEMY_AND",
		"SKILL_ROLE_KIND_DEBUFF_BY_USER_ONE_AND", "SKILL_ROLE_KIND_BUFF_BY_USER_ONE_AND":
		return true
	default:
		return false
	}
}

func enemyAITriggerRetainsTarget(trigger string) bool {
	switch strings.ToUpper(strings.TrimSpace(trigger)) {
	case "ATTACK_AND_ENCHANT_DAMAGE_TURN_APPOINT":
		return true
	case "USER_BLESS", "USER_CURSE", "USER_EXEC_BLESS", "USER_EXEC_CURSE", "USER_BURST_GAUGE_STATE",
		"USER_BURST_GAUGE_VALUE", "USER_BURST_BUFF", "USER_BURST_BUFF_AND", "USER_CARD_BUFF":
		return true
	case "USER_PARAM_ONE", "USER_PLAY_CARD_NUM", "DEAL_NUM_BY_HIGH_USER", "USER_HAND_NUM", "USER_HAND_NUM_ONE",
		"ALL_DAMAGE_TURN_APPOINT",
		"SKILL_ROLE_KIND_DEBUFF_BY_USER_ONE", "SKILL_ROLE_KIND_BUFF_BY_USER_ONE",
		"SKILL_ROLE_KIND_DEBUFF_BY_ENEMY", "SKILL_ROLE_KIND_DEBUFF_BY_ENEMY_ONE", "SKILL_ROLE_KIND_DEBUFF_BY_ENEMY_ONE_AND":
		return true
	case "DEAL_NUM_BY_LOW_USER", "DEAL_NUM_ONE", "USER_HP_RATE", "USER_HP_RATE_ONE", "USER_PARAM_RANK",
		"SKILL_ROLE_KIND_DEBUFF_BY_ENEMY_AND", "SKILL_ROLE_KIND_DEBUFF_BY_USER_ONE_AND", "SKILL_ROLE_KIND_BUFF_BY_USER_ONE_AND":
		return true
	default:
		return false
	}
}

func validateEnemyAIConditionContracts() error {
	if skillRoleKindBuff(battleEffect{Function: "ATK_UP_BOOST"}) != 38 ||
		skillRoleKindBuff(battleEffect{Function: "ATK_UP_BY_MAX_HP"}) != 32 ||
		skillRoleKindBuff(battleEffect{Function: "ATK_BREAK_BOOST"}) != 40 {
		return errors.New("native skill-role kind mapping collapsed a specialized boost/max-HP enum")
	}
	orders := map[int]CombatEnemyAIOrder{
		1:  {ID: 1, Fields: enemyAIConditionFields("SKILL_ROLE_KIND_DEBUFF_BY_USER", "WEAKNESS")},
		2:  {ID: 2, Fields: enemyAIConditionFields("SKILL_ROLE_KIND_DEBUFF", "BURN")},
		3:  {ID: 3, Fields: enemyAIConditionFields("SKILL_ROLE_KIND_DEBUFF_BY_ENEMY_ONE_AND", "ENEMY2", "BURN", "ATK_BREAK_BY_ATK")},
		4:  {ID: 4, Fields: enemyAIConditionFields("SKILL_ROLE_KIND_BUFF_BY_USER_ONE", "MERCENARY", "ATK_UP_BY_INT")},
		5:  {ID: 5, Fields: enemyAIConditionFields("DAMAGE_NUM", "PHYSICS", "2", "3")},
		6:  {ID: 6, Fields: enemyAIConditionFields("NOT_TARGET")},
		7:  {ID: 7, Fields: enemyAIConditionFields("ALL_DAMAGE", "ENEMY2", "100", "100", "NULL", "ALL", "NULL")},
		8:  {ID: 8, Fields: enemyAIConditionFields("USER_PLAY_CARD_NUM", "SINGER", "NULL", "SUPPORT", "2", "5")},
		9:  {ID: 9, Fields: enemyAIConditionFields("AI_FLAG", "1", "2")},
		10: {ID: 10, Fields: enemyAIConditionFields("ENEMY_DEAD", "ENEMY1", "ENEMY2")},
		11: {ID: 11, Fields: enemyAIConditionFields("PHYSIC_DAMAGE_LARGE", "1")},
		12: {ID: 12, Fields: enemyAIConditionFields("USER_PARAM_ONE", "NULL", "ATK", "190", "210", "1")},
		13: {ID: 13, Fields: enemyAIConditionFields("SKILL_ROLE_KIND_DEBUFF", "WEAKNESS")},
		14: {ID: 14, Fields: enemyAIConditionFields("SKILL_ROLE_KIND_DEBUFF_BY_USER_ONE", "MERCENARY", "WEAKNESS")},
		15: {ID: 15, Fields: enemyAIConditionFields("SKILL_ROLE_KIND_DEBUFF_NOW_TURN", "POISON")},
		16: {ID: 16, Fields: enemyAIConditionFields("SKILL_ROLE_KIND_BUFF_NOW_TURN_BY_USER", "ATK_UP_BY_INT")},
	}
	engine := &BattleEngine{turn: 1, enemyCount: 2, catalog: &CombatCatalog{EnemyAIOrders: orders}}
	for index := range engine.players {
		engine.players[index] = battlePlayer{MemberType: index + 1, ArthurType: index + 1, HP: 100, MaxHP: 100, Attack: 100 + index*25, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	}
	engine.players[0].Effects = []battleEffect{
		{Function: "WEAKNESS", Kind: 2, Remaining: 2, AppliedTurn: 1},
		{Function: "ATK_UP_FIXED", Parameter: "INT", Delta: 100, Kind: 1, Remaining: 2, AppliedTurn: 1},
	}
	engine.players[3].Attack = 200
	engine.enemies[0] = battleEnemy{MemberType: 5, HP: 100, MaxHP: 100, Effects: []battleEffect{{Function: "BURN", Kind: 2, Remaining: 2, AppliedTurn: 1}}}
	engine.enemies[0].Effects = append(engine.enemies[0].Effects, battleEffect{Function: "POISON", Kind: 2, Remaining: 2, AppliedTurn: 1})
	engine.enemies[1] = battleEnemy{MemberType: 6, HP: 100, MaxHP: 100, Effects: []battleEffect{
		{Function: "BURN", Kind: 2, Remaining: 2, AppliedTurn: 1},
		{Function: "ATK_BREAK_FIXED", Parameter: "ATK", Delta: -100, Kind: 2, Remaining: 2, AppliedTurn: 1},
	}}
	engine.enemies[0].AITurn.Hits[0] = 2
	engine.enemies[0].AITurn.Damage = [3]int64{5, 3}
	engine.turnStats.DamageEvents = []battleDamageEvent{{Target: 5, Value: 900}, {Target: 6, Value: 100}}
	engine.turnActions = []battleAction{
		{memberType: 4, cardType: 1, skill: CombatSkillDefinition{Kind: "SUPPORT"}},
		{memberType: 4, cardType: 2, skill: CombatSkillDefinition{Kind: "SUPPORT"}},
	}
	engine.selectedPlays = map[int]cardPlaySubmission{4: {CardTypes: [5]int{1, 2}}}
	engine.turnStats.KindsByUser[3] = map[string]int{"SUPPORT": 2}
	engine.recordAIStatusApplied(5, battleEffect{Function: "POISON"})
	engine.recordAIStatusApplied(1, battleEffect{Function: "ATK_UP_FIXED", Parameter: "INT", Delta: 100, Source: 1})
	engine.enemies[0].AIFlags = 1<<1 | 1<<2
	for _, conditionID := range []int{1, 2, 3, 4, 5, 7, 8, 9, 11, 12, 14, 15, 16} {
		if !engine.enemyAIConditionSatisfied(&engine.enemies[0], conditionID) {
			return fmt.Errorf("native enemy AI condition simulation %d did not match", conditionID)
		}
	}
	engine.enemyTriggerTarget = 0
	if !engine.enemyAIConditionSatisfied(&engine.enemies[0], 14) || engine.enemyTriggerTarget != 1 {
		return fmt.Errorf("native user-one status trigger retained member %d, want 1", engine.enemyTriggerTarget)
	}
	if target, ok := engine.selectEnemyActionTarget(&engine.enemies[0], CombatEnemyAction{Target: "TRIGGER_TARGET"}); !ok || target != 1 {
		return fmt.Errorf("native TRIGGER_TARGET resolved member %d ok=%t, want 1", target, ok)
	}
	engine.enemyTriggerTarget = 0
	if !engine.enemyAIConditionSatisfied(&engine.enemies[0], 8) || engine.enemyTriggerTarget != 4 {
		return fmt.Errorf("native card-count trigger retained member %d, want 4", engine.enemyTriggerTarget)
	}
	engine.enemyTriggerTarget = 0
	if !engine.enemyAIConditionSatisfied(&engine.enemies[0], 12) || engine.enemyTriggerTarget != 4 {
		return fmt.Errorf("native ranked-parameter trigger retained member %d, want 4", engine.enemyTriggerTarget)
	}
	if engine.enemyAIConditionSatisfied(&engine.enemies[0], 13) {
		return errors.New("native enemy-local debuff trigger leaked player status into enemy scope")
	}
	if engine.enemyAIConditionSatisfied(&engine.enemies[0], 6) {
		return errors.New("native NOT_TARGET trigger accepted a damaged turn")
	}
	engine.enemies[0].AITurn.Damage = [3]int64{}
	if !engine.enemyAIConditionSatisfied(&engine.enemies[0], 6) {
		return errors.New("native NOT_TARGET trigger rejected an undamaged enemy")
	}
	engine.enemies[0].AIFlags &^= 1 << 2
	if engine.enemyAIConditionSatisfied(&engine.enemies[0], 9) {
		return errors.New("native AI_FLAG trigger accepted a partially-set flag tuple")
	}
	engine.enemies[0].HP, engine.enemies[1].HP = 0, 100
	if !engine.enemyAIConditionSatisfied(&engine.enemies[0], 15) {
		return errors.New("native NOW_TURN enemy-status trigger discarded a status applied before member death")
	}
	engine.players[0].HP = 0
	if !engine.enemyAIConditionSatisfied(&engine.enemies[0], 16) {
		return errors.New("native NOW_TURN player-status trigger discarded a status applied before member death")
	}
	if engine.enemyAIConditionSatisfied(&engine.enemies[0], 10) {
		return errors.New("native ENEMY_DEAD trigger accepted a partially-dead tuple")
	}
	engine.enemies[1].HP = 0
	if !engine.enemyAIConditionSatisfied(&engine.enemies[0], 10) {
		return errors.New("native ENEMY_DEAD trigger rejected an all-dead tuple")
	}
	return nil
}

func enemyAIConditionFields(trigger string, arguments ...string) []string {
	fields := make([]string, 38)
	fields[1] = "NULL"
	// Trigger-only fixtures explicitly enable all turn columns; blank native
	// columns mean disabled, not an omitted constraint.
	for i := 7; i <= 28; i++ {
		fields[i] = "1"
	}
	fields[18] = "11"
	fields[29] = trigger
	copy(fields[30:], arguments)
	return fields
}

func ValidEnemyAIOrderFields(fields []string) bool {
	// Original ParseTrigger returns Trigger.Empty for NULL and skips its
	// unused parameter tail. Official rows such as 37601617 have 31 fields.
	return len(fields) >= 30 && (len(fields) >= 38 || fields[29] == "" || fields[29] == "NULL")
}

func (engine *BattleEngine) enemyAIConditionSatisfied(enemy *battleEnemy, conditionID int) bool {
	if conditionID == 0 {
		return true
	}
	condition, exists := engine.catalog.EnemyAIOrders[conditionID]
	return exists && engine.enemyAIOrderSatisfied(enemy, condition)
}

func (engine *BattleEngine) enemyActionConditionSatisfied(enemy *battleEnemy, action CombatEnemyAction) bool {
	if action.Order != nil {
		return engine.enemyAIOrderSatisfied(enemy, *action.Order)
	}
	return engine.enemyAIConditionSatisfied(enemy, action.AIConditionID)
}

func (engine *BattleEngine) enemyAIOrderSatisfied(enemy *battleEnemy, condition CombatEnemyAIOrder) bool {
	if !ValidEnemyAIOrderFields(condition.Fields) {
		return false
	}
	fields := condition.Fields
	if !engine.enemyAIPartsCondition(enemy, fields[1]) || !engine.enemyTranceCondition(enemy, fields[2]) || !nativeEnemyAITurnEnabled(fields, engine.turn-engine.enemyStartTurn) {
		return false
	}
	if !combatValueInRange(enemy.HP*100/maxInt(1, enemy.MaxHP), fields[3], fields[4]) {
		return false
	}
	// 5aaf0 checks parent HP only for actors which actually have a parent.
	// An independent body does not read member5's HP or its own HP twice.
	if enemy.Parent > 0 {
		if enemy.Parent > engine.enemyCount {
			return false
		}
		parent := &engine.enemies[enemy.Parent-1]
		if !combatValueInRange(parent.HP*100/maxInt(1, parent.MaxHP), fields[5], fields[6]) {
			return false
		}
	}
	switch fields[29] {
	case "TRANCE_GAUGE_STATE", "TRANCE_GAUGE_VALUE", "TRANCE_GAUGE_STATE_CHANGE":
		return engine.enemyAITranceCondition(fields[29], fields[30:38])
	case "HEAL":
		return anyPlayerStatisticAtLeast(engine.turnStats.HealReceived[:], fields[30])
	case "DRAIN":
		return anyPlayerStatisticAtLeast(engine.turnStats.DrainByUser[:], fields[30])
	case "ATTR":
		return engine.executedAttributeMatches(fields[30:33])
	case "TURN_SKILL_ATTACK_DAMAGE":
		return enemyMaxSkillDamageMatches(enemy, fields[30], fields[31], fields[32], fields[33])
	case "ATTACK_AND_ENCHANT_DAMAGE":
		return engine.attackAndEnchantMatches(fields[30:38])
	case "ATTACK_AND_ENCHANT_DAMAGE_TURN_APPOINT":
		return engine.damageTurnMatches([]string{fields[30], fields[31], fields[32], fields[33], fields[34], "NULL", fields[35], fields[36]}, true)
	case "USER_BLESS", "USER_CURSE", "USER_EXEC_BLESS", "USER_EXEC_CURSE", "USER_BURST_GAUGE_STATE",
		"USER_BURST_GAUGE_VALUE", "USER_BURST_BUFF", "USER_BURST_BUFF_AND", "USER_CARD_BUFF":
		return engine.enemyAIHoldStateCondition(fields[29], fields[30:38])
	case "", "NULL":
		return true
	case "NOT_TARGET":
		return enemy.AITurn.Damage[0]+enemy.AITurn.Damage[1]+enemy.AITurn.Damage[2] <= 0
	case "AI_FLAG":
		return enemyAIFlagsAll(enemy, fields[30:33], true)
	case "AI_FLAG_NONE":
		return enemyAIFlagsNone(enemy, fields[30:33], combatParameterInt(fields[33]) != 0)
	case "AI_VAR":
		return enemyAIVariableInRange(enemy, fields[30], fields[31], fields[32], false)
	case "ENEMY_DEAD":
		return engine.enemyMembersAll(fields[30:33], false)
	case "ENEMY_ALIVE":
		return engine.enemyMembersAll(fields[30:33], true)
	case "DEAD_COUNT":
		target, exists := engine.enemyByTriggerName(fields[30])
		return exists && combatValueInRange(target.DeathCount, fields[31], fields[32])
	case "DAMAGE":
		return enemy.AITurn.Damage[0]+enemy.AITurn.Damage[1]+enemy.AITurn.Damage[2] >= int64(combatParameterInt(fields[30]))
	case "DAMAGE_NUM":
		count, exists := enemyAIHitCount(enemy, fields[30])
		return exists && combatValueInRange(count, fields[31], fields[32])
	case "PHYSIC_DAMAGE":
		return enemy.AITurn.Damage[0]+enemy.AITurn.Damage[2] >= int64(combatParameterInt(fields[30]))
	case "MAGIC_DAMAGE":
		return enemy.AITurn.Damage[1]+enemy.AITurn.Damage[2] >= int64(combatParameterInt(fields[30]))
	case "PHYSIC_DAMAGE_LARGE":
		return enemy.AITurn.Damage[0]-enemy.AITurn.Damage[1] >= int64(combatParameterInt(fields[30]))
	case "MAGIC_DAMAGE_LARGE":
		return enemy.AITurn.Damage[1]-enemy.AITurn.Damage[0] >= int64(combatParameterInt(fields[30]))
	case "ENCHANT_DAMAGE":
		return engine.damageEventTotal(enemy.MemberType, fields[30], "ALL", "NULL", true) >= combatParameterInt(fields[31])
	case "HEAL_TOTAL":
		return engine.turnStats.Heal >= combatParameterInt(fields[30])
	case "DECK_COMBO_COUNT":
		return combatValueInRange(engine.turnStats.MaxChain, fields[30], fields[31])
	case "SKILL_KIND":
		return engine.anySkillKind(fields[30:33])
	case "USER_PLAY_CARD_NUM":
		return engine.userPlayCardCountMatches(fields[30], fields[31], fields[32], fields[33], fields[34])
	case "DEAL_NUM_BY_HIGH_USER":
		return engine.highestUserDrawMatches(fields[30], fields[31])
	case "USER_HAND_NUM":
		return engine.userHandCountMatches("NULL", fields[30], fields[31])
	case "USER_HAND_NUM_ONE":
		return engine.userHandCountMatches(fields[30], fields[31], fields[32])
	case "USER_PARAM_ONE":
		return engine.userParameterMatches(fields[30], fields[31], fields[32], fields[33], combatParameterInt(fields[34]))
	case "USER_PARAM_RANK":
		return engine.userParameterRankMatches(fields[30], combatParameterInt(fields[31]), fields[32], fields[33])
	case "USER_HP_RATE":
		return engine.userHPRateMatches("NULL", fields[30], fields[31], 0)
	case "USER_HP_RATE_ONE":
		return engine.userHPRateMatches(fields[30], fields[31], fields[32], combatParameterInt(fields[33]))
	case "DEAL_NUM_ONE":
		return engine.userDrawRankMatches(fields[30], fields[31], fields[32], combatParameterInt(fields[33]))
	case "DEAL_NUM_BY_LOW_USER":
		return engine.userDrawRankMatches("NULL", fields[30], fields[31], 4)
	case "USER_CRITICAL":
		for _, member := range engine.playerTargetCandidates(false) {
			if nativeOptionalRange(nativeCriticalRate(engine.players[member-1].Effects, 0), fields[30], fields[31]) {
				return true
			}
		}
		return false
	case "USER_PLAY_CARD_NUM_TOTAL":
		return engine.userTotalCardCountMatches(fields[30], fields[31], fields[32], fields[33])
	case "ENEMY_ALIVE_OR":
		for _, name := range fields[30:33] {
			if target, ok := engine.enemyByTriggerName(name); ok && target.HP > 0 {
				return true
			}
		}
		return false
	case "USER_DEAD":
		return engine.userDeadMatches(fields[30], combatParameterInt(fields[31]))
	case "BAD_STATUS":
		return enemyAIBadStatusPresent(enemy, fields[30:33])
	case "SKILL_ROLE_KIND_DEBUFF_NOW_TURN", "SKILL_ROLE_KIND_DEBUFF", "SKILL_ROLE_KIND_BUFF",
		"SKILL_ROLE_KIND_BUFF_NOW_TURN_BY_USER", "SKILL_ROLE_KIND_DEBUFF_BY_USER", "SKILL_ROLE_KIND_BUFF_BY_USER",
		"SKILL_ROLE_KIND_DEBUFF_BY_ENEMY_ONE", "SKILL_ROLE_KIND_DEBUFF_BY_ENEMY", "SKILL_ROLE_KIND_DEBUFF_BY_ENEMY_ONE_AND",
		"SKILL_ROLE_KIND_DEBUFF_BY_USER_ONE", "SKILL_ROLE_KIND_BUFF_BY_USER_ONE",
		"SKILL_ROLE_KIND_DEBUFF_BY_ENEMY_AND", "SKILL_ROLE_KIND_DEBUFF_BY_USER_ONE_AND", "SKILL_ROLE_KIND_BUFF_BY_USER_ONE_AND":
		return engine.enemyAIStatusCondition(enemy, fields[29], fields[30:38])
	case "ALL_DAMAGE_TURN_APPOINT":
		return engine.allDamageTurnMatches(fields[30:38])
	case "ALL_DAMAGE":
		if !nativeAIDamageFiltersSupported(fields[33], fields[34], fields[35]) {
			return false
		}
		value := engine.damageEventTotalByTrigger(fields[30], fields[33], fields[34], fields[35])
		return combatValueInRange(value, fields[31], fields[32])
	default:
		// Damage/card-count trigger families depend on counters populated by
		// preceding actions. Until one has fired their condition is false.
		return false
	}
}

func (enemy *battleEnemy) hasAIFlag(flag int) bool {
	// d20e9/d20a0 test both supplied bit indexes, including bit0. Unlike
	// those skill branches, AI_FLAG/AI_FLAG_NONE ignore nonpositive selectors.
	return enemy.AIFlags&(uint32(1)<<(uint(flag)&31)) != 0
}

func enemyAIFlagsAll(enemy *battleEnemy, values []string, wantSet bool) bool {
	for _, value := range values {
		flag := combatParameterInt(value)
		if flag <= 0 {
			continue
		}
		set := enemy.hasAIFlag(flag)
		if set != wantSet {
			return false
		}
	}
	return true
}

func enemyAIFlagsNone(enemy *battleEnemy, values []string, requireAllUnset bool) bool {
	for _, value := range values {
		flag := combatParameterInt(value)
		if flag <= 0 {
			continue
		}
		if !enemy.hasAIFlag(flag) {
			if !requireAllUnset {
				return true
			}
			continue
		}
		if requireAllUnset {
			return false
		}
	}
	return requireAllUnset
}

func (engine *BattleEngine) enemyMembersAll(values []string, alive bool) bool {
	for _, value := range values {
		if combatNullValue(value) {
			continue
		}
		enemy, exists := engine.enemyByTriggerName(value)
		// 4f68c/4f7a0 ignore absent members. A stage may reuse a condition
		// containing ENEMY4 even when it has only a body and two parts.
		if exists && (enemy.HP > 0) != alive {
			return false
		}
	}
	return true
}

func (engine *BattleEngine) enemyByTriggerName(value string) (*battleEnemy, bool) {
	upper := strings.ToUpper(strings.TrimSpace(value))
	if !strings.HasPrefix(upper, "ENEMY") {
		return nil, false
	}
	index := combatParameterInt(strings.TrimPrefix(upper, "ENEMY")) - 1
	if index < 0 || index >= engine.enemyCount {
		return nil, false
	}
	return &engine.enemies[index], true
}

func enemyAIHitCount(enemy *battleEnemy, physics string) (int, bool) {
	switch strings.ToUpper(physics) {
	case "PHYSICS":
		return enemy.AITurn.Hits[0] + enemy.AITurn.Hits[2], true
	case "MAGIC":
		return enemy.AITurn.Hits[1] + enemy.AITurn.Hits[2], true
	case "ALL":
		return enemy.AITurn.Hits[0] + enemy.AITurn.Hits[1] + enemy.AITurn.Hits[2], true
	default:
		return 0, false
	}
}

func (engine *BattleEngine) anySkillKind(values []string) bool {
	for _, value := range values {
		if combatNullValue(value) {
			continue
		}
		for _, kinds := range engine.turnStats.KindsByUser {
			if kinds[strings.ToUpper(value)] > 0 {
				return true
			}
		}
	}
	return false
}

func (engine *BattleEngine) userPlayCardCountMatches(job string, attribute string, kind string, lower string, upper string) bool {
	wantedArthur := combatArthurType(job)
	candidates := make([]int, 0, maxRoomMembers)
	for memberType := 1; memberType <= maxRoomMembers; memberType++ {
		if wantedArthur != 0 && wantedArthur != engine.players[memberType-1].ArthurType || engine.players[memberType-1].HP <= 0 {
			continue
		}
		// 56510 requires an entry from 3b32c. An unsubmitted player is not
		// equivalent to an explicit zero-card PASS (which 40908 registers).
		if _, submitted := engine.selectedPlays[memberType]; !submitted {
			continue
		}
		count := 0
		for _, action := range engine.turnActions {
			if action.memberType != memberType || action.cardType == 0 || action.sphereSlot != 0 {
				continue
			}
			if !combatNullValue(attribute) {
				matches := false
				for _, component := range splitCombatAttributes(attribute) {
					matches = matches || combatAttributeMatches(action.skill.Attribute, component)
				}
				if !matches {
					continue
				}
			}
			if !combatNullValue(kind) && !strings.EqualFold(action.skill.Kind, kind) {
				continue
			}
			count++
		}
		// 56510 uses a closed interval; unlike many skill conditions,
		// upper=0 means zero cards, not an unbounded maximum.
		if count >= combatParameterInt(lower) && count <= combatParameterInt(upper) {
			candidates = append(candidates, memberType)
		}
	}
	return engine.retainEnemyTriggerTarget(candidates)
}

func (engine *BattleEngine) userParameterMatches(job string, parameter string, lower string, upper string, rank int) bool {
	wantedArthur := combatArthurType(job)
	rankValue := 0
	if rank > 0 {
		values := make([]int, 0, maxRoomMembers)
		for index := range engine.players {
			if engine.players[index].HP > 0 {
				values = append(values, combatStatValue(&engine.players[index], parameter))
			}
		}
		if len(values) == 0 {
			return false
		}
		sort.Sort(sort.Reverse(sort.IntSlice(values)))
		rankValue = values[minInt(rank-1, len(values)-1)]
	}
	candidates := make([]int, 0, maxRoomMembers)
	for memberType := 1; memberType <= maxRoomMembers; memberType++ {
		player := &engine.players[memberType-1]
		if player.HP <= 0 || wantedArthur != 0 && wantedArthur != player.ArthurType {
			continue
		}
		value := combatStatValue(player, parameter)
		if (rank <= 0 || value == rankValue) && value >= combatParameterInt(lower) && value <= combatParameterInt(upper) {
			candidates = append(candidates, memberType)
		}
	}
	return engine.retainEnemyTriggerTarget(candidates)
}

func (engine *BattleEngine) retainEnemyTriggerTarget(candidates []int) bool {
	if len(candidates) == 0 {
		return false
	}
	// 56510, 55df0 and 560d0 consume xor128 even for a single candidate.
	// Skipping that roll shifts every subsequent native RNG decision.
	selected := int(engine.rng.next() % uint32(len(candidates)))
	engine.enemyTriggerTarget = candidates[selected]
	return true
}

func (engine *BattleEngine) userDeadMatches(job string, minimum int) bool {
	wantedArthur := combatArthurType(job)
	dead := 0
	for memberType := 1; memberType <= maxRoomMembers; memberType++ {
		if engine.players[memberType-1].MemberType == 0 || wantedArthur != 0 && wantedArthur != engine.players[memberType-1].ArthurType {
			continue
		}
		if engine.players[memberType-1].HP <= 0 {
			dead++
		}
	}
	return dead > 0 && dead >= minimum
}

func (engine *BattleEngine) enemyAIStatusCondition(actor *battleEnemy, trigger string, arguments []string) bool {
	switch trigger {
	case "SKILL_ROLE_KIND_DEBUFF_NOW_TURN":
		return enemyAITurnStatusPresent(actor, arguments, false)
	case "SKILL_ROLE_KIND_DEBUFF":
		return enemyAIHasCurrentStatus(actor.Effects, false, arguments[:minInt(3, len(arguments))], false, false)
	case "SKILL_ROLE_KIND_BUFF":
		return enemyAIHasCurrentStatus(actor.Effects, true, arguments[:minInt(3, len(arguments))], false, false)
	case "SKILL_ROLE_KIND_BUFF_NOW_TURN_BY_USER":
		return enemyAITurnStatusPresent(actor, arguments, true)
	case "SKILL_ROLE_KIND_DEBUFF_BY_USER", "SKILL_ROLE_KIND_BUFF_BY_USER":
		good := trigger == "SKILL_ROLE_KIND_BUFF_BY_USER"
		for _, player := range engine.players {
			if player.HP > 0 && enemyAIHasCurrentStatus(player.Effects, good, arguments[:minInt(3, len(arguments))], false, false) {
				return true
			}
		}
		return false
	case "SKILL_ROLE_KIND_DEBUFF_BY_ENEMY", "SKILL_ROLE_KIND_DEBUFF_BY_ENEMY_AND":
		candidates := make([]int, 0, engine.enemyCount)
		for index := 0; index < engine.enemyCount; index++ {
			target := &engine.enemies[index]
			if target.HP > 0 && enemyAIHasCurrentStatus(target.Effects, false, arguments[:minInt(3, len(arguments))], false, strings.HasSuffix(trigger, "_AND")) {
				candidates = append(candidates, target.MemberType)
			}
		}
		return engine.retainEnemyTriggerTarget(candidates)
	case "SKILL_ROLE_KIND_DEBUFF_BY_ENEMY_ONE", "SKILL_ROLE_KIND_DEBUFF_BY_ENEMY_ONE_AND":
		target, exists := engine.enemyByTriggerName(arguments[0])
		if !exists {
			return false
		}
		matched := enemyAIHasCurrentStatus(target.Effects, false, arguments[1:minInt(4, len(arguments))], false, strings.HasSuffix(trigger, "_AND"))
		if matched {
			// 59e70/59a82 retain this exact target without a random roll,
			// including a KO target whose status entry is still retained.
			engine.enemyTriggerTarget = target.MemberType
		}
		return matched
	case "SKILL_ROLE_KIND_DEBUFF_BY_USER_ONE", "SKILL_ROLE_KIND_BUFF_BY_USER_ONE", "SKILL_ROLE_KIND_DEBUFF_BY_USER_ONE_AND", "SKILL_ROLE_KIND_BUFF_BY_USER_ONE_AND":
		good := strings.HasPrefix(trigger, "SKILL_ROLE_KIND_BUFF_")
		wantedArthur := combatArthurType(arguments[0])
		candidates := make([]int, 0, maxRoomMembers)
		for _, player := range engine.players {
			if player.HP <= 0 || wantedArthur != 0 && player.ArthurType != wantedArthur {
				continue
			}
			if enemyAIHasCurrentStatus(player.Effects, good, arguments[1:minInt(8, len(arguments))], true, strings.HasSuffix(trigger, "_AND")) {
				candidates = append(candidates, player.MemberType)
			}
		}
		return engine.retainEnemyTriggerTarget(candidates)
	default:
		return false
	}
}

func combatTriggerBuffKind(value string) int {
	upper := strings.ToUpper(strings.TrimSpace(value))
	switch upper {
	case "ATTR_RATE_DOWN_INVALID":
		// SKILL_ROLE_KIND_BUFF uses this CSV name; the producer is
		// ATK_OP_ATTR_RATE_DOWN_INVALID. Both describe native kind 13.
		return 13
	case "ATK_UP_BY_ATK":
		return 1
	case "ATK_UP_BY_INT":
		return 2
	case "ATK_UP_BY_MND":
		return 3
	case "DEF_UP_BY_DEF":
		return 4
	case "DEF_UP_BY_MDEF":
		return 5
	case "PARAM_LIMIT_BREAK_BY_ATK":
		return 48
	case "PARAM_LIMIT_BREAK_BY_INT":
		return 49
	case "PARAM_LIMIT_BREAK_BY_MND":
		return 50
	default:
		return skillRoleKindBuffByFunction[upper]
	}
}

func combatTriggerDebuffKind(value string) int {
	upper := strings.ToUpper(strings.TrimSpace(value))
	switch upper {
	case "ATK_BREAK_BY_ATK":
		return 1
	case "ATK_BREAK_BY_INT":
		return 2
	case "ATK_BREAK_BY_MND":
		return 3
	case "GUARD_BREAK_BY_DEF":
		return 4
	case "GUARD_BREAK_BY_MDEF":
		return 5
	default:
		return skillRoleKindDebuffByFunction[upper]
	}
}

func combatBadStatusKind(value string) int {
	return map[string]int{
		"STAN": 1, "SILENCE": 2, "CHARM": 3, "POISON": 4, "BURN": 5, "FREEZE": 6,
		"BLEED": 7, "UNDERMINE": 8, "WEAKNESS": 9, "ELECTRIC": 10, "CARD_TRAP_DOT": 11,
		"COST_BLOCK": 12, "DEAL_PENALTY": 13, "HEAL_CUT": 14,
	}[strings.ToUpper(strings.TrimSpace(value))]
}

func (engine *BattleEngine) damageEventTotalByTrigger(targetName string, attribute string, physics string, dot string) int {
	targetMember := 0
	if !combatNullValue(targetName) {
		target, exists := engine.enemyByTriggerName(targetName)
		if !exists || target.HP <= 0 {
			return 0
		}
		targetMember = target.MemberType
	}
	return engine.damageEventTotal(targetMember, attribute, physics, dot, false)
}

func (engine *BattleEngine) damageEventTotal(targetMember int, attribute string, physics string, dot string, enchantOnly bool) int {
	total := 0
	for _, event := range engine.turnStats.DamageEvents {
		if targetMember != 0 && event.Target != targetMember || enchantOnly && !event.Enchant {
			continue
		}
		if targetMember == 0 {
			enemy, exists := engine.enemyByMemberType(event.Target)
			if !exists || enemy.HP <= 0 {
				continue
			}
		}
		if !nativeAIDamageEventMatches(event, attribute, physics, dot, enchantOnly) {
			continue
		}
		total += event.Value
	}
	return total
}

func (engine *BattleEngine) enemyByMemberType(memberType int) (*battleEnemy, bool) {
	index := memberType - 5
	if index < 0 || index >= engine.enemyCount {
		return nil, false
	}
	return &engine.enemies[index], true
}

func combatNullValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed == "" || strings.EqualFold(trimmed, "NULL")
}

func combatArthurType(value string) int {
	return map[string]int{"MERCENARY": 1, "MILLIONAIRE": 2, "THIEF": 3, "SINGER": 4, "ALCHEMIST": 5}[strings.ToUpper(strings.TrimSpace(value))]
}

// Room member IDs describe join order, not Arthur professions. A missing job
// must not silently select another member in conditions or role targeting.
func (engine *BattleEngine) memberForJob(job string) int {
	arthurType := combatArthurType(job)
	if arthurType == 0 {
		return 0
	}
	for _, player := range engine.players {
		if player.MemberType != 0 && player.ArthurType == arthurType {
			return player.MemberType
		}
	}
	return 0
}

func combatHandCount(player *battlePlayer) int {
	count := 0
	for _, slot := range player.Hand {
		if slot != 0 {
			count++
		}
	}
	return count
}
