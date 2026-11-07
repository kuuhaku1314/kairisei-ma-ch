package battlecore

import "strings"

// JP 7ed30 visits the ten draw/trash positions interleaved, then selects with
// xor128 % remaining candidates and replaces each selected entry with the last.
// Selection changes the draw pool only; the subsequent DISCARD_DRAW does deals.
func (engine *BattleEngine) searchCards(player *battlePlayer, role CombatSkillRole) error {
	count := combatParameterInt(role.Parameters[0])
	if count <= 0 {
		return nil
	}
	type candidate struct {
		slot  int
		trash bool
	}
	pool := make([]candidate, 0, len(player.DeckOrder))
	add := func(slot int, trash bool) error {
		if slot < 0 {
			return nil
		}
		card := player.Deck[slot]
		skill, _, err := engine.catalog.CardSkill(card.CardID, player.ArthurType)
		if err != nil {
			return err
		}
		if !combatNullValue(role.Parameters[1]) && !combatAttributeMatches(skill.Attribute, role.Parameters[1]) ||
			!combatNullValue(role.Parameters[2]) && !strings.EqualFold(skill.Kind, role.Parameters[2]) {
			return nil
		}
		// 7ec50 accepts any of the three nonzero tags. With no tag filter,
		// attribute and kind alone decide; it does not require tag zero.
		hasTag, matches := false, false
		for _, value := range role.Parameters[3:6] {
			if tag := combatParameterInt(value); tag != 0 {
				hasTag = true
				matches = matches || combatCardHasProfileTag(engine.catalog.Cards[card.CardID], tag)
			}
		}
		if !hasTag || matches {
			pool = append(pool, candidate{slot: slot, trash: trash})
		}
		return nil
	}
	for index, slot := range player.DeckOrder {
		if err := add(slot, false); err != nil {
			return err
		}
		if index < len(player.Discard) {
			if err := add(player.Discard[index], true); err != nil {
				return err
			}
		}
	}
	for picked := 0; picked < count && len(pool) > 0; picked++ {
		index := int(engine.rng.next() % uint32(len(pool)))
		card := pool[index]
		if card.trash {
			for i, slot := range player.Discard {
				if slot == card.slot {
					player.Discard[i] = -1
					break
				}
			}
			for len(player.Discard) > 0 && player.Discard[len(player.Discard)-1] < 0 {
				player.Discard = player.Discard[:len(player.Discard)-1]
			}
		} else {
			for i, slot := range player.DeckOrder {
				if slot == card.slot {
					player.DeckOrder[i] = -1
					break
				}
			}
		}
		// 40130 shifts only as far as the first empty draw position. Repeated
		// insertions reverse pick order and preserve the remaining pool holes.
		empty := 0
		for player.DeckOrder[empty] >= 0 {
			empty++
		}
		copy(player.DeckOrder[1:empty+1], player.DeckOrder[:empty])
		player.DeckOrder[0] = card.slot
		player.DrawIndex, player.DrawCount = 0, len(player.DeckOrder)
		pool[index] = pool[len(pool)-1]
		pool = pool[:len(pool)-1]
	}
	return nil
}

func (engine *BattleEngine) executeCardSearch(action battleAction, role CombatSkillRole) ([]BattleResult, error) {
	for _, member := range engine.playerRoleTargets(action, role) {
		if err := engine.searchCards(engine.player(member), role); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// A searched-out trash card leaves its physical position empty. Future discards
// fill the first such position, matching the native collector's sparse array.
func (player *battlePlayer) discardCard(slot int) {
	// The alchemist collector removes used cards rather than storing them in
	// trash. CARD_SEARCH can retrieve only its remaining draw-pool cards.
	if player.Alchemist {
		return
	}
	for index, value := range player.Discard {
		if value < 0 {
			player.Discard[index] = slot
			return
		}
	}
	player.Discard = append(player.Discard, slot)
}
