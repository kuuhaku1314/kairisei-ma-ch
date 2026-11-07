//go:build workshop

package multiplayer

import (
	"fmt"
	"kairisei.local/server/battlecontent"
	"kairisei.local/server/battlecore"
	"maps"
)

// WorkshopSnapshot describes an explicit test situation, not a saved account.
// The editor asks the existing scheduler what it would choose in this situation.
type WorkshopSnapshot struct {
	Turn      int         `json:"turn"`
	HP        [4]int      `json:"hp"`
	Flags     [4]uint32   `json:"flags"`
	Variables [4][5]int32 `json:"variables"`
	Damage    [4][3]int64 `json:"damage"`
}

type WorkshopActionTrace struct {
	Actor     int    `json:"actor"`
	Name      string `json:"name"`
	Slot      int    `json:"slot"`
	Skill     int    `json:"skill"`
	SkillName string `json:"skill_name"`
	Category  string `json:"category"`
	Condition bool   `json:"condition"`
	Selected  int    `json:"selected"`
	Priority  int    `json:"priority"`
	Cost      int    `json:"cost"`
	Uses      int    `json:"uses"`
	MaxUses   int    `json:"max_uses"`
	Reason    string `json:"reason"`
}

type WorkshopTurnTrace struct {
	Turn   int                   `json:"turn"`
	Checks []WorkshopActionTrace `json:"checks"`
	Order  []WorkshopActionTrace `json:"order"`
}

func WorkshopConditionSupported(fields []string) bool {
	return ValidEnemyAIOrderFields(fields) && SupportsEnemyAITrigger(fields[29])
}

func WorkshopTargetSupported(target string) bool { return SupportsEnemyActionTarget(target) }

// WorkshopPlanRows parses the same native rows that the editor exports. Only
// these maps are overlaid; the validated source catalog remains immutable.
func WorkshopPlanRows(catalog *CombatCatalog, rows map[string][][]string, replaceOwned bool, partyID int, seed uint32, snapshots []WorkshopSnapshot) ([]WorkshopTurnTrace, error) {
	c, err := workshopCatalogRows(catalog, rows, replaceOwned)
	if err != nil {
		return nil, err
	}
	return WorkshopPlan(c, partyID, seed, snapshots)
}

func workshopCatalogRows(catalog *CombatCatalog, rows map[string][][]string, replaceOwned bool) (*CombatCatalog, error) {
	if catalog == nil {
		return nil, fmt.Errorf("missing workshop catalog")
	}
	c := *catalog
	if len(rows["skill_enemy.csv"])+len(rows["skill_role_enemy.csv"]) > 0 {
		updated, err := workshopEnemySkillRows(catalog, rows["skill_enemy.csv"], rows["skill_role_enemy.csv"], replaceOwned)
		if err != nil {
			return nil, err
		}
		c = *updated
	}
	c.Enemies = maps.Clone(catalog.Enemies)
	c.EnemyLevels = maps.Clone(catalog.EnemyLevels)
	c.EnemyParties = maps.Clone(catalog.EnemyParties)
	c.EnemyAIOrders = maps.Clone(catalog.EnemyAIOrders)
	for _, row := range rows["enemy.csv"] {
		value, err := battlecontent.ParseEnemy(row)
		if err != nil {
			return nil, err
		}
		if _, exists := c.Enemies[value.ID]; exists && !replaceOwned {
			return nil, fmt.Errorf("duplicate workshop enemy ID %d", value.ID)
		}
		c.Enemies[value.ID] = value
	}
	for _, row := range rows["enemy_lvup.csv"] {
		value, err := battlecontent.ParseEnemyLevel(row)
		if err != nil {
			return nil, err
		}
		if _, exists := c.EnemyLevels[value.ID]; exists && !replaceOwned {
			return nil, fmt.Errorf("duplicate workshop level ID %d", value.ID)
		}
		c.EnemyLevels[value.ID] = value
	}
	for _, row := range rows["enemy_party.csv"] {
		value, err := battlecontent.ParseEnemyParty(row)
		if err != nil {
			return nil, err
		}
		if _, exists := c.EnemyParties[value.ID]; exists && !replaceOwned {
			return nil, fmt.Errorf("duplicate workshop party ID %d", value.ID)
		}
		c.EnemyParties[value.ID] = value
	}
	for _, row := range rows["enemy_ai_order.csv"] {
		id, err := battlecontent.RequiredInt(row, 0, "enemy AI ID")
		if err != nil {
			return nil, err
		}
		if _, exists := c.EnemyAIOrders[id]; exists && !replaceOwned {
			return nil, fmt.Errorf("duplicate workshop AI ID %d", id)
		}
		fields := make([]string, max(38, len(row)))
		copy(fields, row)
		c.EnemyAIOrders[id] = CombatEnemyAIOrder{ID: id, Fields: fields}
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// WorkshopPlan adapts author input and display text around the core planner.
func WorkshopPlan(catalog *CombatCatalog, partyID int, seed uint32, snapshots []WorkshopSnapshot) ([]WorkshopTurnTrace, error) {
	inputs := make([]battlecore.EnemyPlanSnapshot, len(snapshots))
	for i, s := range snapshots {
		inputs[i] = battlecore.EnemyPlanSnapshot{Turn: s.Turn, HP: s.HP, Flags: s.Flags, Variables: s.Variables, Damage: s.Damage}
	}
	plans, err := PreviewEnemyPlans(catalog, partyID, seed, inputs)
	if err != nil {
		return nil, err
	}
	reasons := map[battlecore.PlanReason]string{
		battlecore.PlanBroken:         "部位已破坏",
		battlecore.PlanDeathOnly:      "死亡专用槽，行动计划不触发",
		battlecore.PlanNotPreemptive:  "不属于先制行动",
		battlecore.PlanExhausted:      "次数已用完",
		battlecore.PlanConditionFalse: "条件未满足",
		battlecore.PlanOverBudget:     "行动预算不足",
		battlecore.PlanEligible:       "条件满足，等待预算与概率选择",
		battlecore.PlanSelected:       "选中",
	}
	present := func(rows []battlecore.EnemyActionPlanTrace) []WorkshopActionTrace {
		result := make([]WorkshopActionTrace, 0, len(rows))
		for _, row := range rows {
			skillName := ""
			if skills := catalog.Skills[row.Skill]; len(skills) > 0 {
				skillName = skills[0].Name
			}
			result = append(result, WorkshopActionTrace{Actor: row.Actor, Name: catalog.Enemies[row.EnemyID].Name,
				Slot: row.Slot, Skill: row.Skill, SkillName: skillName, Category: row.Category, Condition: row.Condition,
				Selected: row.Selected, Priority: row.Priority, Cost: row.Cost, Uses: row.Uses, MaxUses: row.MaxUses, Reason: reasons[row.Reason]})
		}
		return result
	}
	result := make([]WorkshopTurnTrace, 0, len(plans))
	for _, plan := range plans {
		result = append(result, WorkshopTurnTrace{Turn: plan.Turn, Checks: present(plan.Checks), Order: present(plan.Order)})
	}
	return result, nil
}
