package battlecore

import "fmt"

// validateSphereSupportContracts binds the official 16000014/62000265 row,
// ResultCmd56 -> ResultCmd69 setup projection and every native support
// arithmetic family used by the reachable CN sphere master.
func validateSphereSupportContracts(catalog *CombatCatalog) error {
	definition, exists := catalog.Spheres[16000014]
	if !exists || definition.Type != SphereTypeChalice || definition.PassiveSkillID != 62000265 {
		return fmt.Errorf("official sphere support representative 16000014 is %+v", definition)
	}
	skill, roles, err := catalog.SphereSupportSkill(definition.ID)
	if err != nil {
		return err
	}
	if skill.ID != 62000265 || skill.Target != "USER_ALL" || len(roles) != 1 {
		return fmt.Errorf("official sphere support definition is skill=%+v roles=%+v", skill, roles)
	}
	role := roles[0]
	if role.Function != "ATK_UP_BOOST" || role.Target != "SELECT" {
		return fmt.Errorf("official sphere support role is %+v", role)
	}
	effect, err := sphereSupportEffect(role, definition.MaxLevel, 1, 1)
	if err != nil {
		return err
	}
	if effect.Function != "ATK_UP_BOOST" || effect.ListType != 1 || effect.Parameter != "ATK" || effect.Attribute != "LIGHT" ||
		effect.Value != 0 || effect.Rate != 500 || effect.Remaining != 99 || effect.Source != 1 {
		return fmt.Errorf("official sphere support effect is %+v", effect)
	}
	if combatParameterFlag(effect.Parameter) != 4 || sphereSupportAttributeFlags(effect.Attribute) != 16 {
		return fmt.Errorf("official sphere support flags are parameter=%d attribute=%d", combatParameterFlag(effect.Parameter), sphereSupportAttributeFlags(effect.Attribute))
	}
	compositeDefinition, exists := catalog.Spheres[16000220]
	if !exists || compositeDefinition.PassiveSkillID != 62000586 {
		return fmt.Errorf("official composite sphere support representative 16000220 is %+v", compositeDefinition)
	}
	_, compositeRoles, err := catalog.SphereSupportSkill(compositeDefinition.ID)
	if err != nil {
		return err
	}
	if len(compositeRoles) != 1 {
		return fmt.Errorf("official composite sphere support roles are %+v", compositeRoles)
	}
	compositeEffect, err := sphereSupportEffect(compositeRoles[0], compositeDefinition.MaxLevel, 1, 1)
	if err != nil {
		return err
	}
	if compositeEffect.Function != "ATK_UP_BOOST" || compositeEffect.Attribute != "LIGHT_DARK" ||
		compositeEffect.Parameter != "INT" || sphereSupportAttributeFlags(compositeEffect.Attribute) != 2097152 {
		return fmt.Errorf("official composite sphere support effect is %+v flags=%d", compositeEffect, sphereSupportAttributeFlags(compositeEffect.Attribute))
	}
	if got := sphereSupportBoostedValue([]battleEffect{compositeEffect}, "ATK_UP_BOOST", "LIGHT", "INT", 1000); got <= 1000 {
		return fmt.Errorf("official LIGHT_DARK sphere support did not match LIGHT: %d", got)
	}
	if got := sphereSupportBoostedValue([]battleEffect{compositeEffect}, "ATK_UP_BOOST", "DARK", "INT", 1000); got <= 1000 {
		return fmt.Errorf("official LIGHT_DARK sphere support did not match DARK: %d", got)
	}
	if got := sphereSupportBoostedValue([]battleEffect{compositeEffect}, "ATK_UP_BOOST", "FIRE", "INT", 1000); got != 1000 {
		return fmt.Errorf("official LIGHT_DARK sphere support matched FIRE: %d", got)
	}

	// The other two reachable parameter-boost functions must keep their own
	// official object graph and consumer identity. They share native
	// FUN_0009c67b arithmetic, but role 125 emits buff kind 24 and role 126
	// emits kind 25; covering only ATK_UP_BOOST would not detect either family
	// being silently routed through the wrong parameter or attribute matcher.
	defenseDefinition, exists := catalog.Spheres[16000030]
	if !exists || defenseDefinition.Type != SphereTypeChalice || defenseDefinition.PassiveSkillID != 62000181 {
		return fmt.Errorf("official DEF_UP_BOOST sphere 16000030 is %+v", defenseDefinition)
	}
	_, defenseRoles, err := catalog.SphereSupportSkill(defenseDefinition.ID)
	if err != nil {
		return err
	}
	if len(defenseRoles) != 2 {
		return fmt.Errorf("official DEF_UP_BOOST sphere roles are %+v", defenseRoles)
	}
	defenseEffects := make([]battleEffect, 0, len(defenseRoles))
	defenseParameters := map[string]bool{"DEF": false, "MDEF": false}
	for _, defenseRole := range defenseRoles {
		defenseEffect, effectErr := sphereSupportEffect(defenseRole, defenseDefinition.MaxLevel, 1, 1)
		if effectErr != nil {
			return effectErr
		}
		if defenseEffect.Function != "DEF_UP_BOOST" || defenseEffect.Attribute != "FIRE" ||
			defenseEffect.Value != 0 || defenseEffect.Rate != 10 || defenseEffect.Remaining != 99 ||
			defenseEffect.Source != 1 || defenseEffect.ListType != 1 {
			return fmt.Errorf("official DEF_UP_BOOST effect is %+v", defenseEffect)
		}
		if _, known := defenseParameters[defenseEffect.Parameter]; !known {
			return fmt.Errorf("official DEF_UP_BOOST parameter is %q", defenseEffect.Parameter)
		}
		defenseParameters[defenseEffect.Parameter] = true
		defenseEffects = append(defenseEffects, defenseEffect)
	}
	if !defenseParameters["DEF"] || !defenseParameters["MDEF"] {
		return fmt.Errorf("official DEF_UP_BOOST parameters are %+v", defenseParameters)
	}
	if got := sphereSupportBoostedValue(defenseEffects, "DEF_UP_BOOST", "FIRE", "DEF", 1000); got != 1010 {
		return fmt.Errorf("official DEF_UP_BOOST DEF value is %d, want 1010", got)
	}
	if got := sphereSupportBoostedValue(defenseEffects, "DEF_UP_BOOST", "FIRE", "MDEF", 1000); got != 1010 {
		return fmt.Errorf("official DEF_UP_BOOST MDEF value is %d, want 1010", got)
	}
	if got := sphereSupportBoostedValue(defenseEffects, "DEF_UP_BOOST", "DARK", "DEF", 1000); got != 1000 {
		return fmt.Errorf("official DEF_UP_BOOST attribute mismatch is %d, want 1000", got)
	}

	breakDefinition, exists := catalog.Spheres[16000070]
	if !exists || breakDefinition.Type != SphereTypeChalice || breakDefinition.PassiveSkillID != 62000471 {
		return fmt.Errorf("official ATK_BREAK_BOOST sphere 16000070 is %+v", breakDefinition)
	}
	_, breakRoles, err := catalog.SphereSupportSkill(breakDefinition.ID)
	if err != nil {
		return err
	}
	if len(breakRoles) != 1 {
		return fmt.Errorf("official ATK_BREAK_BOOST sphere roles are %+v", breakRoles)
	}
	breakEffect, err := sphereSupportEffect(breakRoles[0], breakDefinition.MaxLevel, 1, 2)
	if err != nil {
		return err
	}
	if breakEffect.Function != "ATK_BREAK_BOOST" || breakEffect.Attribute != "DARK" || breakEffect.Parameter != "ATK" ||
		breakEffect.Value != 0 || breakEffect.Rate != 200 || breakEffect.Remaining != 99 ||
		breakEffect.Source != 2 || breakEffect.ListType != 1 {
		return fmt.Errorf("official ATK_BREAK_BOOST effect is %+v", breakEffect)
	}
	if got := sphereSupportBoostedValue([]battleEffect{breakEffect}, "ATK_BREAK_BOOST", "DARK", "ATK", 1000); got != 1200 {
		return fmt.Errorf("official ATK_BREAK_BOOST value is %d, want 1200", got)
	}
	if got := sphereSupportBoostedValue([]battleEffect{breakEffect}, "ATK_BREAK_BOOST", "DARK", "INT", 1000); got != 1000 {
		return fmt.Errorf("official ATK_BREAK_BOOST parameter mismatch is %d, want 1000", got)
	}

	parameterEngine := newSphereContractEngine(catalog)
	parameterEngine.players[0].Spheres[0] = battleSphere{
		Slot: 1, SphereID: defenseDefinition.ID, Level: defenseDefinition.MaxLevel,
		Type: defenseDefinition.Type, Count: defenseDefinition.Count, Maximum: defenseDefinition.Count,
	}
	parameterEngine.players[1].Spheres[0] = battleSphere{
		Slot: 1, SphereID: breakDefinition.ID, Level: breakDefinition.MaxLevel,
		Type: breakDefinition.Type, Count: breakDefinition.Count, Maximum: breakDefinition.Count,
	}
	parameterResults, err := parameterEngine.Start()
	if err != nil {
		return fmt.Errorf("execute official DEF_UP/ATK_BREAK sphere passives: %w", err)
	}
	parameterHeaders := 0
	defenseBuffs := 0
	breakBuffs := 0
	for _, result := range parameterResults {
		switch result.Command {
		case CommandSphereSkill:
			parameterHeaders++
		case CommandPassiveBuff:
			if len(result.Args) != 11 {
				return fmt.Errorf("official parameter sphere passive row is %+v", result)
			}
			switch int(result.Args[3]) {
			case battleBuffCodes["DEF_UP_BOOST"]:
				defenseBuffs++
			case battleBuffCodes["ATK_BREAK_BOOST"]:
				breakBuffs++
			}
		}
	}
	if parameterHeaders != 2 || defenseBuffs != maxRoomMembers*2 || breakBuffs != maxRoomMembers {
		return fmt.Errorf("official parameter sphere projection has headers=%d defense=%d break=%d results=%+v", parameterHeaders, defenseBuffs, breakBuffs, parameterResults)
	}
	for index := range parameterEngine.players {
		playerEffects := parameterEngine.players[index].Effects
		if len(playerEffects) != 3 || playerEffects[0].Function != "DEF_UP_BOOST" ||
			playerEffects[1].Function != "DEF_UP_BOOST" || playerEffects[2].Function != "ATK_BREAK_BOOST" ||
			playerEffects[0].Source != 1 || playerEffects[1].Source != 1 || playerEffects[2].Source != 2 {
			return fmt.Errorf("official parameter sphere target %d state is %+v", index+1, playerEffects)
		}
	}

	engine := newSphereContractEngine(catalog)
	engine.players[0].Spheres[0] = battleSphere{
		Slot: 1, SphereID: definition.ID, Level: definition.MaxLevel,
		Type: definition.Type, Count: definition.Count, Maximum: definition.Count,
	}
	results, err := engine.Start()
	if err != nil {
		return fmt.Errorf("start official sphere support representative: %w", err)
	}
	wantHeader := []int64{1, 1, 62000265, 0, int64(definition.MaxLevel), 2, 0, 0, int64(skill.FunctionID), 0, 1}
	// JP support registration retains the ATK parameter marker.
	wantBuff := []int64{1, 0, 1, int64(battleBuffCodes["ATK_UP_BOOST"]), 1, 4, 16, 0, 0, 0, 0}
	foundHeader := false
	foundBuffs := 0
	for _, result := range results {
		if result.Command == CommandSphereSkill && equalBattleArgs(result.Args, wantHeader) {
			foundHeader = true
		}
		if result.Command == CommandPassiveBuff && len(result.Args) == len(wantBuff) {
			wantBuff[0] = int64(foundBuffs + 1)
			if equalBattleArgs(result.Args, wantBuff) {
				foundBuffs++
			}
		}
	}
	if !foundHeader || foundBuffs != maxRoomMembers {
		return fmt.Errorf("sphere support start projection has header=%t buffs=%d results=%+v", foundHeader, foundBuffs, results)
	}
	effect.SourceSkillID = 62000265
	for index := range engine.players {
		if len(engine.players[index].Effects) != 1 || engine.players[index].Effects[0] != effect {
			return fmt.Errorf("sphere support target %d state is %+v, want %+v", index+1, engine.players[index].Effects, effect)
		}
	}

	if got := sphereSupportBoostedValue([]battleEffect{effect}, "ATK_UP_BOOST", "LIGHT", "ATK", 1000); got != 1500 {
		return fmt.Errorf("sphere ATK_UP_BOOST value is %d, want 1500", got)
	}
	if got := sphereSupportBoostedValue([]battleEffect{effect}, "ATK_UP_BOOST", "ICE", "ATK", 1000); got != 1000 {
		return fmt.Errorf("sphere ATK_UP_BOOST attribute mismatch is %d, want 1000", got)
	}
	stacked := append([]battleEffect{effect}, battleEffect{
		Function: "ATK_UP_BOOST", ListType: 1, Attribute: "LIGHT", Parameter: "ATK",
		Value: 100, Rate: 250, Remaining: 99,
	})
	if got := sphereSupportBoostedValue(stacked, "ATK_UP_BOOST", "LIGHT", "ATK", 1000); got != 1925 {
		return fmt.Errorf("stacked sphere ATK_UP_BOOST value is %d, want 1925", got)
	}

	damageBoost := battleEffect{Function: "DAMAGE_BOOST", ListType: 1, Attribute: "NULL", DamageKind: "PHYSICS", Rate: 50, Remaining: 99}
	if fixed, rate := damageBoostTerms([]battleEffect{damageBoost}, "FIRE", "PHYSICS"); fixed != 0 || rate != 50 {
		return fmt.Errorf("sphere DAMAGE_BOOST terms are %d/%d, want 0/50", fixed, rate)
	}
	if fixed, rate := damageBoostTerms([]battleEffect{damageBoost}, "FIRE", "MAGIC"); fixed != 0 || rate != 0 {
		return fmt.Errorf("sphere DAMAGE_BOOST physics mismatch terms are %d/%d, want 0/0", fixed, rate)
	}

	damageCut := battleEffect{Function: "DAMAGE_CUT2", ListType: 1, Attribute: "WIND", DamageKind: "PHYSICS", Value: 100, Rate: 100, Remaining: 99}
	if got := sphereDamageCut([]battleEffect{damageCut}, "WIND", "PHYSICS", 1000); got != 810 {
		return fmt.Errorf("sphere DAMAGE_CUT2 value is %d, want 810", got)
	}
	if got := sphereDamageCut([]battleEffect{damageCut}, "FIRE", "PHYSICS", 1000); got != 1000 {
		return fmt.Errorf("sphere DAMAGE_CUT2 attribute mismatch is %d, want 1000", got)
	}
	barrier := battleEffect{Function: "ATTACK_BARRIER", Attribute: "ALL", DamageKind: "PHYSICS", Value: 950, Uses: 1, Remaining: 1}
	incoming := []battleEffect{damageCut, barrier}
	resolution := engine.resolveIncomingDamageEffects(&incoming, 1000, 10000, "WIND", "PHYSICS")
	if resolution.Damage != 0 || !resolution.BarrierTriggered || resolution.BarrierRemainingUses != 0 {
		return fmt.Errorf("sphere DAMAGE_CUT2/barrier order is resolution=%+v effects=%+v", resolution, incoming)
	}
	return nil
}
