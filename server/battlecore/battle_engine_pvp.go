package battlecore

// Original JP pvp_user_attack calls 68080 after the common action and appended
// effect queue. Expiry precedes PVP judging; the selected cards are still live
// during that publication. PVP then owns one combined-turn boundary.
func (engine *BattleEngine) settlePVPUserAttack(results []BattleResult) ([]BattleResult, error) {
	var err error
	results, err = engine.appendEffectTurn(results)
	if err != nil {
		return nil, err
	}
	engine.endType = engine.pvpEndType()
	for _, action := range engine.turnActions {
		if action.cardType > 0 && action.sphereSlot == 0 {
			engine.removeCardFromHand(action.memberType, action.cardType)
		}
	}
	engine.flushTokenStock()
	engine.phase = PhaseUserAttack
	if engine.endType != 0 {
		results = append(results, battleEndResult(engine.endType))
		engine.phase = PhaseEnded
		return engine.appendWaveBuffCleanup(results), nil
	}
	return results, nil
}

// 68080 first checks living members (dec87), then the configured turn limit.
// At the limit it compares living count, integer party HP percentage, and
// cumulative received damage, in that order. DamageTaken is already the same
// receiver-owned counter; no separate PVP statistics or damage formula is used.
func (engine *BattleEngine) pvpEndType() int {
	var alive [2]int
	var hp, maximum, damage [2]int64
	for _, player := range engine.playerStates() {
		side := 0
		if player.MemberType > 4 {
			side = 1
		}
		if player.HP > 0 {
			alive[side]++
		}
		hp[side] += int64(maxInt(0, player.HP))
		maximum[side] += int64(player.MaxHP)
		damage[side] += int64(player.DamageTaken)
	}
	if alive[0] == 0 && alive[1] == 0 {
		return 3
	}
	if alive[0] == 0 {
		return 2
	}
	if alive[1] == 0 {
		return 1
	}
	if engine.turn < engine.endTurn {
		return 0
	}
	if alive[0] > alive[1] {
		return 1
	}
	if alive[0] < alive[1] {
		return 2
	}
	left, right := hp[0]*100/maximum[0], hp[1]*100/maximum[1]
	if left > right {
		return 1
	}
	if left < right {
		return 2
	}
	if damage[0] < damage[1] {
		return 1
	}
	if damage[0] > damage[1] {
		return 2
	}
	return 3
}

func (engine *BattleEngine) recordPVPActionAttribute(member int, attribute string) {
	player := engine.player(member)
	for _, part := range splitCombatAttributes(attribute) {
		if code := combatAttributeCode(part); code > 0 && code < len(player.AttributeUses) {
			player.AttributeUses[code]++
		}
	}
}

// JP b602c uses the most frequent executed component, retaining the lower
// enum on ties. PASS has no count and publishes no32. KO members retain their
// counters until a later living turn, rather than being reset with turn stats.
func (engine *BattleEngine) advancePVPAttributes() []BattleResult {
	var rows []BattleResult
	for _, p := range engine.playerStates() {
		if p.HP <= 0 {
			continue
		}
		maximum, chosen := uint32(0), 0
		for code, count := range p.AttributeUses {
			if count > maximum {
				maximum, chosen = count, code
			}
		}
		p.AttributeUses = [10]uint32{}
		if maximum == 0 {
			continue
		}
		attribute := []string{"NULL", "FIRE", "ICE", "WIND", "LIGHT", "DARK", "EARTH", "THUNDER", "WATER", "NEUTRAL"}[chosen]
		p.BaseAttribute = attribute
		refreshPlayerAttribute(p)
		rows = append(rows, rewriteResult(p.MemberType, attribute))
	}
	return rows
}
