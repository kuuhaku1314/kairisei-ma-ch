package battlecontent

import (
	"fmt"
	"strings"

	"kairisei.local/server/battlemodel"
)

const TowerEnemyAITable = "tower_enemy_ai_order.csv"

// The original container loads ordinary AI followed by tower AI into one
// dictionary. Both tables keep their own source identity and share row parsing.
func registerEnemyAIOrder(definitions *battlemodel.Definitions, row []string) error {
	id, err := RequiredInt(row, 0, "enemy AI ID")
	if err != nil {
		return err
	}
	if _, exists := definitions.EnemyAIOrders[id]; exists {
		return fmt.Errorf("duplicate enemy AI ID %d", id)
	}
	fields := make([]string, max(38, len(row)))
	copy(fields, row)
	definitions.EnemyAIOrders[id] = battlemodel.CombatEnemyAIOrder{ID: id, Fields: fields}
	return nil
}

func applyTowerEnemyAITable(definitions *battlemodel.Definitions, tables map[string]string) error {
	text, exists := tables[TowerEnemyAITable]
	if !exists {
		return nil
	}
	if len(text) == 0 || len(text) > maxCombatMasterBytes {
		return fmt.Errorf("combat table %s is outside the size boundary", TowerEnemyAITable)
	}
	if err := readCombatData(strings.NewReader(text), true, func(row []string) error {
		return registerEnemyAIOrder(definitions, row)
	}); err != nil {
		return fmt.Errorf("load %s: %w", TowerEnemyAITable, err)
	}
	return nil
}
