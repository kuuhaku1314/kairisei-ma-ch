//go:build workshop

package multiplayer

import (
	"fmt"
	"maps"

	"kairisei.local/server/battlecontent"
)

func WorkshopAppendConditionSupported(kind string) bool { return SupportsAppendCondition(kind) }

// Match the native CSV booleans through the production parser. In particular
// negative/non-one mask fields are not enabled, and only 1 excludes self.
func WorkshopUnrestrictedRoleTargets(row []string) bool {
	role, err := battlecontent.ParseSkillRole(row)
	if err != nil || role.ExcludeSelf {
		return false
	}
	for _, allowed := range role.Attributes {
		if !allowed {
			return false
		}
	}
	return true
}

func WorkshopValidateAppendSkills(rows [][]string) error {
	for _, row := range rows {
		skill, err := battlecontent.ParseSkill(row)
		if err != nil {
			return err
		}
		if err = ValidateAppendSkillDefinition("workshop call", skill); err != nil {
			return err
		}
	}
	return nil
}

// Validate authored rows through the same parsers and supported native
// functions as the production catalog. The source remains immutable.
func WorkshopValidatePlayerSkills(catalog *CombatCatalog, skills, roles [][]string) error {
	c := *catalog
	c.Skills = maps.Clone(catalog.Skills)
	c.SkillRoles = maps.Clone(catalog.SkillRoles)
	for _, row := range skills {
		v, e := battlecontent.ParseSkill(row)
		if e != nil {
			return e
		}
		for _, condition := range []string{v.BranchCondition, v.BranchCondition2} {
			if !SupportsPlayerBranchCondition(condition) {
				return fmt.Errorf("技能%d的条件尚未接入战斗执行：%s", v.ID, condition)
			}
		}
		c.Skills[v.ID] = nil
	}
	for _, row := range roles {
		v, e := battlecontent.ParseSkillRole(row)
		if e != nil {
			return e
		}
		c.SkillRoles[v.SkillID] = nil
	}
	for _, row := range skills {
		v, e := battlecontent.ParseSkill(row)
		if e != nil {
			return e
		}
		battlecontent.AppendSkillVariant(c.Skills, v)
	}
	for _, row := range roles {
		v, e := battlecontent.ParseSkillRole(row)
		if e != nil {
			return e
		}
		if !SupportsPlayerFunction(v.Function) {
			return fmt.Errorf("技能效果尚未登记：%s", v.Function)
		}
		v, e = battlecontent.BindSkillRole(v, c.RoleParamRules)
		if e != nil {
			return e
		}
		v.RoleIndex = len(c.SkillRoles[v.SkillID])
		c.SkillRoles[v.SkillID] = append(c.SkillRoles[v.SkillID], v)
	}
	return c.ValidateRoleParameterContracts()
}

// Bind a temporary test catalog from the same authored rows used by native
// export. No assets, APK, accounts or resource pack are required for this path.
func WorkshopOverlayPlayerRows(catalog *CombatCatalog, cards, skills, roles [][]string) (*CombatCatalog, error) {
	c := *catalog
	c.Cards = maps.Clone(catalog.Cards)
	c.Skills = maps.Clone(catalog.Skills)
	c.SkillRoles = maps.Clone(catalog.SkillRoles)
	for _, row := range cards {
		v, e := battlecontent.ParseCard(row)
		if e != nil {
			return nil, e
		}
		if _, ok := c.Cards[v.ID]; ok {
			return nil, fmt.Errorf("duplicate test card ID")
		}
		c.Cards[v.ID] = v
	}
	seenSkills, seenRoles := map[int]bool{}, map[int]bool{}
	for _, row := range skills {
		v, e := battlecontent.ParseSkill(row)
		if e != nil {
			return nil, e
		}
		if !seenSkills[v.ID] {
			if _, ok := c.Skills[v.ID]; ok {
				return nil, fmt.Errorf("duplicate test skill ID")
			}
			seenSkills[v.ID] = true
		}
		battlecontent.AppendSkillVariant(c.Skills, v)
	}
	for _, row := range roles {
		v, e := battlecontent.ParseSkillRole(row)
		if e != nil {
			return nil, e
		}
		if !seenRoles[v.SkillID] {
			if _, ok := c.SkillRoles[v.SkillID]; ok {
				return nil, fmt.Errorf("duplicate test effect ID")
			}
			seenRoles[v.SkillID] = true
		}
		v, e = battlecontent.BindSkillRole(v, c.RoleParamRules)
		if e != nil {
			return nil, e
		}
		v.RoleIndex = len(c.SkillRoles[v.SkillID])
		c.SkillRoles[v.SkillID] = append(c.SkillRoles[v.SkillID], v)
	}
	if e := c.ValidatePlayerFunctionCoverage(); e != nil {
		return nil, e
	}
	if e := c.ValidateCardSupportFunctionCoverage(); e != nil {
		return nil, e
	}
	return &c, nil
}
