package battlecore

// 5dd72 is called by TurnPhase, before enemy charge/AI checks. Hand state
// must already be authoritative while waiting for TurnPhaseFinish; only the
// visible DEAL notifications wait for UserPhase. Preserve empty slot order.
func (engine *BattleEngine) prepareTurnDraw() {
	engine.turnDrawn = [8][5]bool{}
	for index, player := range engine.playerStates() {
		if player.SummonSource != 0 {
			engine.turnDrawn[index][0] = true
		}
		drawLimit := 1
		if engine.initialDealPending {
			drawLimit = 0
			for _, slot := range player.Hand {
				if slot == 0 {
					drawLimit++
				}
			}
		}
		drawLimit += playerDrawEffectValue(player, battleBuffCodes["DEAL_BONUS"]) - playerDrawEffectValue(player, battleBuffCodes["DEAL_PENALTY"])
		if player.ContinueDraw {
			// 5cecc replaces the effect-derived bonus with 48974's empty
			// slot count. The base draw cannot exceed the five-slot hand.
			drawLimit = len(player.Hand)
			player.ContinueDraw = false
		}
		drawLimit = maxInt(0, drawLimit)
		if player.HP <= 0 {
			continue
		}
		poolPrepared := false
		for slot := range player.Hand {
			if drawLimit == 0 {
				break
			}
			if player.Hand[slot] != 0 {
				continue
			}
			if !poolPrepared && engine.alchemist && player.remainingDeckCount() == 0 {
				// Alchemist prepares one empty collector per draw batch. Its
				// ten null slots still consume nine shuffle words, once even
				// when several hand positions are empty. Trash is not returned.
				engine.shuffleDeck(player)
				engine.notifyDeckOrder(player, false)
			}
			poolPrepared = true
			player.Hand[slot] = engine.drawCard(player)
			if player.Hand[slot] != 0 {
				engine.turnDrawn[index][slot] = true
				drawLimit--
			}
		}
	}
	engine.initialDealPending = false
}
