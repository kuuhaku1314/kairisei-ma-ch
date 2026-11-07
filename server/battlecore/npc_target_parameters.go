package battlecore

// NPC target ranking reads the actual combatant type; opponent users remain
// players and never become substitute monster definitions or encounter rows.
func (engine *BattleEngine) npcAttributeRate(member int, attribute string) int {
	if !engine.playerMember(member) {
		return enemyAttributeRate(&engine.enemies[member-5], attribute)
	}
	if !engine.pvp {
		return 100
	}
	best := 0
	for _, part := range splitCombatAttributes(attribute) {
		best = maxInt(best, nativeElementalRate(part, engine.player(member).Attribute))
	}
	return best
}
func (engine *BattleEngine) npcFixedAttributeDefense(member int, attribute string) int {
	if engine.playerMember(member) {
		return 0
	}
	return enemyFixedAttributeDefense(&engine.enemies[member-5], attribute)
}
