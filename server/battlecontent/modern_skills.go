package battlecontent

import (
	"fmt"
	"strings"

	"kairisei.local/server/battlemodel"
)

// The modern client loads these four original JP tables as one definition
// group. They extend the existing player/enemy identities without adding a
// rule implementation. Legacy file sets omit this entire optional group.
var modernSkillTables = []string{"skill_player_alchemist.csv", "skill_role_player_alchemist.csv", "tower_skill_enemy.csv", "tower_skill_role_enemy.csv"}

func applyAdditionalSkillTables(catalog *battlemodel.Definitions, tables map[string]string) error {
	names := modernSkillTables
	count := 0
	for _, name := range names {
		if _, exists := tables[name]; exists {
			count++
		}
	}
	if count == 0 {
		return nil
	}
	if count != len(names) {
		return fmt.Errorf("modern skill definition group is incomplete")
	}
	for index, name := range names {
		text := tables[name]
		if len(text) == 0 || len(text) > maxCombatMasterBytes {
			return fmt.Errorf("modern skill table %s is outside the size boundary", name)
		}
		if err := readCombatData(strings.NewReader(text), true, func(row []string) error {
			if index%2 == 0 {
				value, err := ParseSkill(row)
				if err != nil {
					return err
				}
				AppendSkillVariant(catalog.Skills, value)
				return nil
			}
			value, err := ParseSkillRole(row)
			if err != nil {
				return err
			}
			value.RoleIndex = len(catalog.SkillRoles[value.SkillID])
			catalog.SkillRoles[value.SkillID] = append(catalog.SkillRoles[value.SkillID], value)
			return nil
		}); err != nil {
			return fmt.Errorf("load %s: %w", name, err)
		}
	}
	return nil
}
