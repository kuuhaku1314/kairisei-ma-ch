//go:build workshop

package multiplayer

import "kairisei.local/server/battlecore"

func ArthurJob(arthurType int) string { return battlecore.ArthurJob(arthurType) }

func SupportsSupportFunction(function string) bool {
	return battlecore.SupportsSupportFunction(function)
}

func SupportsPlayerFunction(function string) bool { return battlecore.SupportsPlayerFunction(function) }

func PreviewEnemyPlans(catalog *CombatCatalog, partyID int, seed uint32, snapshots []battlecore.EnemyPlanSnapshot) ([]battlecore.EnemyTurnPlan, error) {
	return battlecore.PreviewEnemyPlans(catalog, partyID, seed, snapshots)
}

func ValidateEnemyPassiveDefinitions(catalog *CombatCatalog) error {
	return battlecore.ValidateEnemyPassiveDefinitions(catalog)
}

func SupportsMainCardPassiveFunction(function string) bool {
	return battlecore.SupportsMainCardPassiveFunction(function)
}

func EnemySkillLevel(function string) int { return battlecore.EnemySkillLevel(function) }

func SupportsPlayerBranchCondition(condition string) bool {
	return battlecore.SupportsPlayerBranchCondition(condition)
}

func SupportsEnemyBranchCondition(condition string) bool {
	return battlecore.SupportsEnemyBranchCondition(condition)
}

func FunctionNeedsBuffCode(function string) bool { return battlecore.FunctionNeedsBuffCode(function) }

func SupportsEnemyFunction(function string) bool { return battlecore.SupportsEnemyFunction(function) }

func ValidEnemyAIOrderFields(fields []string) bool { return battlecore.ValidEnemyAIOrderFields(fields) }

func SupportsEnemyAITrigger(trigger string) bool { return battlecore.SupportsEnemyAITrigger(trigger) }

func SupportsEnemyActionTarget(kind string) bool { return battlecore.SupportsEnemyActionTarget(kind) }

func SupportsAppendCondition(condition string) bool {
	return battlecore.SupportsAppendCondition(condition)
}

// Existing server-facing names delegate to the single shared core.
// No battle implementation or mutable state is duplicated here.
func ValidateAppendSkillDefinition(source string, skill CombatSkillDefinition) error {
	return battlecore.ValidateAppendSkillDefinition(source, skill)
}
