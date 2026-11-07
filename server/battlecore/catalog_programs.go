package battlecore

import "iter"

// Master tables share one program namespace. Validation walks the programs
// referenced by each actual consumer, rather than inferring use from a filename.
func (c *CombatCatalog) programs(references iter.Seq[int]) iter.Seq2[int, []CombatSkillDefinition] {
	return func(yield func(int, []CombatSkillDefinition) bool) {
		seen := make(map[int]bool)
		for id := range references {
			if id == 0 || seen[id] {
				continue
			}
			seen[id] = true
			if !yield(id, c.Skills[id]) {
				return
			}
		}
	}
}

func (c *CombatCatalog) PlayerSkillPrograms() iter.Seq2[int, []CombatSkillDefinition] {
	return c.programs(func(yield func(int) bool) {
		for _, card := range c.Cards {
			for _, id := range []int{card.NormalSkillID, card.ArthurSkillID, card.CallSkillID} {
				if !yield(id) {
					return
				}
			}
		}
		for _, sphere := range c.Spheres {
			if !yield(sphere.SkillID) || !yield(sphere.CallSkillID) {
				return
			}
		}
		for _, summon := range c.Summons {
			if !yield(summon.UseSkillID) {
				return
			}
		}
	})
}

func (c *CombatCatalog) BurstSkillPrograms() iter.Seq2[int, []CombatSkillDefinition] {
	return c.programs(func(yield func(int) bool) {
		for _, buddy := range c.Buddies {
			if !yield(buddy.PassiveSkillID) {
				return
			}
			for _, slot := range buddy.Skills {
				if !yield(slot.SkillID) || !yield(slot.CallSkillID) {
					return
				}
			}
		}
	})
}

func (c *CombatCatalog) EnemySkillPrograms() iter.Seq2[int, []CombatSkillDefinition] {
	return c.programs(func(yield func(int) bool) {
		for _, level := range c.EnemyLevels {
			if !yield(level.PassiveSkillID) {
				return
			}
			for _, id := range level.CallSkillIDs {
				if !yield(id) {
					return
				}
			}
			for _, action := range level.Actions {
				if !yield(action.SkillID) {
					return
				}
			}
			for _, action := range level.GaugeActions {
				if !yield(action.SkillID) {
					return
				}
			}
		}
	})
}

func (c *CombatCatalog) roleSets(programs iter.Seq2[int, []CombatSkillDefinition]) iter.Seq2[int, []CombatSkillRole] {
	return func(yield func(int, []CombatSkillRole) bool) {
		seen := make(map[int]bool)
		for _, variants := range programs {
			for _, skill := range variants {
				if seen[skill.FunctionID] {
					continue
				}
				seen[skill.FunctionID] = true
				if !yield(skill.FunctionID, c.SkillRoles[skill.FunctionID]) {
					return
				}
			}
		}
	}
}

func (c *CombatCatalog) EnemyRoleSets() iter.Seq2[int, []CombatSkillRole] {
	return c.roleSets(c.EnemySkillPrograms())
}

func (c *CombatCatalog) BurstRoleSets() iter.Seq2[int, []CombatSkillRole] {
	return c.roleSets(c.BurstSkillPrograms())
}
