//go:build workshop

package multiplayer

import (
	"fmt"
	"maps"

	"kairisei.local/server/battlecontent"
)

func WorkshopEnemySkillLevel(function string) int { return EnemySkillLevel(function) }
func WorkshopEnemyFunctionSupported(function string) bool {
	return SupportsEnemyFunction(function)
}

// AnimationBattleEventCall routes Buff and Debuff to the same native queue;
// Erase and Dispel likewise consume the release queue. These are presentation
// requirements, not a second implementation of the battle effects.
func WorkshopEnemyPresentationCallbacks(function string) []string {
	switch function {
	case "ATTACK_AA", "DESTRUCT":
		return []string{"Damage"}
	case "HEAL_FIXED", "HEAL_BY_SELF_PARAM", "HEAL_BY_TARGET_MAXHP", "ATK_OP_DRAIN", "REVIVE":
		return []string{"Heal"}
	case "HP_CUT":
		return []string{"HpCut"}
	case "BUFF_RELEASE", "BUFF_RELEASE_OLD", "BUFF_RELEASE_RANDOM", "BUFF_RELEASE_ONE", "BUFF_RELEASE_ONE_NUM",
		"DEBUFF_RELEASE", "DEBUFF_RELEASE_OLD", "DEBUFF_RELEASE_RANDOM", "DEBUFF_RELEASE_ONE", "DEBUFF_RELEASE_ONE_NUM":
		return []string{"Erase"}
	case "ENEMY_CURSE", "BLESS_TURN_DOWN", "BURST_GAUGE_QUICK_UP", "BURST_GAUGE_QUICK_DOWN":
		return []string{"Buff"}
	}
	if FunctionNeedsBuffCode(function) {
		return []string{"Buff"}
	}
	return nil
}

// The 2D script pipeline separates burst gauges from multi_buff. Its common
// buff script also consumes DOT value changes and enemy gauge additions.
func WorkshopEnemyScriptCallbacks(function string) []string {
	switch function {
	case "BURST_GAUGE_QUICK_UP", "BURST_GAUGE_QUICK_DOWN":
		return []string{"Burst"}
	case "DOT_VALUE_UP", "TRANCE_GAUGE_VALUE_UP", "TRANCE_GAUGE_VALUE_DOWN", "TRANCE_GAUGE_OVER_HEAT_TURN_ADD":
		return []string{"Buff"}
	case "TRANCE_GAUGE_STATE_CHANGE":
		return []string{"Trance"}
	}
	return WorkshopEnemyPresentationCallbacks(function)
}

func WorkshopValidateEnemySkills(catalog *CombatCatalog, skills, roles [][]string) error {
	c, e := workshopEnemySkillRows(catalog, skills, roles, true)
	if e != nil {
		return e
	}
	return c.ValidateRoleParameterContracts()
}

// Reuse the production passive contract: passive lifetime and execution are
// distinct from ordinary enemy actions.
func WorkshopValidateEnemyPassive(catalog *CombatCatalog, skills, roles [][]string) error {
	c, e := workshopEnemySkillRows(catalog, skills, roles, true)
	if e != nil {
		return e
	}
	c.EnemyLevels = map[int]CombatEnemyLevel{}
	for _, row := range skills {
		id := battlecontent.OptionalInt(row, 0)
		c.EnemyLevels[id] = CombatEnemyLevel{PassiveSkillID: id}
	}
	return ValidateEnemyPassiveDefinitions(c)
}

func workshopEnemySkillRows(catalog *CombatCatalog, skills, roles [][]string, replace bool) (*CombatCatalog, error) {
	c := *catalog
	c.Skills = maps.Clone(catalog.Skills)
	c.SkillRoles = maps.Clone(catalog.SkillRoles)
	seenSkills, seenRoles := map[int]bool{}, map[int]bool{}
	for _, row := range skills {
		value, e := battlecontent.ParseSkill(row)
		if e != nil {
			return nil, e
		}
		if !seenSkills[value.ID] {
			if _, ok := c.Skills[value.ID]; ok && !replace {
				return nil, fmt.Errorf("duplicate workshop enemy skill ID %d", value.ID)
			}
			c.Skills[value.ID] = nil
			seenSkills[value.ID] = true
		}
		battlecontent.AppendSkillVariant(c.Skills, value)
	}
	for _, row := range roles {
		value, e := battlecontent.ParseSkillRole(row)
		if e != nil {
			return nil, e
		}
		if !SupportsEnemyFunction(value.Function) {
			return nil, fmt.Errorf("BOSS技能效果尚未登记：%s", value.Function)
		}
		if !seenRoles[value.SkillID] {
			if _, ok := c.SkillRoles[value.SkillID]; ok && !replace {
				return nil, fmt.Errorf("duplicate workshop enemy function ID %d", value.SkillID)
			}
			c.SkillRoles[value.SkillID] = nil
			seenRoles[value.SkillID] = true
		}
		value, e = battlecontent.BindSkillRole(value, c.RoleParamRules)
		if e != nil {
			return nil, e
		}
		value.RoleIndex = len(c.SkillRoles[value.SkillID])
		c.SkillRoles[value.SkillID] = append(c.SkillRoles[value.SkillID], value)
	}
	for id := range seenSkills {
		if len(c.Skills[id]) > 5 {
			return nil, fmt.Errorf("enemy skill exceeds native branch capacity")
		}
		for _, v := range c.Skills[id] {
			if !SupportsEnemyBranchCondition(v.BranchCondition) || !SupportsEnemyBranchCondition(v.BranchCondition2) {
				return nil, fmt.Errorf("BOSS技能条件尚未接入：%s／%s", v.BranchCondition, v.BranchCondition2)
			}
			if len(c.SkillRoles[v.FunctionID]) == 0 || len(c.SkillRoles[v.FunctionID]) > 5 {
				return nil, fmt.Errorf("enemy skill has missing or oversized effects")
			}
		}
	}
	return &c, nil
}
