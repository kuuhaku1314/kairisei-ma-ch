package battlecontent

import (
	"fmt"
	"strconv"

	"kairisei.local/server/battlemodel"
)

// ParseEnemy reads the unified enemy table, including all three tribe slots
// and the gauge state. It does not project through an older column layout.
func ParseEnemy(row []string) (battlemodel.CombatEnemyDefinition, error) {
	if len(row) < 32 {
		return battlemodel.CombatEnemyDefinition{}, fmt.Errorf("enemy row is too short")
	}
	id, err := RequiredInt(row, 0, "enemy ID")
	if err != nil {
		return battlemodel.CombatEnemyDefinition{}, err
	}
	tribes := make([]int, 3)
	for i := range tribes {
		text := Field(row, 1+i)
		if text == "" {
			continue
		}
		number, err := strconv.ParseInt(text, 10, 32)
		if err != nil || number < 0 {
			return battlemodel.CombatEnemyDefinition{}, fmt.Errorf("enemy %d has an invalid tribe slot", id)
		}
		tribes[i] = int(number)
	}
	value := battlemodel.CombatEnemyDefinition{
		ID: id, TribeIDs: tribes, ModelID: Field(row, 4),
		ImageID: OptionalInt(row, 6), Live2DName: Field(row, 7), Name: Field(row, 8),
		Attribute: Field(row, 9), HP: OptionalInt(row, 10), Attack: OptionalInt(row, 11),
		Magic: OptionalInt(row, 12), Recovery: OptionalInt(row, 13),
		Defense: OptionalInt(row, 14), MagicDefense: OptionalInt(row, 15),
		DamageReduction: OptionalInt(row, 16), Size: Field(row, 29),
		AttributeFixed: [5]int{OptionalInt(row, 17), OptionalInt(row, 18), OptionalInt(row, 19), OptionalInt(row, 20), OptionalInt(row, 21)},
		TranceLimit:    OptionalInt(row, 25), OverheatLimit: OptionalInt(row, 26),
		OverheatTurns: OptionalInt(row, 27), OverheatResist: OptionalInt(row, 28),
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
