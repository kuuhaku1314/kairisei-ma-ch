package battlecore

// ValidBattleDrop keeps the HTTP preview, BattleSv start gate and in-memory
// engine on the same subset of RewardInfo that can represent a tangible enemy
// drop. Account-only EXP and other presentation-only rewards stay in HTTP
// settlement and never become break-drop rows.
func ValidBattleDrop(drop BattleDrop) bool {
	if drop.EnemyIndex < 0 || drop.Num <= 0 {
		return false
	}
	switch drop.RewardType {
	case 4, 10, 12: // GOLD, COIN, BP
		return drop.RewardTypeID == 0
	case 6, 8, 13, 15, 19: // CARD, ITEM, CARD_STACK, SPHR, BUDDY
		return drop.RewardTypeID > 0
	default:
		return false
	}
}

// breakDropResult uses the five-column layout shared by PARTS_BREAK_DROP and
// ENEMY_BREAK_DROP in BattleResultCmdFunctionBase:
// member, zero-based drop slot, reward type, quantity, reward type ID.
// D-329's two-drop original API capture proves the second field is not reserved.
func breakDropResult(command int, memberType int, slot int, drop BattleDrop) BattleResult {
	return BattleResult{Command: command, Args: []int64{
		int64(memberType), int64(slot), int64(drop.RewardType), int64(drop.Num), int64(drop.RewardTypeID),
	}}
}

// enemyBreakDropResults is one-shot even if a native REVIVE role later makes
// the same part targetable again. The local reward profile is granted once;
// reconnect projects the retained DropData with ResultCmd108 instead of
// granting the configured rewards again.
func enemyBreakDropResults(enemy *battleEnemy) []BattleResult {
	if enemy == nil || enemy.DropResolved {
		return nil
	}
	enemy.DropResolved = true
	if len(enemy.Drops) == 0 {
		return nil
	}
	command := CommandEnemyBreakDrop
	if enemy.Parent > 0 {
		command = CommandPartsBreakDrop
	}
	results := make([]BattleResult, 0, len(enemy.Drops))
	for slot, drop := range enemy.Drops {
		results = append(results, breakDropResult(command, enemy.MemberType, slot, drop))
	}
	enemy.DropReleased = true
	return results
}

// resumeEnemyDropResults mirrors the managed ResultCmd108 consumer. It
// restores current-wave DropData and marks it direct. Prior-wave rewards are
// supplied separately to ResumeResults, just like the original API's vector.
func resumeEnemyDropResults(enemy *battleEnemy) []BattleResult {
	if enemy == nil || !enemy.DropReleased || len(enemy.Drops) == 0 {
		return nil
	}
	results := make([]BattleResult, 0, len(enemy.Drops))
	for slot, drop := range enemy.Drops {
		results = append(results, breakDropResult(CommandResumeEnemyDrop, enemy.MemberType, slot, drop))
	}
	return results
}
