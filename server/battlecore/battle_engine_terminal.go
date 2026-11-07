package battlecore

// Default TEAMBATTLE has no AwakeTakeOver buff flag. The x86 EAX argument
// omitted by Ghidra's decompile selects 61d60 -> a7bac -> 7233e, NOT 5eb20.
// Clear player lists NORMAL/PASSIVE/FIELD/CARD (including KO users), retain
// EVENT and BURST lists, emit grouped 72 plus 5/6, and tick burst counters.
// Ordinary expiry (72a18) is a different path that also emits 70/71.
func (engine *BattleEngine) appendTerminalBuffCleanup(results []BattleResult) ([]BattleResult, error) {
	// TEAMBATTLE caller retirement via 228c0 -> 661f3 has no wave cleanup.
	// EXPLORE/STORY phase-owned defeat uses the normal terminal cleanup.
	if engine.endType == 2 {
		if !engine.memberDeadEnd && !engine.story && !engine.explore {
			return results, nil
		}
		// Defeat clears terminal state even when a successful wave would
		// retain buffs. JP EXPLORE does not run ordinary expiry here.
		return engine.appendWaveBuffCleanup(results), nil
	}
	if engine.awakeTakeover.KeepBuff {
		rows, err := engine.appendEffectTurn(results)
		if err != nil {
			return nil, err
		}
		return engine.appendWaveHoldCleanup(rows), nil
	}
	return engine.appendWaveBuffCleanup(results), nil
}

// Limit-turn termination uses wave cleanup even with native ENEMY_WIN;
// ordinary party defeat above deliberately skips it.
func (engine *BattleEngine) appendWaveBuffCleanup(results []BattleResult) []BattleResult {
	for _, player := range engine.playerStates() {
		if player.MemberType == 0 {
			continue
		}
		results = appendDefaultWavePlayerCleanup(results, player, engine.pvp)
	}
	engine.tickBurstCounters()
	results = engine.appendWaveHoldCleanup(results)
	if engine.pvp {
		// PVP publishes the retained BURST_NORMAL parameters in the terminal
		// rows, then discards that temporary list without extra release rows.
		for _, player := range engine.playerStates() {
			kept := player.Effects[:0]
			for _, effect := range player.Effects {
				if effect.ListType == 5 {
					revertPlayerEffect(player, effect)
				} else {
					kept = append(kept, effect)
				}
			}
			player.Effects = kept
			refreshPlayerBattleParameters(player)
			refreshPlayerAttribute(player)
		}
	}
	return results
}

func (engine *BattleEngine) appendWaveHoldCleanup(results []BattleResult) []BattleResult {
	// 61d60 -> a7c08 -> 422c0 removes remaining holds after all members'
	// buff cleanup; 42ae4 retains each BLESS/CURSE append index.
	for _, player := range engine.playerStates() {
		for _, hold := range player.CardHolds {
			player.releaseHeldCard(hold.Action, true)
			results = append(results, cardHoldLostResult(hold.Action))
		}
		player.CardHolds = nil
		// a7c08 calls 42ae4(kind=0) then kind=1: CURSE before BLESS,
		// retaining vector order within each kind regardless of insertion order.
		for _, cardType := range [...]int{21, 22} {
			for _, hold := range player.BlessHolds {
				if hold.CardType == cardType {
					results = append(results, blessHoldLostResult(player.MemberType, hold))
				}
			}
		}
		player.BlessHolds = nil
	}
	return results
}

func appendDefaultWavePlayerCleanup(results []BattleResult, player *battlePlayer, pvp bool) []BattleResult {
	ensurePlayerBaseParameters(player)
	kept := make([]battleEffect, 0, len(player.Effects))
	var removed []battleEffect
	eventCodes := make(map[int]bool)
	for _, effect := range player.Effects {
		if effect.ListType == 2 || effect.ListType == 5 || effect.ListType == 6 {
			kept = append(kept, effect)
			if effect.ListType == 2 {
				eventCodes[battleBuffCodes[effect.Function]] = true
			}
		} else {
			removed = append(removed, effect)
			revertPlayerEffect(player, effect)
		}
	}
	player.Effects = kept
	refreshPlayerBattleParameters(player)
	refreshPlayerAttribute(player)
	released := make(map[int]bool)
	parameterMasks := make(map[int]int)
	attributeMasks := make(map[int]int)
	for _, effect := range removed {
		code, ok := battleBuffCodes[effect.Function]
		if !ok || effect.ListType != 0 || eventCodes[code] {
			continue
		}
		key, row := naturalEffectReleaseResult(player.MemberType, effect, code)
		// Attribute defense retains a separate release for each attribute;
		// other ordinary statuses group by code and overlapping parameter bits.
		parameterFlags, attributeFlags := key[1], key[2]
		separateAttributes := pvp || code == 19 || code == 113
		if !released[code] || parameterFlags != 0 && parameterMasks[code]&parameterFlags == 0 || separateAttributes && parameterFlags == 0 && attributeFlags != 0 && attributeMasks[code]&attributeFlags == 0 {
			results = append(results, row)
			released[code] = true
			parameterMasks[code] |= parameterFlags
			attributeMasks[code] |= attributeFlags
		}
	}
	return append(results,
		playerBaseParameterResult(player),
		BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(player.MemberType, player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery, player.Defense, player.MDefense, player.LimitAttack, player.LimitMagic, player.LimitRecovery)},
	)
}
