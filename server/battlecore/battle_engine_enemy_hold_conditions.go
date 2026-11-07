package battlecore

import "strings"

// 70aec/70b74 keep at most thirteen CALL attempts per holder and append kind.
// 5ed94 clears them at TurnPhase. They survive one-shot removal and are
// separate from successful buff counters and currently registered holds.
type battleAppendExecution struct {
	Attribute string
	Kind      string
}

func (player *battlePlayer) recordAppendExecution(hold battleBlessHold) {
	index := hold.CardType - 21
	if index < 0 || index >= len(player.AppendExecutions) {
		return
	}
	count := player.AppendExecutionCounts[index]
	if count < len(player.AppendExecutions[index]) {
		player.AppendExecutions[index][count] = battleAppendExecution{hold.Skill.Attribute, hold.Skill.Kind}
		player.AppendExecutionCounts[index]++
	}
}

func (player *battlePlayer) hasAppendExecution(cardType int, attribute, kind string) bool {
	index := cardType - 21
	for _, entry := range player.AppendExecutions[index][:player.AppendExecutionCounts[index]] {
		if !combatNullValue(kind) && !strings.EqualFold(entry.Kind, kind) {
			continue
		}
		if combatNullValue(attribute) {
			return true
		}
		// The native record contains component attribute bits; a dual-attribute
		// enum is not a request for either component in this predicate.
		for _, component := range splitCombatAttributes(entry.Attribute) {
			if strings.EqualFold(component, attribute) && !combatNullValue(component) {
				return true
			}
		}
	}
	return false
}

func (engine *BattleEngine) enemyAIHoldStateCondition(trigger string, params []string) bool {
	candidates := make([]int, 0, maxRoomMembers)
	for i := range engine.players {
		player := &engine.players[i]
		if player.MemberType == 0 || player.HP <= 0 {
			continue
		}
		matched := false
		switch trigger {
		case "USER_BLESS", "USER_CURSE":
			cardType := 21
			if trigger == "USER_BLESS" {
				cardType = 22
			}
			matched = combatValueInRange(playerAppendCardCount(player, cardType, params[0]), params[1], params[2])
		case "USER_EXEC_BLESS", "USER_EXEC_CURSE":
			cardType := 21
			if trigger == "USER_EXEC_BLESS" {
				cardType = 22
			}
			matched = player.hasAppendExecution(cardType, params[0], params[1])
		default:
			if job := combatArthurType(params[0]); job != 0 && player.ArthurType != job {
				continue
			}
			switch trigger {
			case "USER_BURST_GAUGE_VALUE":
				matched = combatValueInRange(player.Burst, params[1], params[2])
			case "USER_BURST_GAUGE_STATE":
				state, exists := burstGaugeStateCode(params[1])
				matched = exists && player.BurstState == state
			case "USER_CARD_BUFF":
				// 5490e passes member+4de80 (list7); native consumes six kinds,
				// even though the CSV parser accepts seven after Arthur type.
				matched = engine.playerHasCardBuff(player, params[1:7])
			case "USER_BURST_BUFF", "USER_BURST_BUFF_AND":
				lastList := 5
				if combatParameterInt(params[1]) != 0 {
					lastList = 6
				}
				matched = enemyAIHasStatusInLists(player.Effects, true, params[2:8], 5, lastList, trigger == "USER_BURST_BUFF_AND")
			}
		}
		if matched {
			candidates = append(candidates, player.MemberType)
		}
	}
	return engine.retainEnemyTriggerTarget(candidates)
}

func (engine *BattleEngine) playerHasCardBuff(player *battlePlayer, kinds []string) bool {
	return engine.playerCardHasBuff(player, kinds, 0)
}

func (engine *BattleEngine) playerCardHasBuff(player *battlePlayer, kinds []string, cardType int) bool {
	for _, effect := range player.Effects {
		if effect.ListType != 7 || cardType != 0 && effect.CardType != cardType {
			continue
		}
		kind := branchStatusKind(effect, true)
		for _, wanted := range kinds {
			if kind >= 0 && kind == combatTriggerBuffKind(wanted) {
				return true
			}
		}
	}
	// Registered hand modifiers have their own existing storage. Read that
	// same lifetime here rather than creating a second durable status copy.
	for slot, entries := range player.CardBurstSkills {
		if cardType != 0 && slot != cardType {
			continue
		}
		for _, entry := range entries {
			for _, role := range engine.cardModifierRoles(entry) {
				if !activeCardModifier(role, entry.Age) {
					continue
				}
				kind := skillRoleKindBuff(battleEffect{Function: role.Function})
				for _, wanted := range kinds {
					if kind >= 0 && kind == combatTriggerBuffKind(wanted) {
						return true
					}
				}
			}
		}
	}
	return false
}
