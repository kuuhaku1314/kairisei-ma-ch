package battlecore

import "strings"

// branchHasStatus mirrors d403c/d4288 and d5a4a/d5ed6. Conditions compare
// SKILL_ROLE_KIND, not producer names. Presence in the selected native lists
// is authoritative; remaining turns and the member's HP are not target gates.
func branchHasStatus(effects []battleEffect, good bool, parameters [5]string) bool {
	wanted := [2]int{combatTriggerDebuffKind(parameters[0]), combatTriggerDebuffKind(parameters[1])}
	if good {
		wanted = [2]int{combatTriggerBuffKind(parameters[0]), combatTriggerBuffKind(parameters[1])}
	}
	for _, effect := range effects {
		if effect.ListType != 0 && (!good || effect.ListType != 5 && effect.ListType != 6) {
			continue
		}
		kind := branchStatusKind(effect, good)
		if kind >= 0 && (kind == wanted[0] || kind == wanted[1]) {
			return true
		}
	}
	return false
}

// Parameter states are classified by their retained signed delta. A known
// parameter buff with no change has native kind NULL (0), not an unknown kind
// (-1). Keep this separate from role metadata classification used by producers.
func branchStatusKind(effect battleEffect, good bool) int {
	code, exists := battleBuffCodes[effect.Function]
	if !exists {
		return -1
	}
	if good {
		if !(code < 100 || code >= 200 && code < 300 || code == 400 || code == 409 || code == 412) {
			return -1
		}
	} else if !(code >= 100 && code < 200 || code >= 300 && code < 400 || code == 408) {
		return -1
	}
	parameter := strings.ToUpper(effect.Parameter)
	switch code {
	case 0, 1, 8, 17, 34, 100, 101, 109, 115:
		if good && effect.Delta <= 0 || !good && effect.Delta >= 0 {
			return 0
		}
		switch parameter {
		case "ATK":
			return 1
		case "INT":
			return 2
		case "MND":
			return 3
		case "MAX_HP":
			if good {
				return 32
			}
		}
		return 0
	case 4, 5, 9, 18, 102, 103, 104, 110:
		if good && effect.Delta <= 0 || !good && effect.Delta >= 0 {
			return 0
		}
		if parameter == "DEF" {
			return 4
		}
		if parameter == "MDEF" {
			return 5
		}
		return 0
	case 32, 33:
		switch parameter {
		case "ATK":
			return 48
		case "INT":
			return 49
		case "MND":
			return 50
		}
		return 0
	}
	kind := skillRoleKindDebuff(effect)
	if good {
		kind = skillRoleKindBuff(effect)
	}
	if kind == 0 {
		return -1
	}
	return kind
}

func (engine *BattleEngine) branchSideHasStatus(actor int, users bool, good bool, parameters [5]string) bool {
	// d4a46/d1caa/d688e use 6f45d to omit HP<=0. In this PvE profile
	// USER_SIDE always means players, including when the caster is an enemy.

	if engine.pvp {
		party := engine.opposingParty(actor)
		if users {
			party = engine.playerParty(actor)
		}
		for _, p := range party {
			if p.HP > 0 && branchHasStatus(p.Effects, good, parameters) {
				return true
			}
		}
		return false
	}
	if users {
		for index := range engine.players {
			player := &engine.players[index]
			if player.HP > 0 && branchHasStatus(player.Effects, good, parameters) {
				return true
			}
		}
	} else {
		for index := 0; index < engine.enemyCount; index++ {
			enemy := &engine.enemies[index]
			if enemy.HP > 0 && branchHasStatus(enemy.Effects, good, parameters) {
				return true
			}
		}
	}
	return false
}

func (engine *BattleEngine) branchTargetAttributeMatches(target int, parameters [5]string) bool {
	// d3fb1 rejects a null target, even when the skill targets a whole side.
	attribute := ""
	if engine.playerMember(target) {
		attribute = engine.player(target).Attribute
	} else if target >= 5 && target < 5+engine.enemyCount {
		attribute = engine.enemies[target-5].Attribute
	}
	for _, wanted := range parameters[:2] {
		for _, component := range splitCombatAttributes(wanted) {
			if combatAttributeMatches(attribute, component) {
				return true
			}
		}
	}
	return false
}

// d6374/d4768 count (typed kind, primary attribute) pairs in NORMAL only.
// cf940 excludes native fields and non-counting control states; a recognized
// zero-delta parameter state retains its NULL kind rather than disappearing.
func branchStatusKindCount(effects []battleEffect, good bool) int {
	kinds := make(map[[2]int]struct{})
	for _, effect := range effects {
		code, exists := battleBuffCodes[effect.Function]
		if effect.ListType != 0 || !exists {
			continue
		}
		eligible := code >= 100 && code <= 115 && code != 107 || code >= 300 && code <= 312 || code == 408
		if good {
			eligible = code >= 0 && code <= 10 || code == 19 || code == 20 || code >= 22 && code <= 34 ||
				code >= 38 && code <= 44 || code == 200 || code == 201 || code == 203 || code >= 205 && code <= 216
		}
		if !eligible {
			continue
		}
		kind := branchStatusKind(effect, good)
		if kind < 0 {
			continue
		}
		attribute := 0
		if components := splitCombatAttributes(effect.Attribute); len(components) > 0 {
			attribute = combatAttributeCode(components[0])
		}
		kinds[[2]int{kind, attribute}] = struct{}{}
	}
	return len(kinds)
}

// d5590/d44ea compare the producer's native side, excluding the actor itself.
// Good status scans NORMAL/BURST/BURST_PASSIVE, bad status only NORMAL.
func branchHasSourcedStatus(effects []battleEffect, member int, good bool, parameters [5]string) bool {
	actorEnemy := member >= 5 && member <= 8
	for _, effect := range effects {
		sourceEnemy := effect.Source >= 5 && effect.Source <= 8
		if effect.Source == member || (sourceEnemy == actorEnemy) != good ||
			effect.ListType != 0 && (!good || effect.ListType != 5 && effect.ListType != 6) {
			continue
		}
		kind := branchStatusKind(effect, good)
		for _, value := range parameters[:2] {
			wanted := combatTriggerDebuffKind(value)
			if good {
				wanted = combatTriggerBuffKind(value)
			}
			if kind >= 0 && kind == wanted {
				return true
			}
		}
	}
	return false
}
