package battlecore

import "kairisei.local/server/battlemodel"

func (sphere battleSphere) executionLevel(id int) int {
	return (battlemodel.Sphere{Level: sphere.Level, SkillLevels: sphere.SkillLevels}).ExecutionLevel(id)
}

func (action battleAction) callExecutionLevel() int {
	if action.hasCallLevel {
		return action.callLevel
	}
	return action.cardLevel
}
