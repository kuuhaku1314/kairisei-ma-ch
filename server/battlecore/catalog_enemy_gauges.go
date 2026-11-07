package battlecore

import "fmt"

func (c *CombatCatalog) validateEnemyGauges() error {
	for id, enemy := range c.Enemies {
		level, exists := c.EnemyLevels[id]
		if enemy.ReactiveGauge == nil {
			for _, action := range level.GaugeActions {
				if action.SkillID != 0 {
					return fmt.Errorf("enemy %d has a state skill without a reactive gauge", id)
				}
			}
			continue
		}
		if !exists {
			return fmt.Errorf("enemy %d gauge has no level definition", id)
		}
		if err := validateEnemyGauge(enemy.ReactiveGauge); err != nil {
			return fmt.Errorf("enemy %d: %w", id, err)
		}
		for state, action := range level.GaugeActions {
			if action.SkillID == 0 {
				continue
			}
			if !SupportsEnemyActionTarget(action.Target) {
				return fmt.Errorf("enemy %d state %d has unsupported target %q", id, state+1, action.Target)
			}
			variants := c.Skills[action.SkillID]
			if len(variants) == 0 {
				return fmt.Errorf("enemy %d state %d references missing skill %d", id, state+1, action.SkillID)
			}
			for _, skill := range variants {
				roles := c.SkillRoles[skill.FunctionID]
				if len(roles) == 0 || !SupportsEnemyBranchCondition(skill.BranchCondition) || !SupportsEnemyBranchCondition(skill.BranchCondition2) {
					return fmt.Errorf("enemy %d state skill %d has incomplete or unsupported branches", id, action.SkillID)
				}
				for _, role := range roles {
					if !SupportsEnemyFunction(role.Function) {
						return fmt.Errorf("enemy %d state skill %d has unsupported role %q", id, action.SkillID, role.Function)
					}
				}
			}
		}
	}
	return nil
}
