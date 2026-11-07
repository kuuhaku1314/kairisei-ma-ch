package battlecore

import "strings"

// 57590 uses the master override unless it is 100, which selects the native
// elemental matrix (DAT_000f6544). An explicit zero becomes 100 AFTER that
// lookup: zero is neutral damage, not immunity and not the matrix sentinel.
// Dual-attribute attacks select the greater multiplier, retaining the first
// component on ties. 927b0/93461 use that same selected component for fixed
// armor and ResultCmd60, not the combined enum or both fixed armor values.
func enemyBaseAttackAttribute(enemy *battleEnemy, attribute string) (int, string) {
	parts := splitCombatAttributes(attribute)
	if len(parts) == 0 {
		return 100, "NULL"
	}
	best, selected := 0, "NULL"
	for _, part := range parts {
		rate := enemy.Level.AttributeRates[combatAttributeIndex(part)]
		if rate == 100 {
			rate = nativeElementalRate(part, enemy.Attribute)
		}
		if rate == 0 {
			rate = 100
		}
		if rate > best {
			best, selected = rate, part
		}
	}
	return best, selected
}

func enemyAttributeRate(enemy *battleEnemy, attribute string) int {
	rate, _ := enemyBaseAttackAttribute(enemy, attribute)
	return rate
}

func nativeElementalRate(attack, defense string) int {
	switch strings.ToUpper(attack) + ":" + strings.ToUpper(defense) {
	case "FIRE:WIND", "ICE:FIRE", "WIND:ICE", "LIGHT:DARK", "DARK:LIGHT":
		return 200
	case "FIRE:ICE", "ICE:WIND", "WIND:FIRE":
		return 50
	default:
		return 100
	}
}

func enemyFixedAttributeDefense(enemy *battleEnemy, attribute string) int {
	// 56b6d copies enemy.csv 15..19; 57661 reads the selected attribute's
	// fixed amount. EARTH/THUNDER/WATER/NEUTRAL have zero-initialized slots.
	index := combatAttributeIndex(attribute)
	if index < 0 || index >= len(enemy.AttributeFixed) {
		return 0
	}
	return enemy.AttributeFixed[index]
}

// The player receiver is shared by ordinary enemies and opposing users.
// Only PVP uses the user's active attribute for the elemental matrix.
func (engine *BattleEngine) playerAttackAttribute(player *battlePlayer, attribute string) (int, string) {
	if !engine.pvp {
		return 100, attribute
	}
	best, selected := 0, "NULL"
	for _, part := range splitCombatAttributes(attribute) {
		rate := nativeElementalRate(part, player.Attribute)
		if rate > best {
			best, selected = rate, part
		}
	}
	if best == 0 {
		return 100, attribute
	}
	return best, selected
}
