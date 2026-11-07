package battlecore

import (
	"errors"
	"math/bits"
)

var ErrSummonSlotsFull = errors.New("all three summoned member slots are occupied")

type battleSummon struct {
	Input      Summon
	MemberType int // Last spawned slot; original KO/replacement does not clear it.
	Executions uint8
}

func (engine *BattleEngine) validateSummonSelection(member int, selection CardSelection) error {
	if selection.SummonsCardMask == 0 {
		return nil
	}
	if member != 1 {
		return errors.New("summoning requires the alchemist Arthur")
	}
	free := 0
	for _, player := range engine.players[1:] {
		if player.MemberType == 0 || player.HP <= 0 {
			free++
		}
	}
	if bits.OnesCount32(uint32(selection.SummonsCardMask)) > free {
		return ErrSummonSlotsFull
	}
	for index, cardType := range selection.CardTypes {
		if selection.SummonsCardMask&(1<<index) == 0 {
			continue
		}
		if cardType < 1 || cardType > len(engine.summons) {
			return errors.New("summon source card is unavailable")
		}
		input := engine.summons[cardType-1].Input
		if input.ID == 0 || input.Card.CardID == 0 || input.Card.SkillLevels[0].ID == 0 {
			return errors.New("summon data and action card must be registered")
		}
	}
	return nil
}

func (engine *BattleEngine) createSelectedSummons() ([]BattleResult, error) {
	if !engine.alchemist {
		return nil, nil
	}
	var results []BattleResult
	selection := engine.selectedPlays[1]
	for index, cardType := range selection.CardTypes {
		if selection.SummonsCardMask&(1<<index) == 0 {
			continue
		}
		registration := &engine.summons[cardType-1]
		input := registration.Input
		if engine.livingSummonExists(input.ID) {
			continue
		}
		member := 0
		for i := 1; i < len(engine.players); i++ {
			if engine.players[i].MemberType == 0 || engine.players[i].HP <= 0 {
				member = i + 1
				break
			}
		}
		if member == 0 {
			return nil, ErrSummonSlotsFull
		}
		limit := initialParameterLimit(true)
		player := battlePlayer{Alchemist: true, SummonSource: cardType, SummonTurn: engine.turn,
			MemberType: member, HP: input.HP, MaxHP: input.HP, BaseMaxHP: input.HP,
			Attack: input.Attack, BaseAttack: input.Attack, Magic: input.Magic, BaseMagic: input.Magic,
			Recovery: input.Recovery, BaseRecovery: input.Recovery,
			Defense: input.Defense, BaseDefense: input.Defense, MDefense: input.MDefense, BaseMDefense: input.MDefense,
			BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", LimitAttack: limit, LimitMagic: limit, LimitRecovery: limit}
		player.Deck[0] = input.Card
		player.Deck[0].CardType = 1
		refreshPlayerBattleParameters(&player)
		player.HP = input.HP
		engine.players[member-1] = player
		delete(engine.selectedPlays, member)
		registration.MemberType = member
		registration.Executions++
		results = append(results, BattleResult{Command: CommandSummon, Args: []int64{int64(member), int64(cardType), int64(input.ID)}},
			CardIdentityResult(member, player.Deck[0]), playerBaseParameterResult(&player),
			BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(member, player.HP, player.MaxHP,
				player.Attack, player.Magic, player.Recovery, player.Defense, player.MDefense,
				player.LimitAttack, player.LimitMagic, player.LimitRecovery)})
		if input.Ability == 1 {
			results = append(results, BattleResult{Command: CommandCardDeal, Args: []int64{int64(member), 1, int64(player.Deck[0].Level)}})
			rows, err := engine.SubmitNPC(member, engine.enemyType, engine.catalog.NPC)
			if err != nil {
				return nil, err
			}
			results = append(results, rows...)
		}
	}
	return results, nil
}

// The native execution skips a repeated living summon ID without rejecting
// the submitted source card. A KO member does not block a later summon.
func (engine *BattleEngine) livingSummonExists(id int) bool {
	for _, player := range engine.players[1:] {
		if player.SummonSource != 0 && player.HP > 0 && engine.summons[player.SummonSource-1].Input.ID == id {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) SummonExecutionCounts() [10]int16 {
	var counts [10]int16
	for index, value := range engine.summons {
		counts[index] = int16(value.Executions)
	}
	return counts
}

func (engine *BattleEngine) memberTurnCost(player *battlePlayer) int {
	if player.SummonSource != 0 {
		return minInt(10, maxInt(0, engine.turn-player.SummonTurn))
	}
	return engine.turnCost()
}

func (player *battlePlayer) playableCardSlots() [5]int {
	if player.SummonSource != 0 {
		return [5]int{1}
	}
	return player.Hand
}

// ea3be looks up the registration by CARD_TYPE, then reads the associated
// member from the hand-slot index. Preserve those two distinct native indices.
func (engine *BattleEngine) summonConditionResults() []BattleResult {
	if !engine.alchemist {
		return nil
	}
	var results []BattleResult
	for index, cardType := range engine.players[0].Hand {
		if cardType < 1 || cardType > len(engine.summons) || engine.summons[cardType-1].Input.ID == 0 {
			continue
		}
		member := engine.summons[index].MemberType
		if member != 0 && engine.players[member-1].HP > 0 {
			continue
		}
		results = append(results, BattleResult{Command: CommandSummonCondition, Args: []int64{int64(cardType)}})
	}
	return results
}
