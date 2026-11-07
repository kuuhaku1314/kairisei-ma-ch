package battlecore

import "fmt"

const storyHelpMember = 9

type battleStoryHelp struct {
	Input StoryHelpCard
	Actor battlePlayer
}

func (engine *BattleEngine) storyHelpTarget(skill CombatSkillDefinition) int {
	helper := engine.storyHelp.Input
	target := 0
	switch {
	case helper.Target == 0 || helper.Target == 5:
		candidates := engine.livingSideMembers(1, helper.Target == 5, false)
		if len(candidates) > 0 {
			target = candidates[int(engine.rng.next()%uint32(len(candidates)))]
		}
	case helper.Target >= 1 && helper.Target <= 4:
		target = helper.Target
	case helper.Target >= 6 && helper.Target <= 9:
		target = helper.Target - 1
	}
	hp, _, exists := engine.battleMemberHP(target)
	if exists && hp > 0 {
		return target
	}
	switch skill.Target {
	case "USER_ONE", "USER_ALL":
		return 1
	case "ENEMY_ONE", "ENEMY_ALL":
		for i := 0; i < engine.enemyCount; i++ {
			if engine.enemies[i].HP > 0 {
				return engine.enemies[i].MemberType
			}
		}
	}
	return 0
}

func (engine *BattleEngine) storyHelpTurnEnabled() bool {
	helper := engine.storyHelp
	return helper != nil && !(engine.turn >= 1 && engine.turn <= 10 && helper.Input.Turns[engine.turn-1] <= 0)
}

func (engine *BattleEngine) executeStoryHelp() ([]BattleResult, error) {
	helper := engine.storyHelp
	variants := engine.catalog.Skills[helper.Input.SkillID]
	if len(variants) == 0 {
		return nil, fmt.Errorf("story helper skill %d is unavailable", helper.Input.SkillID)
	}
	skill := variants[0]
	target := engine.storyHelpTarget(skill)
	if skill.Target == "ENEMY_ONE" && (target < 5 || target >= 5+engine.enemyCount) || skill.Target == "USER_ONE" && (target < 1 || target > 4) {
		return nil, nil
	}
	action := battleAction{helper: true, memberType: storyHelpMember, cardID: helper.Input.CardID, cardType: 0, cardLevel: helper.Input.Level, target: target, skill: skill, roles: engine.catalog.SkillRoles[skill.FunctionID]}
	return engine.executePlayerAction(action, nil)
}
