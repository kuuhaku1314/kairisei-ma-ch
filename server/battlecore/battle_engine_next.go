package battlecore

import (
	"errors"
	"fmt"
)

// NextBattle uses the configured takeover policy, shared with registered input.
// Default TEAMBATTLE/STORY reset all six fields, including awakened encounters.
// Return a separate engine so failure cannot advance a live room.
func (engine *BattleEngine) NextBattle(partyID int, drops []BattleDrop) (*BattleEngine, error) {
	return engine.nextBattle(engine.awakeTakeover, func(next *BattleEngine) error {
		party, exists := engine.catalog.EnemyParties[partyID]
		if !exists {
			return fmt.Errorf("next enemy party %d is unavailable", partyID)
		}
		return next.loadEnemyParty(party, drops)
	})
}

// NextEncounter uses the same handover for clients that register the next
// enemy's complete data. The caller owns the catalog snapshot with those
// skills; shared masters and the current-wave engine remain unchanged.
func (engine *BattleEngine) NextEncounter(catalog *CombatCatalog, encounter Encounter, policy WaveTakeover) (*BattleEngine, error) {
	if catalog == nil {
		return nil, errors.New("next encounter requires a combat catalog")
	}
	return engine.nextBattle(policy, func(next *BattleEngine) error {
		next.catalog = catalog
		return next.loadEncounter(encounter)
	})
}

func (engine *BattleEngine) nextBattle(policy WaveTakeover, load func(*BattleEngine) error) (*BattleEngine, error) {
	if engine.phase != PhaseEnded || (engine.endType != 1 && engine.endType != 4) {
		return nil, errors.New("next battle requires a completed winning/awake wave")
	}
	next := *engine
	if err := load(&next); err != nil {
		return nil, err
	}
	next.awakeTakeover = policy
	next.turn = 0
	if policy.KeepTurn {
		next.turn = engine.turn
	}
	// Cost has its own turn coordinate. Start retains its current value;
	// the following TurnPhase advances it even if the visible turn reset.
	next.costTurnOffset = -next.turn
	if policy.KeepCost {
		next.costTurnOffset = engine.costTurnOffset + engine.turn - next.turn
	}
	next.enemyStartTurn = next.turn
	next.elapsedWaveTurns += engine.turn - next.turn
	next.endType = 0
	next.initialDealPending = !policy.KeepDeal || next.turn == 0
	next.resumeSide, next.turnDrawn = 0, [8][5]bool{}
	next.phase = PhaseCreated
	next.selectedPlays = make(map[int]cardPlaySubmission)
	next.enemyUses = make(map[int]int)
	next.enemyTriggerTarget, next.turnStats, next.turnActions = 0, battleTurnStats{}, nil
	next.damageHistory = [29][]battleDamageEvent{}
	next.forceEndCheck = false
	next.waveRecovery = nil
	for index := range next.players {
		player := &next.players[index]
		// Isolate retained state before the next wave mutates its durations.
		// Default handover also removes any unresolved KO member effects.
		if len(player.Effects) != 0 {
			player.Effects = append([]battleEffect(nil), player.Effects...)
			if !policy.KeepBuff {
				_ = appendDefaultWavePlayerCleanup(nil, player, false)
			}
		}
		if !player.GameOver && player.HP <= 0 {
			player.HP = 1
			next.waveRecovery = append(next.waveRecovery, BattleResult{Command: CommandHP,
				Args: []int64{int64(player.MemberType), int64(player.MaxHP), 1, 0}})
		}
		player.Cost = next.turnCost()
		player.DamageTaken, player.TurnDamage = 0, 0
		// Native 6f70b clears per-turn statistics, including executed CALL
		// records, at the incoming TurnPhase. Start/resume see prior values.
		// 62af0 skips a7af6 during awakening, preserving each card's played
		// flag together with the ongoing battle. Ordinary new waves reset it.
		if engine.endType != 4 {
			player.PlayedCardTypes = [11]bool{}
		}
		player.BlessHolds, player.CardHolds, player.ReservedChalice = nil, nil, 0
		if !policy.KeepBuff {
			for slot := 0; slot < tokenCardFirst; slot++ {
				player.CardDisplay[slot] = battleDisplayPower{}
			}
			player.CardCostDown = [33]int{}
			player.CardBurstSkills = [33][]battleCardBurstSkill{}
			player.CardCostIncreases = [33][]battleCardCostIncrease{}
		} else {
			for slot := range player.CardBurstSkills {
				player.CardBurstSkills[slot] = append([]battleCardBurstSkill(nil), player.CardBurstSkills[slot]...)
				player.CardCostIncreases[slot] = append([]battleCardCostIncrease(nil), player.CardCostIncreases[slot]...)
			}
		}
		for slot := range player.Spheres {
			if !policy.KeepSphereCount {
				player.Spheres[slot].Count = player.Spheres[slot].Maximum
			}
			player.Spheres[slot].Playable = false
			player.Spheres[slot].ChalicePlayable = false
			player.Spheres[slot].Remaining = 0
		}
		if policy.KeepDeckTrash {
			// Recycling here mixes used cards into the not-yet-drawn cards,
			// allowing duplicates before the first ten slots have all appeared.
			player.Discard = append([]int(nil), player.Discard...)
		} else {
			next.recycleDrawPool(player)
		}
	}
	return &next, nil
}
