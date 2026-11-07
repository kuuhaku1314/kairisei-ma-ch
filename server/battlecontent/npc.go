package battlecontent

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"kairisei.local/server/battlemodel"
)

const NPCTable = "npc_ai.csv"
const NPCParameterTable = "npc_ai_situation_param_rule.csv"

// NPC tables are one content input. Registered clients may instead supply the
// original typed rule records through preparation; manual battles need neither.
func applyNPCTables(definitions *battlemodel.Definitions, tables map[string]string) error {
	text, hasNPC := tables[NPCTable]
	parameters, hasParameters := tables[NPCParameterTable]
	if !hasNPC && !hasParameters {
		return nil
	}
	if !hasNPC || !hasParameters || len(text) == 0 || len(parameters) == 0 || len(text) > maxCombatMasterBytes || len(parameters) > maxCombatMasterBytes {
		return fmt.Errorf("NPC content requires both bounded rule and parameter tables")
	}
	program, err := ParseNPCProgram(text, parameters)
	if err != nil {
		return err
	}
	definitions.NPC = program
	return nil
}

// ParseNPCProgram preserves the original six named priority columns. Parameter
// families determine whether a token is a signed int64 or a canonical enum name;
// packed enum numbers and original DTO offsets stay in the native adapter.
func ParseNPCProgram(text, parameters string) (battlemodel.NPCProgram, error) {
	var result battlemodel.NPCProgram
	families := make(map[string][5]string)
	if err := readCombatData(strings.NewReader(parameters), false, func(row []string) error {
		if len(row) < 6 {
			return fmt.Errorf("NPC situation %s has incomplete parameter columns", row[0])
		}
		if _, duplicate := families[row[0]]; duplicate {
			return fmt.Errorf("duplicate NPC situation %s", row[0])
		}
		var types [5]string
		copy(types[:], row[1:6])
		families[row[0]] = types
		return nil
	}); err != nil {
		return result, fmt.Errorf("load NPC parameter rules: %w", err)
	}
	enemyTypes := map[string]int{"MOB": 0, "BOSS": 1, "RARE_BOSS": 2, "USER": 3, "AWAKE": 4}
	seen := make(map[int]bool)
	err := readCombatData(strings.NewReader(text), true, func(row []string) error {
		if len(row) < 15 {
			return fmt.Errorf("NPC rule has fewer than six priority columns")
		}
		id, err := strconv.Atoi(row[0])
		if err != nil || id <= 0 || seen[id] {
			return fmt.Errorf("invalid or duplicate NPC rule ID %s", row[0])
		}
		seen[id] = true
		enemyType, exists := enemyTypes[row[1]]
		if !exists {
			return fmt.Errorf("NPC rule %d has unknown enemy type %s", id, row[1])
		}
		types, exists := families[row[2]]
		if !exists {
			return fmt.Errorf("NPC rule %d has unknown situation %s", id, row[2])
		}
		rule := battlemodel.NPCRule{ID: id, EnemyType: enemyType, Situation: row[2], Selector: row[8]}
		if rule.Selector == "" {
			rule.Selector = "NULL"
		}
		for i, family := range types {
			value := row[3+i]
			switch family {
			case "", "NULL":
				value = "0"
			case "VALUE":
				if value == "" {
					value = "0"
				}
				number, err := strconv.ParseInt(value, 10, 64)
				if err != nil {
					return fmt.Errorf("NPC rule %d parameter %d is not an int64", id, i)
				}
				value = strconv.FormatInt(number, 10)
			case "SKILL_PHYSICS_TYPE", "SKILL_ROLE_KIND_BUFF", "SKILL_ROLE_KIND_DEBUFF":
				if value == "" {
					value = "NULL"
				}
			default:
				return fmt.Errorf("NPC rule %d has unknown parameter family %s", id, family)
			}
			rule.Parameters[i] = value
		}
		for i := range rule.Priority {
			number, err := strconv.ParseInt(row[9+i], 10, 32)
			if err != nil {
				return fmt.Errorf("NPC rule %d priority %d is not an int32", id, i)
			}
			rule.Priority[i] = int(number)
		}
		result.Rules = append(result.Rules, rule)
		return nil
	})
	if err != nil {
		return battlemodel.NPCProgram{}, fmt.Errorf("load NPC rules: %w", err)
	}
	sort.SliceStable(result.Rules, func(i, j int) bool { return result.Rules[i].ID < result.Rules[j].ID })
	return result, nil
}
