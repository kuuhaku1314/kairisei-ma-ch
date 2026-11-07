package battlecore

import "errors"

// settlePlayerDeaths is the native 65010/61d60 boundary. A KO remains a
// participant while continuation is offered; only GameOver retires it.
func (engine *BattleEngine) settlePlayerDeaths(results []BattleResult) []BattleResult {
	// Original member_dead_end ends at the phase settlement boundary, after
	// the current enemy queue and GUTS processing. It does not retire the KO
	// member or offer continuation; all participants retain score eligibility.
	if engine.memberDeadEnd {
		for _, player := range engine.players {
			if player.MemberType != 0 && player.HP <= 0 {
				if engine.endType == 0 {
					engine.endType = 2
				}
				engine.continuePending = false
				return results
			}
		}
	}
	if engine.continueAllowed && (engine.endType == 0 || engine.endType == 2) {
		for _, player := range engine.players {
			if player.MemberType != 0 && player.HP <= 0 && !player.GameOver {
				engine.endType = 0
				engine.continuePending = true
				return append(results, BattleResult{Command: CommandContinueWait})
			}
		}
	}
	if !engine.gameOverByCaller {
		results = engine.appendNewPlayerGameOver(results)
	}
	// TEAMBATTLE waits for caller retirement (R110). EXPLORE/STORY settle
	// a bare all-KO defeat at this phase boundary when no Continue is offered.
	if (!engine.gameOverByCaller || engine.story || engine.explore) && engine.allPlayersDead() && engine.endType == 0 {
		engine.endType = 2
	}
	return results
}

// GameOver mirrors the explicit 228c0 retirement call. A KO and a retired
// participant are distinct; continuation decisions belong to the caller.
func (engine *BattleEngine) GameOver(member int) ([]BattleResult, error) {
	if !engine.playerMember(member) || engine.player(member).MemberType == 0 || engine.player(member).HP > 0 {
		return nil, errors.New("gameover member is unavailable")
	}
	engine.continuePending = false
	results := []BattleResult{engine.retireContinuedPlayer(engine.player(member))}
	// The PVP API retires just this member; the combined action boundary judges.
	if engine.pvp {
		return results, nil
	}
	for _, player := range engine.players {
		if player.MemberType != 0 && !player.GameOver {
			return results, nil
		}
	}
	engine.endType, engine.phase = 2, PhaseEnded
	return append(results, battleEndResult(2)), nil
}

func (engine *BattleEngine) CancelContinue() []BattleResult {
	// The managed cancellation calls GameOver for each waiting member. Preserve
	// that API's reservation snapshot until the normal chalice cleanup boundary.
	var results []BattleResult
	for index := range engine.players {
		player := &engine.players[index]
		if player.MemberType != 0 && player.HP <= 0 && !player.GameOver {
			results = append(results, engine.retireContinuedPlayer(player))
		}
	}
	engine.continuePending = false
	if engine.allPlayersDead() {
		engine.endType, engine.phase = 2, PhaseEnded
		results = append(results, battleEndResult(2))
	}
	return results
}

func (engine *BattleEngine) retireContinuedPlayer(player *battlePlayer) BattleResult {
	player.GameOver = true
	player.BlessHolds, player.CardHolds = nil, nil
	delete(engine.selectedPlays, player.MemberType)
	// R109 original CN/JP GameOver -> ContinueDataGet retains 318 reservation.
	// HP remains zero, so it cannot execute or consume a sphere use.
	return battleGameOverResult(player.MemberType)
}

func (engine *BattleEngine) applyContinue(plan *continuePlanState) {
	engine.players = plan.players
	engine.continuePending, engine.forceEndCheck, engine.endType = false, false, 0
	engine.continueCount++
}

// ContinuePlan is an opaque, single-use revival prepared for one battle. The
// caller must keep that battle paused until it commits or discards this plan.
// Preparing/encoding a payment must not expose revival or consume battle RNG.
// Copies of the handle share the same commit state.
type ContinuePlan struct{ state *continuePlanState }

type continuePlanState struct {
	owner   *BattleEngine
	players [maxRoomMembers]battlePlayer
	results []BattleResult
}

func (plan *ContinuePlan) Results() []BattleResult {
	if plan == nil || plan.state == nil {
		return nil
	}
	rows := make([]BattleResult, len(plan.state.results))
	for i, result := range plan.state.results {
		rows[i] = BattleResult{Command: result.Command, Args: append([]int64(nil), result.Args...)}
	}
	return rows
}

