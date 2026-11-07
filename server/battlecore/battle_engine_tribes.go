package battlecore

// Membership is a boolean query, not a count: duplicate native tribe slots
// never multiply a branch or boost. Explicit zero slots remain meaningful to
// TARGET_TRIBAL/ENEMY_TRIBAL; the boost's zero wildcard is handled by its caller.
func enemyHasTribe(enemy *battleEnemy, tribe int) bool {
	if len(enemy.TribeIDs) == 0 {
		return tribe == 0
	}
	for _, current := range enemy.TribeIDs {
		if current == tribe {
			return true
		}
	}
	return false
}
