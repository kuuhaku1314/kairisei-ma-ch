package battlecore

import "errors"

// beginStoryEnd runs after a complete card/enemy skill, or at enemy-turn end.
// Script endings are presentation events, separate from ordinary win/loss.
func (engine *BattleEngine) beginStoryEnd(turnComplete bool) bool {
	if !engine.story {
		return false
	}
	order := engine.storyEnd
	match := turnComplete && order.Turn > 0 && engine.turn >= order.Turn
	if order.EnemyHPPercent > 0 {
		var hp, maximum int64
		for index := 0; index < engine.enemyCount; index++ {
			enemy := &engine.enemies[index]
			if enemy.Parent == 0 {
				hp += int64(enemy.HP)
				maximum += int64(enemy.MaxHP)
			}
		}
		match = match || maximum > 0 && hp*100/maximum <= int64(order.EnemyHPPercent)
	}
	match = match || order.UserHPZero && engine.players[0].MemberType != 0 && engine.players[0].HP <= 0
	if !match {
		return false
	}
	engine.endType = 3
	return true
}

func (engine *BattleEngine) finishStoryPhase(results []BattleResult, side int) ([]BattleResult, error) {
	engine.flushTokenStock()
	engine.resumeSide, engine.phase = side, PhaseEnded
	results = append(results, BattleResult{Command: CommandStoryEnd}, BattleResult{Command: CommandStoryEnd})
	return engine.appendTerminalBuffCleanup(results)
}

// Original 82280 (DESTRUCT) and 85d36 (ordinary attack) only preserve root bodies.
// Other HP producers retain their independent original commit rules.
func (engine *BattleEngine) applyStoryEnemyFloor(enemy *battleEnemy) {
	if engine.story && engine.storyEnd.EnemyHPPercent > 0 && enemy.Parent == 0 && enemy.HP <= 0 {
		enemy.HP = 1
	}
}

// GameEndResults is the original per-user read-only game_end_result query.
// A KO user reports ENEMY_WIN even when the presentation ended as STORY_END.
func (engine *BattleEngine) GameEndResults(member int) ([]BattleResult, error) {
	if !engine.playerMember(member) || engine.player(member).MemberType == 0 {
		return nil, errors.New("battle end result requires a registered player")
	}
	if engine.player(member).HP <= 0 {
		return []BattleResult{battleEndResult(2)}, nil
	}
	if engine.endType == 1 || engine.endType == 2 {
		return []BattleResult{battleEndResult(engine.endType)}, nil
	}
	return nil, nil
}
