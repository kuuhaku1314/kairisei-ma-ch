package battlecore

// Existing effect-math contracts inspect expiry's visible grouped changes.
// Unconditional parameter/status snapshots and per-entry expiry notifications have
// their own exact wire-order tests in battle_lifecycle_test.go. Do not use
// this projection for runtime output or claim it is a native phase transcript.
func expireForEffectContract(engine *BattleEngine) ([]BattleResult, error) {
	rows, err := engine.tickPersistentEffects()
	if err != nil {
		return nil, err
	}
	changes := make([]BattleResult, 0, len(rows))
	for _, row := range rows {
		if row.Command != CommandBattleParam && row.Command != CommandBuffLostOne &&
			row.Command != CommandEnemyDebuff && row.Command != CommandFixedAttrDefense {
			changes = append(changes, row)
		}
	}
	return changes, nil
}

// Advance compact full-battle simulations through the native enemy-tail API
// before opening another turn. Returned rows span both APIs, not one packet.
func nextTurnForBattleContract(engine *BattleEngine) ([]BattleResult, error) {
	var rows []BattleResult
	if engine.phase == PhaseEnemy {
		var err error
		rows, err = engine.ExecuteChaliceEnemyPhase()
		if err != nil {
			return nil, err
		}
	}
	next, err := engine.TurnPhase()
	return append(rows, next...), err
}
