package battlecore

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// CombatCatalog adds execution validation and queries to immutable definitions.
type CombatCatalog Definitions

func (c *CombatCatalog) Validate() error {
	if c == nil || len(c.Cards) == 0 || len(c.Spheres) == 0 || len(c.Buddies) == 0 ||
		len(c.Skills) == 0 || len(c.SkillRoles) == 0 || len(c.RoleParamRules) == 0 ||
		len(c.EnemyParties) == 0 || len(c.Enemies) == 0 || len(c.EnemyLevels) == 0 || len(c.EnemyAIOrders) == 0 {
		return errors.New("combat catalog is incomplete")
	}
	if c.BurstGauge.NormalThreshold <= 0 || c.BurstGauge.Maximum < c.BurstGauge.NormalThreshold || c.BurstGauge.BreakTurns <= 0 {
		return errors.New("combat burst gauge config is invalid")
	}
	for index, rate := range c.BurstGauge.ReactionRates {
		if rate <= 0 {
			return fmt.Errorf("combat burst gauge reaction rate %d is invalid", index+1)
		}
	}
	for sphereID, sphere := range c.Spheres {
		if sphere.SameSphereID <= 0 || sphere.MaxLevel <= 0 || sphere.Count <= 0 ||
			(sphere.Type != "NORMAL" && sphere.Type != "CHALICE") {
			return fmt.Errorf("official sphere %d has invalid core data", sphereID)
		}
		if len(c.Skills[sphere.SkillID]) == 0 {
			return fmt.Errorf("official sphere %d references missing player skill %d", sphereID, sphere.SkillID)
		}
		if sphere.PassiveSkillID != 0 && len(c.Skills[sphere.PassiveSkillID]) == 0 {
			return fmt.Errorf("official sphere %d references missing support skill %d", sphereID, sphere.PassiveSkillID)
		}
		if sphere.CallSkillID != 0 && len(c.Skills[sphere.CallSkillID]) == 0 {
			return fmt.Errorf("official sphere %d references missing call skill %d", sphereID, sphere.CallSkillID)
		}
		switch sphere.PlayCondition {
		case "", "NONE":
		case "TURN_AFTER":
			if sphere.PlayParam < 0 {
				return fmt.Errorf("official sphere %d has invalid TURN_AFTER parameter", sphereID)
			}
		default:
			return fmt.Errorf("official sphere %d has unsupported play condition %q", sphereID, sphere.PlayCondition)
		}
	}
	for partyID, party := range c.EnemyParties {
		seen := 0
		for _, slot := range party.Slots {
			if slot.EnemyID == 0 {
				continue
			}
			seen++
			if _, exists := c.Enemies[slot.EnemyID]; !exists {
				return fmt.Errorf("enemy party %d references missing enemy %d", partyID, slot.EnemyID)
			}
			if _, exists := c.EnemyLevels[slot.EnemyID]; !exists {
				return fmt.Errorf("enemy party %d references missing enemy level %d", partyID, slot.EnemyID)
			}
		}
		if seen == 0 {
			return fmt.Errorf("enemy party %d is empty", partyID)
		}
	}
	return c.validateEnemyGauges()
}

