package battlecore

import "fmt"

// These ordinary-enemy producers consume the native managed level one.
// Source-specific row counts, references and hashes are not engine invariants.
func validateEnemyZeroGrowthLevelContracts(_ *CombatCatalog) error {
	for _, function := range []string{"PARAM_LIMIT_BREAK_FIXED", "HEAL_BY_SELF_PARAM", "DOT_VALUE_UP", "BURST_GAUGE_QUICK_UP"} {
		if EnemySkillLevel(function) != 1 {
			return fmt.Errorf("native ordinary enemy %s managed level is not calibrated to one", function)
		}
	}
	return nil
}