// ResumeResults validates the post-revival snapshot before an external payment.
// It does not commit revival, increment the live continue count or draw cards.
func (plan *ContinuePlan) ResumeResults(priorDrops ...BattleDrop) ([]BattleResult, error) {
	if plan == nil || plan.state == nil || plan.state.owner == nil {
		return nil, errors.New("combat continue plan is closed")
	}
	view := *plan.state.owner
	view.applyContinue(plan.state)
	return view.ResumeResults(priorDrops...)
}

// Commit applies the prepared revival once, to its original battle. The
// adapter owns payment and must freeze progression while payment is pending.
func (plan *ContinuePlan) Commit() error {
	if plan == nil || plan.state == nil || plan.state.owner == nil {
		return errors.New("combat continue plan is closed")
	}
	plan.state.owner.applyContinue(plan.state)
	plan.state.owner = nil
	return nil
}

// PrepareContinue mirrors x86 battle5_api_continue -> 65cec. Eligibility and
// payment belong to the room; this operation only prepares the same-side
// revival. Proof: cn602-d446-continue-static-20260907 (static original ELF).
func (engine *BattleEngine) PrepareContinue(memberType int) (*ContinuePlan, error) {
	if engine == nil || memberType < 1 || memberType > maxRoomMembers ||
		engine.players[memberType-1].MemberType != memberType || engine.players[memberType-1].GameOver {
		return nil, errors.New("continue member is unavailable")
	}
	plan := &continuePlanState{owner: engine, players: engine.players}
	for index := range plan.players {
		player := &plan.players[index]
		if player.MemberType == 0 || player.GameOver {
			continue
		}
		oldBlocked := engine.playerEffectValue(player, "COST_BLOCK")
		if player.HP <= 0 {
			plan.results = append(plan.results, BattleResult{Command: CommandRevive, Args: []int64{int64(player.MemberType)}})
		}
		// 73302(2), 73302(5), 7339f(2), 7339f(5). Each nonempty
		// category has its own natural expiry and parameter projection.
		for _, listType := range [...]int{0, 2} {
			for _, category := range [...]int{2, 5} {
				plan.results = appendContinueRelease(plan.results, player, listType, category)
			}
		}
		newBlocked := engine.playerEffectValue(player, "COST_BLOCK")
		if engine.phase == PhaseTurn {
			// No selection has occurred in this turn; removing the block restores
			// the current turn's full baseline cost.
			player.Cost = maxInt(0, engine.turnCost()-newBlocked)
		} else if oldBlocked != newBlocked {
			// COST_BLOCK is subtracted by the managed cost getter. Removing it
			// restores available cost while retaining already spent cost.
			player.Cost = maxInt(0, minInt(engine.turnCost(), player.Cost+oldBlocked-newBlocked))
		}
		// 42ae4(kind=0) removes CURSE while preserving BLESS, card holds,
		// submitted cards, costs and Sphere usage.
		kept := make([]battleBlessHold, 0, len(player.BlessHolds))
		for _, hold := range player.BlessHolds {
			if hold.CardType == 21 {
				plan.results = append(plan.results, blessHoldLostResult(player.MemberType, hold))
			} else {
				kept = append(kept, hold)
			}
		}
		player.BlessHolds = kept
		// JP Continue removes card-bound NEED_COST_UP with the other debuffs.
		player.CardCostIncreases = [33][]battleCardCostIncrease{}
		player.HP = player.MaxHP
		plan.results = append(plan.results, BattleResult{Command: CommandHP, Args: []int64{
			int64(player.MemberType), int64(player.MaxHP), int64(player.HP), 3,
		}})
		// +59ab0 is consumed by 5cecc at the next draw: refill the hand,
		// overriding ordinary draw bonuses/penalties, without drawing now.
		player.ContinueDraw = true
	}
	plan.results = append(plan.results, BattleResult{Command: CommandContinue, Args: []int64{int64(memberType)}})
	return &ContinuePlan{state: plan}, nil
}

func appendContinueRelease(results []BattleResult, player *battlePlayer, listType, category int) []BattleResult {
	var removed []battleEffect
	kept := make([]battleEffect, 0, len(player.Effects))
	for _, effect := range player.Effects {
		code, known := battleBuffCodes[effect.Function]
		if known && effect.ListType == listType && battleBuffKind(code) == category {
			removed = append(removed, effect)
		} else {
			kept = append(kept, effect)
		}
	}
	if len(removed) == 0 {
		return results
	}
	ensurePlayerBaseParameters(player)
	for _, effect := range removed {
		revertPlayerEffect(player, effect)
	}
	player.Effects = kept
	refreshPlayerBattleParameters(player)
	refreshPlayerAttribute(player)
	results = appendNaturalExpiryRows(results, player.MemberType, removed, kept, player.Attribute)
	return append(results, BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(player.MemberType,
		player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery, player.Defense, player.MDefense,
		player.LimitAttack, player.LimitMagic, player.LimitRecovery)})
}
