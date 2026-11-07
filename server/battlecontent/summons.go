package battlecontent

import (
	"fmt"
	"strconv"
	"strings"

	"kairisei.local/server/battlemodel"
)

const SummonsTable = "summons.csv"

// ParseSummon shares the original client's interleaved initial/maximum layout.
// No level interpolation, inventory admission or skill execution belongs here.
func ParseSummon(row []string) (battlemodel.CombatSummonDefinition, error) {
	if len(row) == 17 && row[16] == "" {
		row = row[:16]
	}
	if len(row) != 16 {
		return battlemodel.CombatSummonDefinition{}, fmt.Errorf("summon row has %d columns, want 16", len(row))
	}
	id, err := RequiredInt(row, 0, "summon ID")
	if err != nil || id <= 0 {
		return battlemodel.CombatSummonDefinition{}, fmt.Errorf("invalid summon ID %q", row[0])
	}
	result := battlemodel.CombatSummonDefinition{ID: id, Name: row[1]}
	for index := range result.Initial {
		for boundary := range 2 {
			value, err := strconv.ParseInt(row[2+index*2+boundary], 10, 32)
			if err != nil {
				return battlemodel.CombatSummonDefinition{}, fmt.Errorf("summon %d parameter column %d: %w", id, 2+index*2+boundary, err)
			}
			if boundary == 0 {
				result.Initial[index] = int32(value)
			} else {
				result.Maximum[index] = int32(value)
			}
		}
	}
	switch row[14] {
	case "NONE", "0":
		result.Ability = 0
	case "QUICK", "1":
		result.Ability = 1
	default:
		return battlemodel.CombatSummonDefinition{}, fmt.Errorf("summon %d has unknown ability %q", id, row[14])
	}
	result.UseSkillID, err = RequiredInt(row, 15, "summon skill ID")
	if err != nil || result.UseSkillID < 0 {
		return battlemodel.CombatSummonDefinition{}, fmt.Errorf("summon %d has invalid skill ID %q", id, row[15])
	}
	return result, nil
}

func applySummonTable(catalog *battlemodel.Definitions, tables map[string]string) error {
	text, exists := tables[SummonsTable]
	if !exists {
		return nil
	}
	if len(text) == 0 || len(text) > maxCombatMasterBytes {
		return fmt.Errorf("summon table is outside the size boundary")
	}
	values := make(map[int]battlemodel.CombatSummonDefinition)
	if err := readCombatData(strings.NewReader(text), true, func(row []string) error {
		value, err := ParseSummon(row)
		if err != nil {
			return err
		}
		if _, duplicate := values[value.ID]; duplicate {
			return fmt.Errorf("duplicate summon ID %d", value.ID)
		}
		values[value.ID] = value
		return nil
	}); err != nil {
		return fmt.Errorf("read summon definitions: %w", err)
	}
	if len(values) == 0 {
		return fmt.Errorf("summon table has no definitions")
	}
	catalog.Summons = values
	return nil
}
