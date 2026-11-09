//go:build workshop

package multiplayer

import (
	"fmt"
	"maps"

	"kairisei.local/server/battlecontent"
)

func WorkshopSupportFunctionSupported(function, mode string) bool {
	if mode == "passive" {
		return SupportsMainCardPassiveFunction(function)
	}
	return mode == "ex" && SupportsSupportFunction(function)
}

func WorkshopOverlaySupportRows(catalog *CombatCatalog, skills, roles [][]string, replace bool) (*CombatCatalog, error) {
	if len(skills)+len(roles) == 0 {
		return catalog, nil
	}
	c := *catalog
	c.Skills = maps.Clone(catalog.Skills)
	c.SkillRoles = maps.Clone(catalog.SkillRoles)
	seenSkills, seenRoles := map[int]bool{}, map[int]bool{}
	for _, row := range skills {
		v, e := battlecontent.ParseSkill(row)
		if e != nil {
			return nil, e
		}
		if len(row) < 50 || row[9] != "PASSIVE" {
			return nil, fmt.Errorf("被动技能必须保留原生PASSIVE类型")
		}
		if !seenSkills[v.ID] {
			if _, ok := c.Skills[v.ID]; ok && !replace {
				return nil, fmt.Errorf("被动技能编号冲突")
			}
			seenSkills[v.ID] = true
			c.Skills[v.ID] = nil
		}
		battlecontent.AppendSkillVariant(c.Skills, v)
	}
	for _, row := range roles {
		v, e := battlecontent.ParseSkillRole(row)
		if e != nil {
			return nil, e
		}
		if !seenRoles[v.SkillID] {
			if _, ok := c.SkillRoles[v.SkillID]; ok && !replace {
				return nil, fmt.Errorf("被动效果组编号冲突")
			}
			seenRoles[v.SkillID] = true
			c.SkillRoles[v.SkillID] = nil
		}
		v, e = battlecontent.BindSkillRole(v, c.RoleParamRules)
		if e != nil {
			return nil, e
		}
		v.RoleIndex = len(c.SkillRoles[v.SkillID])
		c.SkillRoles[v.SkillID] = append(c.SkillRoles[v.SkillID], v)
	}
	return &c, nil
}

func WorkshopValidateSupportSkill(catalog *CombatCatalog, skills, roles [][]string, mode string) error {
	if mode != "ex" && mode != "passive" {
		return fmt.Errorf("未知被动用途")
	}
	if len(skills) != 1 || len(roles) < 1 || len(roles) > 5 {
		return fmt.Errorf("被动需一个技能定义和1–5个效果")
	}
	base := *catalog
	base.Skills = map[int][]CombatSkillDefinition{}
	base.SkillRoles = map[int][]CombatSkillRole{}
	c, e := WorkshopOverlaySupportRows(&base, skills, roles, true)
	if e != nil {
		return e
	}
	skill, e := battlecontent.ParseSkill(skills[0])
	if e != nil {
		return e
	}
	if (skill.Target != "SELF" && skill.Target != "USER_ALL") || skill.BranchCondition != "" || skill.BranchCondition2 != "" {
		return fmt.Errorf("被动保持单定义，只开放自身／全队范围，属性和COST条件在效果中设置")
	}
	for _, row := range roles {
		role, err := battlecontent.ParseSkillRole(row)
		if err != nil {
			return err
		}
		if role.SkillID != skill.FunctionID || !WorkshopSupportFunctionSupported(role.Function, mode) {
			return fmt.Errorf("此效果尚未接入所选被动用途：%s", role.Function)
		}
		if role.Target != "SELECT" || role.ExcludeSelf {
			return fmt.Errorf("被动效果必须继承技能范围且不排除自身")
		}
		if role.Function == "BEGINNING_DRAW" && skill.Target != "SELF" {
			return fmt.Errorf("优先初始抽牌只能用于自身主卡被动")
		}
	}
	card := CombatCardDefinition{ID: 1}
	if mode == "ex" {
		card.SupportSkillIDs[0] = skill.ID
		c.Cards = map[int]CombatCardDefinition{1: card}
		return c.ValidateCardSupportFunctionCoverage()
	}
	card.PassiveSkillID = skill.ID
	e = c.ValidateMainCardPassives(card)
	return e
}
