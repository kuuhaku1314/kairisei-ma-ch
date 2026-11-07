package battlecore

import "fmt"

// JP7a347 clears the card/sphere indices at a real skill entry. The Buddy index
// has its own setter and remains available when both physical indices are0.
// Preview7875a overrides only the card index and restores it. Keep identities,
// not SkillData copies or an independent CALL cache.
type battleExecutionSource struct {
	CardType       int
	SphereSlot     int
	BuddySkillSlot int
}

func (source *battleExecutionSource) setPhysical(cardType, sphereSlot int) {
	source.CardType, source.SphereSlot = cardType, sphereSlot
}

func (engine *BattleEngine) playerSourceCall(player *battlePlayer, source battleExecutionSource) (CombatSkillDefinition, []CombatSkillRole, int, error) {
	var skill CombatSkillDefinition
	var roles []CombatSkillRole
	level := 0
	var err error
	switch {
	case source.CardType == 22:
		// afe56 explicitly stops recursive BLESS lookup for an append preview.
	case source.CardType != 0:
		card, exists := conditionCardByType(player, source.CardType)
		if !exists {
			return skill, nil, 0, nil
		}
		skill, roles, err = engine.handCardCallSkill(player, card)
		level = card.ExecutionLevel(skill.ID)
	case source.SphereSlot != 0:
		sphere := player.Spheres[source.SphereSlot-1]
		skill, roles, err = engine.catalog.SphereCallSkill(sphere.SphereID)
		level = sphere.executionLevel(skill.ID)
	case source.BuddySkillSlot != 0:
		buddy := player.Buddies[0]
		id := engine.catalog.Buddies[buddy.BuddyID].Skills[source.BuddySkillSlot-1].CallSkillID
		if id != 0 {
			variants := engine.catalog.Skills[id]
			if len(variants) == 0 || len(engine.catalog.SkillRoles[variants[0].FunctionID]) == 0 {
				return skill, nil, 0, fmt.Errorf("combat buddy call skill %d is unavailable", id)
			}
			skill, roles = variants[0], engine.catalog.SkillRoles[variants[0].FunctionID]
			level = buddy.ExecutionLevel(id)
		}
	}
	return skill, roles, level, err
}
