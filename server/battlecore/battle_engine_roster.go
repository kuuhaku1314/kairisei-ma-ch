package battlecore

import (
	"iter"
	"maps"
)

// A player roster is distinct from monster enemies. PVP keeps a second party
// of the same player state; the native member IDs remain1..4 and5..8.
func (engine *BattleEngine) playerMember(member int) bool {
	return member >= 1 && (member <= maxRoomMembers || engine.pvp && member <= 2*maxRoomMembers)
}

// Callers resolve/validate a player member before accessing its state.
func (engine *BattleEngine) player(member int) *battlePlayer {
	if member == storyHelpMember {
		return &engine.storyHelp.Actor
	}
	if member <= maxRoomMembers {
		return &engine.players[member-1]
	}
	return &engine.opponents[member-maxRoomMembers-1]
}

func (engine *BattleEngine) playerParty(member int) *[4]battlePlayer {
	if engine.pvp && member > maxRoomMembers {
		return engine.opponents
	}
	return &engine.players
}

// Preserve native member order without allocating a combined roster.
func (engine *BattleEngine) playerStates() iter.Seq2[int, *battlePlayer] {
	return func(yield func(int, *battlePlayer) bool) {
		for i := range engine.players {
			if !yield(i, &engine.players[i]) {
				return
			}
		}
		if engine.pvp {
			for i := range engine.opponents {
				if !yield(i+maxRoomMembers, &engine.opponents[i]) {
					return
				}
			}
		}
	}
}

func (engine *BattleEngine) opposingParty(member int) *[4]battlePlayer {
	if member <= maxRoomMembers {
		return engine.opponents
	}
	return &engine.players
}
func (engine *BattleEngine) turnDisplayLimit() int64 {
	if engine.pvp {
		return 2147483647
	}
	return 0
}

func (engine *BattleEngine) playerCount() int {
	if engine.pvp {
		return 8
	}
	return 4
}
func (engine *BattleEngine) samePlayerParty(a, b int) bool { return !engine.pvp || (a-1)/4 == (b-1)/4 }
func (engine *BattleEngine) partyActions(actions []battleAction, member int) []battleAction {
	if !engine.pvp {
		return actions
	}
	result := make([]battleAction, 0, len(actions))
	for _, a := range actions {
		if engine.samePlayerParty(member, a.memberType) {
			result = append(result, a)
		}
	}
	return result
}
func (engine *BattleEngine) selectionView() BattleEngine {
	view := *engine
	view.selectedPlays = maps.Clone(engine.selectedPlays)
	if view.selectedPlays == nil {
		view.selectedPlays = make(map[int]cardPlaySubmission)
	}
	if engine.opponents != nil {
		party := *engine.opponents
		view.opponents = &party
	}
	return view
}
func (engine *BattleEngine) commitSelections(view *BattleEngine) {
	engine.players, engine.opponents = view.players, view.opponents
	engine.rng, engine.selectedPlays, engine.selectionOrder = view.rng, view.selectedPlays, view.selectionOrder
	engine.turnActions, engine.npcSimulatedHP, engine.npcSimulatedMaxHP = view.turnActions, view.npcSimulatedHP, view.npcSimulatedMaxHP
}

func (engine *BattleEngine) actionPriority(skill CombatSkillDefinition) int {
	if engine.pvp {
		return skill.PriorityPVP
	}
	return skill.PriorityPVE
}

// Native PVP uses side0/1 in the input-group field used by ordinary manual/CPU
// ordering. This does not change an action's actual automatic-selection flag.
func (engine *BattleEngine) actionOrderGroup(action battleAction) bool {
	if engine.pvp {
		return action.memberType > maxRoomMembers
	}
	return action.automatic
}

// Original PVP68a07 caches this card ordering weight before Start passives.
// Its fame/love/rarity inputs do not mutate during combat; spheres have no card.
func (engine *BattleEngine) pvpCardWeight(action battleAction) int32 {
	if action.sphereSlot != 0 || action.cardType < 1 || action.cardType > 20 {
		return 0
	}
	card, ok := conditionCardByType(engine.player(action.memberType), action.cardType)
	if !ok {
		return 0
	}
	definition := engine.catalog.Cards[card.CardID]
	bonus := int32(1)
	if definition.RarityPlus == 1 {
		bonus = 100
	}
	factor := int32(1)
	switch definition.Rarity {
	case "EXRARE", "LEGEND", "TOKEN_GOOD", "TOKEN_BAD":
		factor = 2
	}
	return (int32(card.Fame) + int32(engine.catalog.cardLoveRate(card)) + bonus) * factor
}

func (engine *BattleEngine) livingSideMembers(actor int, enemy, parts bool) []int {
	var result []int
	if engine.pvp {
		if parts {
			return nil
		}
		party := engine.playerParty(actor)
		if enemy {
			party = engine.opposingParty(actor)
		}
		for _, p := range party {
			if p.MemberType != 0 && p.HP > 0 {
				result = append(result, p.MemberType)
			}
		}
	} else if enemy {
		return engine.livingMonsterMembers(parts)
	} else {
		for _, p := range engine.playerParty(actor) {
			if p.MemberType != 0 && p.HP > 0 {
				result = append(result, p.MemberType)
			}
		}
	}
	return result
}

// A few native selectors explicitly query EnemyData (6de09/6debd), even
// when the general NPC target domain also includes opposing player parties.
func (engine *BattleEngine) livingMonsterMembers(parts bool) []int {
	var members []int
	for i := 0; i < engine.enemyCount; i++ {
		e := &engine.enemies[i]
		if e.MemberType != 0 && e.HP > 0 && (!parts || e.Parent > 0) {
			members = append(members, e.MemberType)
		}
	}
	return members
}

// Neutral actors share parameter storage, without becoming party participants.
func (engine *BattleEngine) parameterState(member int) *battlePlayer {
	if engine.playerMember(member) || member == storyHelpMember && engine.storyHelp != nil {
		return engine.player(member)
	}
	return nil
}
