package battlecore

// NextWaveIndex follows BattleFunctionBase.calcNextBattleIndex. An ordinary
// victory skips optional AWAKE entries (type 4); awakening retains the next
// entry. Admission, scene loading and rewards remain caller responsibilities.
func NextWaveIndex(enemyTypes []int8, current, endType int) (int, bool) {
	if current < 0 || current >= len(enemyTypes) || (endType != 1 && endType != 4) {
		return 0, false
	}
	index := current + 1
	for index < len(enemyTypes) && enemyTypes[index] == 4 && endType != 4 {
		index++
	}
	return index, index < len(enemyTypes)
}

// AWAKE entries are branches of their preceding encounter. A score result can
// finish without killing any body; broken parts alone do not defeat a party.
func DefeatedEnemyParties(enemyTypes []int8, deadBits []int) bool {
	if len(enemyTypes) == 0 || len(enemyTypes) != len(deadBits) {
		return false
	}
	defeated := false
	for index, kind := range enemyTypes {
		if index > 0 && kind != 4 {
			if !defeated {
				return false
			}
			defeated = false
		}
		defeated = defeated || deadBits[index]&1 != 0
	}
	return defeated
}
