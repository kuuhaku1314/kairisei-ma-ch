package battlecore

// 82c85 -> 56f6a/574df records nonzero direct damage on its concrete target.
// Enchant uses ALL=2; 838d0 also records reflected damage in that category.
// Parent HP propagation and DOT do not call these AI producers.
func recordEnemyAIDamage(enemy *battleEnemy, damage, physics int) {
	if enemy == nil || damage <= 0 || physics < 0 || physics >= 3 {
		return
	}
	enemy.AITurn.Damage[physics] += int64(damage)
	enemy.AITurn.Hits[physics]++
	enemy.Trance.Damage += int64(damage) // 574df also calls 574af.
}

// 8bb40 -> 56f95 remembers successful applications on this enemy until
// 56dea at TurnPhase. Later removal, expiry or KO does not erase the event.
func recordEnemyAIBadStatus(enemy *battleEnemy, function string) {
	if kind := combatBadStatusKind(function); enemy != nil && kind > 0 {
		enemy.AITurn.BadStatus |= 1 << uint(kind)
	}
}

func enemyAIBadStatusPresent(enemy *battleEnemy, values []string) bool {
	for _, value := range values {
		if kind := combatBadStatusKind(value); kind > 0 && enemy.AITurn.BadStatus&(1<<uint(kind)) != 0 {
			return true
		}
	}
	return false
}

// Successful native status consumers record events, separately from the
// retained effects. 56ffe records only on the receiving enemy; 57041 fans a
// player-to-player positive status out to every enemy (6eeb6 includes KO).
func (engine *BattleEngine) recordAIStatusApplied(member int, effect battleEffect) {
	engine.recordSkillStatistics(member, effect)
	// 88a20/8d7e0 increment source+7e6c4[typed kind] after acceptance.
	// 86a00's draw-bonus consumer broadcasts AI status but does NOT update
	// BUFF_EXEC; a normal heal likewise is not a registered regeneration buff.
	if kind := branchStatusKind(effect, true); kind >= 0 && kind < skillRoleBuffKindCount && battleBuffCodes[effect.Function] != 400 {
		engine.recordExecutedBuffKind(effect.Source, kind)
	}
	if enemy, ok := engine.enemyByMemberType(member); ok {
		recordEnemyAIBadStatus(enemy, effect.Function)
		if kind := branchStatusKind(effect, false); kind >= 0 && kind < 32 {
			enemy.AITurn.DebuffKinds |= 1 << uint(kind)
			if (kind < 7 || kind > 10) && kind != 22 && kind != 25 {
				enemy.Trance.Debuff = true
			} // 56fbe
		}
		return
	}
	if member < 1 || member > maxRoomMembers || effect.Source < 1 || effect.Source > maxRoomMembers {
		return
	}
	if kind := branchStatusKind(effect, true); kind >= 0 && kind < skillRoleBuffKindCount {
		for i := 0; i < engine.enemyCount; i++ {
			enemy := &engine.enemies[i]
			if enemy.MemberType != 0 {
				enemy.AITurn.PlayerBuffKinds[kind]++
				if kind == 16 {
					enemy.Trance.Covering = true
				}
			}
		}
	}
}

// Both ordinary statuses and CARD-list modifiers publish this typed source
// event. CARD entries remain in their own program list, not the status list.
func (engine *BattleEngine) recordExecutedBuffKind(source, kind int) {
	if engine.playerMember(source) {
		engine.player(source).ExecutedBuffKinds[kind]++
	} else if actor, ok := engine.enemyByMemberType(source); ok {
		actor.ExecutedBuffKinds[kind]++
	}
}

func enemyAITurnStatusPresent(enemy *battleEnemy, values []string, playerBuff bool) bool {
	for _, value := range values[:minInt(3, len(values))] {
		if playerBuff {
			kind := combatTriggerBuffKind(value)
			if kind > 0 && kind < len(enemy.AITurn.PlayerBuffKinds) && enemy.AITurn.PlayerBuffKinds[kind] > 0 {
				return true
			}
		} else {
			kind := combatTriggerDebuffKind(value)
			if kind > 0 && kind < 32 && enemy.AITurn.DebuffKinds&(1<<uint(kind)) != 0 {
				return true
			}
		}
	}
	return false
}
