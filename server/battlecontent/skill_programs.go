package battlecontent

import (
	"fmt"
	"strconv"
	"strings"

	"kairisei.local/server/battlemodel"
)

// SkillDataContainer reads the common header only for the first row of an ID.
// Later rows contribute the display role, conditions, priority and role-set ID.
func AppendSkillVariant(skills map[int][]battlemodel.CombatSkillDefinition, variant battlemodel.CombatSkillDefinition) {
	variants := skills[variant.ID]
	if len(variants) != 0 {
		branch := variant
		variant = variants[0]
		variant.DisplayRole, variant.FunctionID = branch.DisplayRole, branch.FunctionID
		variant.BranchCondition, variant.BranchParameters = branch.BranchCondition, branch.BranchParameters
		variant.BranchCondition2, variant.BranchParameters2 = branch.BranchCondition2, branch.BranchParameters2
		variant.BranchPriority = branch.BranchPriority
	}
	skills[variant.ID] = append(variants, variant)
}

// BindSkillRole projects the same rule flags and signed Int32 VALUE parameters
// as SkillDataContainer.ParseRole. Symbolic selectors retain their names,
// including the distinction between an empty ATTR/SKILL_KIND and explicit NULL.
func BindSkillRole(role battlemodel.CombatSkillRole, rules map[string]battlemodel.CombatRoleParameterRule) (battlemodel.CombatSkillRole, error) {
	rule, exists := rules[role.Function]
	if !exists {
		return role, fmt.Errorf("missing skill role parameter rule %s", role.Function)
	}
	role.PVPRate, role.ValueBoostUse = rule.PVPRate, rule.ValueBoostUse
	for i, kind := range rule.Parameters {
		switch kind {
		case "", "NULL":
			role.Parameters[i] = ""
		case "VALUE":
			text := strings.TrimSpace(role.Parameters[i])
			if text == "ALL" {
				role.Parameters[i] = "-1"
			} else {
				value, err := strconv.ParseInt(text, 10, 32)
				// Int32.TryParse returns the supplied default on invalid/overflow.
				if err != nil {
					value = 0
				}
				role.Parameters[i] = strconv.FormatInt(value, 10)
			}
		}
	}
	return role, nil
}