// ValidateRoleParameterContracts checks the exact official parameter columns
// used by the status families simulated by the Go engine. The column meaning
// remains data-driven; numeric zero is valid for scale terms and appointed
// darkness slots, so only syntax and schema are gated here.
func (c *CombatCatalog) ValidateRoleParameterContracts() error {
	expected := map[string][]string{
		"ATTACK_AA":                     {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "ATTR", "SKILL_PHYSICS_TYPE"},
		"ATK_OP_DRAIN":                  {"VALUE", "VALUE", "VALUE"},
		"ATK_OP_DRAIN_ALL":              {"VALUE", "VALUE", "VALUE"},
		"ATK_OP_REVENGE":                {"VALUE", "VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE"},
		"ATK_OP_NOW_TURN_REVENGE":       {"VALUE", "VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE"},
		"ATK_OP_PIERCING":               {"VALUE", "VALUE"},
		"ATK_OP_ATTR_DEF_PIERCING":      {"VALUE", "VALUE"},
		"ATTR_CUT_ATTACK":               {"VALUE", "VALUE", "VALUE", "VALUE", "ATTR", "SKILL_PHYSICS_TYPE"},
		"ATTR_DRAIN_ATTACK":             {"VALUE", "VALUE", "VALUE", "VALUE", "ATTR", "SKILL_PHYSICS_TYPE"},
		"ATTR_CUT_ENCHANT":              {"VALUE", "VALUE", "VALUE", "VALUE", "ATTR"},
		"ATTR_DRAIN_ENCHANT":            {"VALUE", "VALUE", "VALUE", "VALUE", "ATTR"},
		"ATTR_CUT_DOT":                  {"VALUE", "VALUE", "VALUE", "VALUE", "DOT"},
		"ATTR_DRAIN_DOT":                {"VALUE", "VALUE", "VALUE", "VALUE", "DOT"},
		"ATK_OP_DAMAGE_INCREASE":        {"VALUE", "VALUE", "VALUE", "VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE"},
		"ATK_OP_ATTR_RATE_DOWN_INVALID": {},
		"DEAL_BONUS":                    {"VALUE"},
		"DEAL_PENALTY":                  {"VALUE"},
		"DEAL_PENALTY_TURN_APPOINT":     {"VALUE", "VALUE"},
		"HEAL_FIXED":                    {"VALUE", "VALUE", "VALUE", "VALUE", "SKILL_ROLE_BATTLE_PARAM"},
		"HEAL_BY_TARGET_MAXHP":          {"VALUE", "VALUE", "VALUE", "VALUE"},
		"HEAL_BY_TARGET_HP":             {"VALUE", "VALUE", "VALUE", "VALUE"},
		"HEAL_CUT":                      {"VALUE", "VALUE"},
		"HEAL_BY_SELF_PARAM":            {"SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE", "VALUE", "VALUE"},
		"REGENERATE_FIXED":              {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "SKILL_ROLE_BATTLE_PARAM"},
		"REGENERATE_BY_SELF_PARAM":      {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE", "VALUE", "VALUE"},
		"DESTRUCT":                      {"VALUE", "VALUE", "VALUE"},
		"ATTR_SEE":                      {"VALUE"},
		"HP_CUT":                        {"VALUE", "VALUE"},
		"BURST_GAUGE_QUICK_UP":          {"VALUE", "VALUE"},
		"ENEMY_AI_TRIGGER_FLAG_SET":     {"VALUE", "VALUE"},
		"ENEMY_AI_VAR_ADD":              {"VALUE", "VALUE"},
		"ENEMY_AI_VAR_SET":              {"VALUE", "VALUE"},
		"ENEMY_AWAKE_FLAG_SET":          {"VALUE"},
		"FORCE_BATTLE_END":              {},
		"HEAL_REVERSE":                  {"VALUE", "VALUE", "VALUE"},
		"ATTR_HIDE":                     {"VALUE"},
		"ENDURE":                        {"VALUE", "VALUE", "VALUE"},
		"ENEMY_CURSE":                   {"VALUE", "VALUE", "VALUE"},
		"CURSE_RELEASE":                 {"VALUE", "VALUE", "ATTR"},
		"CARD_TOKEN":                    {"VALUE", "VALUE", "VALUE"},
		"BLESS_RELEASE":                 {"VALUE", "VALUE", "ATTR"},
		"GUTS":                          {"VALUE", "VALUE", "VALUE"},
		"PARAM_LIMIT_BREAK_FIXED":       {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE", "VALUE", "VALUE"},
		"ATK_UP_BY_SELF_PARAM":          {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE", "VALUE", "VALUE"},
		"ATK_UP_FIXED":                  {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE", "VALUE", "VALUE"},
		"ATK_UP_FIXED_ENEMY":            {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE"},
		"DEF_UP_FIXED_ENEMY":            {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE"},
		"ATK_BREAK_FIXED_ENEMY":         {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE"},
		"GUARD_BREAK_FIXED_ENEMY":       {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE"},
		"DEF_UP_BY_SELF_PARAM":          {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE", "VALUE", "VALUE"},
		"DEF_UP_FIXED":                  {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE", "VALUE", "VALUE"},
		"ATK_BREAK_BY_SELF_PARAM":       {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE", "VALUE", "VALUE"},
		"ATK_BREAK_FIXED":               {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE", "VALUE", "VALUE"},
		"GUARD_BREAK_BY_SELF_PARAM":     {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE", "VALUE", "VALUE"},
		"GUARD_BREAK_FIXED":             {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE", "VALUE", "VALUE"},
		"CRITICAL_UP":                   {"VALUE", "VALUE", "VALUE"},
		"CRITICAL_DOWN":                 {"VALUE", "VALUE", "VALUE"},
		"WEAKNESS":                      {"VALUE", "VALUE", "VALUE"},
		"ENCHANT":                       {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "ATTR"},
		"COVERING":                      {"VALUE", "VALUE", "VALUE", "ATTR", "SKILL_PHYSICS_TYPE"},
		"COST_BLOCK":                    {"VALUE", "VALUE"},
		"REVIVE":                        {"VALUE", "VALUE", "VALUE"},
		"BLESS":                         {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "VALUE"},
		"BLESS_TURN_UP":                 {"VALUE", "ATTR"},
		"BLESS_TURN_DOWN":               {"VALUE", "ATTR"},
		"DARKNESS_RANDOM":               {"VALUE", "VALUE", "VALUE"},
		"DARKNESS_APPOINT":              {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "VALUE"},
		"CARD_TRAP_DAMAGE":              {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "SKILL_ROLE_BATTLE_PARAM"},
		"CARD_SEAL":                     {"VALUE", "VALUE", "VALUE", "VALUE", "SKILL_KIND_BIT", "ATTR_BIT", "VALUE", "VALUE", "VALUE"},
		"CARD_SEAL_REGIST":              {"VALUE", "VALUE", "VALUE"},
		"DARKNESS_REGIST":               {"VALUE", "VALUE", "VALUE"},
		"DEBUFF_REGIST":                 {"VALUE", "VALUE", "SKILL_ROLE_KIND_DEBUFF"},
		"STAN":                          {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE"},
		"ATTR_DEF_UP":                   {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "ATTR"},
		"ATTR_DEF_DOWN":                 {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "ATTR"},
		"BUFF_RELEASE":                  {"VALUE", "VALUE"},
		"DEBUFF_RELEASE":                {"VALUE", "VALUE"},
		"BUFF_RELEASE_ONE":              {"VALUE", "VALUE", "SKILL_ROLE_KIND_BUFF", "SKILL_ROLE_KIND_BUFF"},
		"DEBUFF_RELEASE_ONE":            {"VALUE", "VALUE", "SKILL_ROLE_KIND_DEBUFF", "SKILL_ROLE_KIND_DEBUFF"},
		"BUFF_RELEASE_ONE_NUM":          {"VALUE", "VALUE", "SKILL_ROLE_KIND_BUFF", "SKILL_ROLE_KIND_BUFF", "VALUE"},
		"DEBUFF_RELEASE_ONE_NUM":        {"VALUE", "VALUE", "SKILL_ROLE_KIND_DEBUFF", "SKILL_ROLE_KIND_DEBUFF", "VALUE"},
		"BUFF_RELEASE_RANDOM":           {"VALUE", "VALUE", "VALUE"},
		"DEBUFF_RELEASE_RANDOM":         {"VALUE", "VALUE", "VALUE"},
		"BUFF_RELEASE_OLD":              {"VALUE", "VALUE", "VALUE"},
		"DEBUFF_RELEASE_OLD":            {"VALUE", "VALUE", "VALUE"},
		"DAMAGE_BOOST":                  {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "ATTR", "SKILL_PHYSICS_TYPE", "VALUE", "VALUE"},
		"ATK_UP_BOOST":                  {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "ATTR", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE"},
		"DEF_UP_BOOST":                  {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "ATTR", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE"},
		"ATK_BREAK_BOOST":               {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "ATTR", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE"},
		"GUARD_BREAK_BOOST":             {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "ATTR", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE"},
		"HEAL_BOOST":                    {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "ATTR", "VALUE", "VALUE"},
		"CRITICAL_BOOST":                {"VALUE", "VALUE", "VALUE", "ATTR", "SKILL_PHYSICS_TYPE", "VALUE", "VALUE"},
		"DAMAGE_CUT2":                   {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "ATTR", "SKILL_PHYSICS_TYPE"},
	}
	for function, columns := range expected {
		rule, exists := c.RoleParamRules[function]
		if !exists {
			return fmt.Errorf("official CN parameter rule %s is missing", function)
		}
		for index := range rule.Parameters {
			wanted := ""
			if index < len(columns) {
				wanted = columns[index]
			}
			if rule.Parameters[index] != wanted {
				return fmt.Errorf("official CN parameter rule %s column %d is %q, want %q", function, index+1, rule.Parameters[index], wanted)
			}
		}
	}
	validateRoles := func(label string, roles map[int][]CombatSkillRole) error {
		for skillID, variants := range roles {
			for _, role := range variants {
				columns, relevant := expected[role.Function]
				if !relevant {
					continue
				}
				for index, column := range columns {
					if column != "VALUE" {
						continue
					}
					text := strings.TrimSpace(role.Parameters[index])
					if text == "" {
						continue
					}
					_, err := strconv.Atoi(text)
					if err != nil {
						return fmt.Errorf("official CN %s skill %d %s numeric parameter %d is invalid", label, skillID, role.Function, index+1)
					}
				}
				if (role.Function == "DEAL_BONUS" || role.Function == "DEAL_PENALTY" || role.Function == "DEAL_PENALTY_TURN_APPOINT") && combatParameterIntForValidation(role.Parameters[0]) <= 0 {
					return fmt.Errorf("official CN %s skill %d %s primary value is not positive", label, skillID, role.Function)
				}
			}
		}
		return nil
	}
	return validateRoles("skill", c.SkillRoles)
}

func combatParameterIntForValidation(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}

// ValidatePlayerFunctionCoverage is the startup capability gate for every
// official CN card in the supplied card master. It prevents an unregistered
// primary effect from surviving until a live battle happens to draw it.
func (c *CombatCatalog) ValidatePlayerFunctionCoverage() error {
	if err := c.ValidateCardSupportFunctionCoverage(); err != nil {
		return err
	}
	missing := make(map[string]struct{})
	validateSkill := func(skillID int) {
		if skillID <= 0 {
			return
		}
		for _, variant := range c.Skills[skillID] {
			functionID := variant.FunctionID
			if functionID == 0 {
				functionID = skillID
			}
			for _, role := range c.SkillRoles[functionID] {
				if role.Function != "" && !SupportsPlayerFunction(role.Function) {
					missing[role.Function] = struct{}{}
				} else if FunctionNeedsBuffCode(role.Function) {
					if _, exists := battleBuffCodes[role.Function]; !exists {
						missing[role.Function+"(buff-code)"] = struct{}{}
					}
				}
			}
		}
	}
	for _, card := range c.Cards {
		if err := c.ValidateMainCardPassives(card); err != nil {
			return err
		}
		if card.CallSkillID != 0 {
			if _, _, err := c.CardCallSkill(card.ID); err != nil {
				return err
			}
			for _, skill := range c.Skills[card.CallSkillID] {
				if err := ValidateAppendSkillDefinition("card call", skill); err != nil {
					return err
				}
				if len(c.SkillRoles[skill.FunctionID]) == 0 {
					return fmt.Errorf("card %d call skill role %d is incomplete", card.ID, skill.FunctionID)
				}
			}
		}
		for _, skillID := range []int{card.NormalSkillID, card.ArthurSkillID, card.CallSkillID} {
			validateSkill(skillID)
		}
	}
	for _, sphere := range c.Spheres {
		validateSkill(sphere.SkillID)
		validateSkill(sphere.CallSkillID)
	}
	if len(missing) == 0 {
		return nil
	}
	functions := make([]string, 0, len(missing))
	for function := range missing {
		functions = append(functions, function)
	}
	sort.Strings(functions)
	return fmt.Errorf("unregistered official CN player combat functions: %s", strings.Join(functions, ", "))
}

// ValidateBurstFunctionCoverage checks the shared programs referenced by actual
// buddy active, CALL and passive slots before those entries become playable.
func (c *CombatCatalog) ValidateBurstFunctionCoverage() error {
	missing := make(map[string]struct{})
	for skillID, variants := range c.BurstSkillPrograms() {
		for _, variant := range variants {
			if _, ok := combatSkillTargetCode(variant.Target); !ok {
				return fmt.Errorf("official burst skill %d has unsupported SKILL_TARGET %q", skillID, variant.Target)
			}
			roles := c.SkillRoles[variant.FunctionID]
			if len(roles) == 0 {
				return fmt.Errorf("official burst skill %d references missing role set %d", skillID, variant.FunctionID)
			}
		}
	}
	for _, roles := range c.BurstRoleSets() {
		for _, role := range roles {
			if role.Function != "" && !burstCombatFunctionRegistered(role.Function) {
				missing[role.Function] = struct{}{}
			} else if FunctionNeedsBuffCode(role.Function) {
				if _, exists := battleBuffCodes[role.Function]; !exists {
					missing[role.Function+"(buff-code)"] = struct{}{}
				}
			}
		}
	}
	for buddyID, buddy := range c.Buddies {
		if buddy.PassiveSkillID != 0 && len(c.Skills[buddy.PassiveSkillID]) == 0 {
			return fmt.Errorf("official buddy %d references missing passive skill %d", buddyID, buddy.PassiveSkillID)
		}
		for slot, skill := range buddy.Skills {
			if skill.SkillID != 0 && len(c.Skills[skill.SkillID]) == 0 {
				return fmt.Errorf("official buddy %d slot %d references missing burst skill %d", buddyID, slot+1, skill.SkillID)
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	functions := make([]string, 0, len(missing))
	for function := range missing {
		functions = append(functions, function)
	}
	sort.Strings(functions)
	return fmt.Errorf("unregistered official CN burst combat functions: %s", strings.Join(functions, ", "))
}

func burstCombatFunctionRegistered(function string) bool {
	switch function {
	case "ATK_UP_FIXED", "DEF_UP_FIXED", "PARAM_LIMIT_BREAK_FIXED", "BURST_GAUGE_QUICK_UP", "ENCHANT", "DAMAGE_BOOST",
		"NEED_COST_DOWN_BURST", "ATTACK_MULTISTAGE", "ADD_ATK_OP_PIERCING", "DAMAGE_BOOST_ORDER_TRIBAL",
		"ATK_UP_BOOST_ORDER_TRIBAL", "DEF_UP_BOOST_ORDER_TRIBAL", "CRITICAL_DAMAGE_BOOST", "DISCARD_DRAW",
		"LIMIT_BREAK_BONUS", "PARAM_UP_SKILL_BONUS", "CARD_SEARCH":
		return true
	default:
		return false
	}
}

// ValidateEnemyFunctionCoverage checks every program referenced by enemy levels,
// including currently unpublished bosses and programs stored in shared tables.
func (c *CombatCatalog) ValidateEnemyFunctionCoverage() error {
	missing := make(map[string]struct{})
	for _, roles := range c.EnemyRoleSets() {
		for _, role := range roles {
			if role.Function != "" && !SupportsEnemyFunction(role.Function) {
				missing[role.Function] = struct{}{}
			} else if FunctionNeedsBuffCode(role.Function) {
				if _, exists := battleBuffCodes[role.Function]; !exists {
					missing[role.Function+"(buff-code)"] = struct{}{}
				}
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	functions := make([]string, 0, len(missing))
	for function := range missing {
		functions = append(functions, function)
	}
	sort.Strings(functions)
	return fmt.Errorf("unregistered official CN enemy combat functions: %s", strings.Join(functions, ", "))
}

func (c *CombatCatalog) CardSkill(cardID int, arthurType int) (CombatSkillDefinition, []CombatSkillRole, error) {
	card, exists := c.Cards[cardID]
	if !exists {
		return CombatSkillDefinition{}, nil, fmt.Errorf("card %d is unavailable", cardID)
	}
	skillID := card.NormalSkillID
	if c.cardUsesArthurSkill(cardID, arthurType) {
		skillID = card.ArthurSkillID
	}
	variants := c.Skills[skillID]
	if len(variants) == 0 {
		return CombatSkillDefinition{}, nil, fmt.Errorf("card %d skill %d is incomplete", cardID, skillID)
	}
	roles := c.SkillRoles[variants[0].FunctionID]
	if len(roles) == 0 {
		return CombatSkillDefinition{}, nil, fmt.Errorf("card %d skill role %d is incomplete", cardID, variants[0].FunctionID)
	}
	return variants[0], append([]CombatSkillRole(nil), roles...), nil
}

// JP41e01 -> 78360 compares the base bonus skill's exact arthur_type(+20),
// independently of job_type(+16).
// NULL is not a wildcard. 795fb rejects the three-explicit-NONE sentinel;
// missing extend slots are zero-initialized NULL, not NONE.
func (c *CombatCatalog) cardUsesArthurSkill(cardID, arthurType int) bool {
	card := c.Cards[cardID]
	variants := c.Skills[card.ArthurSkillID]
	if card.ArthurSkillID <= 0 || len(variants) == 0 || !skillMatchesArthur(variants[0], arthurType) {
		return false
	}
	if len(variants) >= 3 {
		empty := true
		for _, variant := range variants[:3] {
			roles := c.SkillRoles[variant.FunctionID]
			if len(roles) == 0 || roles[0].Function != "NONE" {
				empty = false
				break
			}
		}
		if empty {
			return false
		}
	}
	return true
}

func skillMatchesArthur(skill CombatSkillDefinition, arthurType int) bool {
	if arthurType == 0 {
		return combatNullValue(skill.ArthurJob)
	}
	job := ArthurJob(arthurType)
	return job != "" && skill.ArthurJob == job
}

func (c *CombatCatalog) SphereSkill(sphereID int) (CombatSkillDefinition, []CombatSkillRole, error) {
	sphere, exists := c.Spheres[sphereID]
	if !exists {
		return CombatSkillDefinition{}, nil, fmt.Errorf("sphere %d is unavailable", sphereID)
	}
	variants := c.Skills[sphere.SkillID]
	if len(variants) == 0 {
		return CombatSkillDefinition{}, nil, fmt.Errorf("sphere %d skill %d is incomplete", sphereID, sphere.SkillID)
	}
	roles := c.SkillRoles[variants[0].FunctionID]
	if len(roles) == 0 {
		return CombatSkillDefinition{}, nil, fmt.Errorf("sphere %d skill role %d is incomplete", sphereID, variants[0].FunctionID)
	}
	return variants[0], append([]CombatSkillRole(nil), roles...), nil
}

func (c *CombatCatalog) SphereCallSkill(sphereID int) (CombatSkillDefinition, []CombatSkillRole, error) {
	sphere, exists := c.Spheres[sphereID]
	if !exists {
		return CombatSkillDefinition{}, nil, fmt.Errorf("sphere %d is unavailable", sphereID)
	}
	if sphere.CallSkillID == 0 {
		return CombatSkillDefinition{}, nil, nil
	}
	variants := c.Skills[sphere.CallSkillID]
	if len(variants) == 0 {
		return CombatSkillDefinition{}, nil, fmt.Errorf("sphere %d call skill %d is incomplete", sphereID, sphere.CallSkillID)
	}
	roles := c.SkillRoles[variants[0].FunctionID]
	if len(roles) == 0 {
		return CombatSkillDefinition{}, nil, fmt.Errorf("sphere %d call skill role %d is incomplete", sphereID, variants[0].FunctionID)
	}
	return variants[0], append([]CombatSkillRole(nil), roles...), nil
}

func (c *CombatCatalog) CardCallSkill(cardID int) (CombatSkillDefinition, []CombatSkillRole, error) {
	card, exists := c.Cards[cardID]
	if !exists {
		return CombatSkillDefinition{}, nil, fmt.Errorf("card %d is unavailable", cardID)
	}
	if card.CallSkillID == 0 {
		return CombatSkillDefinition{}, nil, nil
	}
	variants := c.Skills[card.CallSkillID]
	if len(variants) == 0 {
		return CombatSkillDefinition{}, nil, fmt.Errorf("card %d call skill %d is incomplete", cardID, card.CallSkillID)
	}
	roles := c.SkillRoles[variants[0].FunctionID]
	if len(roles) == 0 {
		return CombatSkillDefinition{}, nil, fmt.Errorf("card %d call skill role %d is incomplete", cardID, variants[0].FunctionID)
	}
	return variants[0], append([]CombatSkillRole(nil), roles...), nil
}

func ArthurJob(arthurType int) string {
	switch arthurType {
	case 1:
		return "MERCENARY"
	case 2:
		return "MILLIONAIRE"
	case 3:
		return "THIEF"
	case 4:
		return "SINGER"
	case 5:
		return "ALCHEMIST"
	default:
		return ""
	}
}
