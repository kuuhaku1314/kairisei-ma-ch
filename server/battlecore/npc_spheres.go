package battlecore

// cf660 appends eligible non-chalice spheres after the hand. A selected sphere
// excludes all three sphere slots from further choices in this planning pass.
func (engine *BattleEngine) npcSpherePool(player *battlePlayer, remaining int) ([]npcCandidate, error) {
	var choices []npcCandidate
	for i := range player.Spheres {
		sphere := &player.Spheres[i]
		if sphere.SphereID == 0 || sphere.Type != SphereTypeNormal || sphere.Count <= 0 || !sphere.Playable {
			continue
		}
		skill, roles, err := engine.catalog.SphereSkill(sphere.SphereID)
		if err != nil {
			return nil, err
		}
		cost := maxInt(0, skill.Cost)
		if cost > remaining {
			continue
		}
		action := battleAction{memberType: player.MemberType, sphereSlot: sphere.Slot, cardLevel: sphere.executionLevel(skill.ID), skill: skill, roles: roles}
		branch, _ := engine.selectCombatSkillVariantsMode(engine.catalog.Skills[skill.ID], action, engine.turnActions, engine.appendChainCounts(player.MemberType, skill), false)
		if branch.ID != 0 {
			roles = engine.catalog.SkillRoles[branch.FunctionID]
		} else {
			roles = nil
		}
		display, err := engine.sphereDisplayState(player, sphere)
		if err != nil {
			return nil, err
		}
		choices = append(choices, npcCandidate{sphereSlot: sphere.Slot, level: sphere.executionLevel(skill.ID), skill: skill, roles: roles, cost: cost, power: display.Power, arthur: true})
	}
	return choices, nil
}
