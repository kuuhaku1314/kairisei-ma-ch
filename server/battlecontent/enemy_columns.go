package battlecontent

import (
	"fmt"
	"strconv"

	"kairisei.local/server/battlemodel"
)

// JP EnemyDataContainer.ParseEnemyCsvData reads three tribe slots followed by
// the common fields, reaction ID, initial state/rate, and the existing limits.
// Old published packages retain their explicit legacy reader. Modern content
// no longer stores memberships as a TRIBES_V1 suffix.
func parseModernEnemy(row []string) (battlemodel.CombatEnemyDefinition, error) {
	legacy := make([]string, 31)
	legacy[0], legacy[1] = row[0], row[1]
	copy(legacy[2:20], row[4:22])
	copy(legacy[20:27], row[25:32])
	legacy[28], legacy[29], legacy[30] = "TRIBES_V1", Field(row, 2), Field(row, 3)
	// Empty native CSV slots are zero; the former explicit suffix required a
	// decimal value. Normalize only these two slots at that adapter boundary.
	for _, index := range []int{29, 30} {
		if legacy[index] == "" {
			legacy[index] = "0"
		}
	}
	value, err := parseLegacyEnemy(legacy)
	if err != nil {
		return value, err
	}
	for _, field := range []struct {
		column int
		target *int
	}{{22, &value.GaugeReactionID}, {24, &value.InitialGaugeRate}} {
		text := Field(row, field.column)
		if text == "" {
			continue
		}
		number, err := strconv.ParseInt(text, 10, 32)
		if err != nil {
			return battlemodel.CombatEnemyDefinition{}, fmt.Errorf("enemy %d has an invalid gauge field %d", value.ID, field.column)
		}
		*field.target = int(number)
	}
	state := Field(row, 23)
	if state == "" {
		state = "NONE"
	}
	var known bool
	value.InitialGaugeState, known = enemyGaugeState(state)
	if !known || value.GaugeReactionID < 0 {
		return battlemodel.CombatEnemyDefinition{}, fmt.Errorf("enemy %d has invalid gauge identity/state", value.ID)
	}
	return value, nil
}
