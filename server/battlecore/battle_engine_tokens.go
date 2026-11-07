package battlecore

import "fmt"

const tokenCardFirst = 23

// Tokens reference immutable CALL programs, not inventory cards or draw-pool
// entries. The native allocator shares a cursor but checks each holder's slots.
type battleToken struct {
	ID, SkillID, Level int
	EnemySkill         bool
}

func isTokenCardType(cardType int) bool { return cardType >= 23 && cardType <= 32 }

// IsHandCardInput accepts empty selections and the two physical hand domains.
// Ownership, skill filters and battle phase remain the engine's responsibility.
func IsHandCardInput(cardType int) bool {
	return cardType >= 0 && cardType <= 10 || isTokenCardType(cardType)
}

func (player *battlePlayer) handSlotCard(slot int) BattleCard {
	if isTokenCardType(slot) {
		token := player.Tokens[slot-tokenCardFirst]
		return BattleCard{CardType: slot, CardID: token.ID, Level: token.Level}
	}
	return player.Deck[slot-1]
}

func (player *battlePlayer) tokenUsesEnemySkill(cardType int) bool {
	return isTokenCardType(cardType) && player.Tokens[cardType-tokenCardFirst].EnemySkill
}

func (engine *BattleEngine) handCardSkill(player *battlePlayer, card BattleCard) (CombatSkillDefinition, []CombatSkillRole, error) {
	if player.SummonSource != 0 && card.CardType == 1 {
		id := card.SkillLevels[0].ID
		variants := engine.catalog.Skills[id]
		if len(variants) == 0 {
			return CombatSkillDefinition{}, nil, fmt.Errorf("summoned action skill %d is unavailable", id)
		}
		return variants[0], engine.catalog.SkillRoles[variants[0].FunctionID], nil
	}
	if !isTokenCardType(card.CardType) {
		return engine.catalog.CardSkill(card.CardID, player.ArthurType)
	}
	token := player.Tokens[card.CardType-tokenCardFirst]
	variants := engine.catalog.Skills[token.SkillID]
	if len(variants) == 0 {
		return CombatSkillDefinition{}, nil, fmt.Errorf("token CALL skill %d is unavailable", token.SkillID)
	}
	return variants[0], engine.catalog.SkillRoles[variants[0].FunctionID], nil
}

func (engine *BattleEngine) handCardCallSkill(player *battlePlayer, card BattleCard) (CombatSkillDefinition, []CombatSkillRole, error) {
	if player.SummonSource != 0 || isTokenCardType(card.CardType) {
		return CombatSkillDefinition{}, nil, nil
	}
	return engine.catalog.CardCallSkill(card.CardID)
}

func (engine *BattleEngine) createEnemyTokenCards(actor *battleEnemy, selected int, role CombatSkillRole) ([]BattleResult, error) {
	index := combatParameterInt(role.Parameters[0])
	if index < 0 || index >= len(actor.Level.CallSkillIDs) {
		return nil, fmt.Errorf("token CALL slot %d is invalid", index)
	}
	level := 1
	if owned := actor.Level.CallSkillLevels[index]; owned.ID != 0 {
		level = owned.Value
	}
	return engine.createTokenCards(engine.enemyRolePlayerTargets(actor, selected, role), role, actor.Level.CallSkillIDs[index], level, true)
}

func (engine *BattleEngine) createTokenCards(targets []int, role CombatSkillRole, skillID, level int, enemySkill bool) ([]BattleResult, error) {
	if skillID == 0 {
		return nil, nil
	}
	var results []BattleResult
	for _, member := range targets {
		player := engine.player(member)
		remaining := combatParameterInt(role.Parameters[1])
		available := 0
		for _, slot := range player.Hand {
			if slot == 0 {
				available++
				continue
			}
			card := player.handSlotCard(slot)
			for _, selected := range engine.selectedPlays[member].CardTypes {
				if selected != 0 && selected == card.CardType {
					available++
					break
				}
			}
		}
		for _, slot := range player.TokenStock {
			if slot != 0 {
				available--
			}
		}
		for stockSlot, value := range player.TokenStock {
			if remaining <= 0 || available <= 0 {
				break
			}
			if value != 0 {
				continue
			}
			index := engine.tokenCursor
			for scanned := 0; scanned < len(player.Tokens) && player.Tokens[index].SkillID != 0; scanned++ {
				index = (index + 1) % len(player.Tokens)
			}
			if player.Tokens[index].SkillID != 0 {
				break
			}
			engine.tokenCursor = (index + 1) % len(player.Tokens)
			cardType := tokenCardFirst + index
			player.Tokens[index] = battleToken{ID: combatParameterInt(role.Parameters[2]), SkillID: skillID, Level: level, EnemySkill: enemySkill}
			player.TokenStock[stockSlot] = cardType
			player.CardDisplay[cardType] = battleDisplayPower{Known: true}
			results = append(results, BattleResult{Command: 327, Args: []int64{
				int64(member), int64(role.RoleIndex), int64(cardType), 0, int64(skillID), int64(index), int64(level), 0, int64(player.Tokens[index].ID),
			}})
			remaining--
			available--
		}
	}
	return results, nil
}

func fixedUserSideArthurType(kind string) int {
	switch kind {
	case "USER_SIDE_MERCENARY":
		return 1
	case "USER_SIDE_MILLIONAIRE":
		return 2
	case "USER_SIDE_THIEF":
		return 3
	case "USER_SIDE_SINGER":
		return 4
	}
	return 0
}

// Role targets18..21 search the primary user side by ArthurType, even in
// PVP when the caster is on the opposing party. The native selector draws once
// among living matches, including a singleton; absent professions do not draw.
func (engine *BattleEngine) userSideRoleTargets(kind string) ([]int, bool) {
	arthurType := fixedUserSideArthurType(kind)
	if arthurType == 0 {
		return nil, false
	}
	var candidates []int
	for _, player := range engine.players {
		if player.HP > 0 && player.ArthurType == arthurType {
			candidates = append(candidates, player.MemberType)
		}
	}
	if len(candidates) == 0 {
		return nil, true
	}
	return []int{candidates[int(engine.rng.next()%uint32(len(candidates)))]}, true
}

// Native keeps generated cards in a stock buffer until the action phase has
// removed its selected cards. Their hand/display state is not live mid-skill.
func (engine *BattleEngine) flushTokenStock() {
	for _, player := range engine.playerStates() {
		for _, cardType := range player.TokenStock {
			if cardType == 0 {
				continue
			}
			for slot, value := range player.Hand {
				if value == 0 {
					player.Hand[slot] = cardType
					player.CardDisplay[cardType] = battleDisplayPower{Known: true}
					break
				}
			}
		}
		player.TokenStock = [5]int{}
	}
}
