package battlecore

import (
	"errors"
	"fmt"
)

func validateBattleDropContracts(encodeBattleResult func(BattleResult) (string, error)) error {
	gold := BattleDrop{EnemyIndex: 0, RewardType: 4, Num: 100}
	card := BattleDrop{EnemyIndex: 0, RewardType: 6, Num: 1, RewardTypeID: 10002001}

	parent := &battleEnemy{MemberType: 5, HP: 0, Drops: []BattleDrop{gold, card}}
	engine := &BattleEngine{turn: 3}
	results, err := engine.resolveEnemyDeath(nil, parent)
	if err != nil {
		return err
	}
	if len(results) != 3 || results[0].Command != CommandEnemyBreak ||
		results[1].Command != CommandEnemyBreakDrop || results[2].Command != CommandEnemyBreakDrop ||
		results[1].Args[0] != 5 || results[1].Args[2] != 4 || results[1].Args[3] != 100 ||
		results[2].Args[2] != 6 || results[2].Args[3] != 1 || results[2].Args[4] != 10002001 ||
		!parent.DropReleased {
		return fmt.Errorf("enemy break drop projection is results=%+v enemy=%+v", results, parent)
	}
	if repeated, repeatErr := engine.resolveEnemyDeath(nil, parent); repeatErr != nil || len(repeated) != 0 {
		return fmt.Errorf("enemy break drop repeated: results=%+v error=%v", repeated, repeatErr)
	}

	part := &battleEnemy{MemberType: 6, Parent: 1, HP: 0, Drops: []BattleDrop{card}}
	partResults, err := engine.resolveEnemyDeath(nil, part)
	if err != nil {
		return err
	}
	if len(partResults) != 2 || partResults[0].Command != CommandPartsBreak || partResults[1].Command != CommandPartsBreakDrop {
		return fmt.Errorf("parts break drop projection is %+v", partResults)
	}

	resume := resumeEnemyDropResults(parent)
	if len(resume) != 2 || resume[0].Command != CommandResumeEnemyDrop || len(resume[0].Args) != 5 ||
		resume[0].Args[0] != 5 || resume[0].Args[2] != 4 || resume[0].Args[3] != 100 || resume[0].Args[4] != 0 {
		return fmt.Errorf("resume enemy drop projection is %+v", resume)
	}
	if len(resumeEnemyDropResults(&battleEnemy{MemberType: 5, Drops: []BattleDrop{gold}})) != 0 {
		return errors.New("unreleased enemy drop leaked into comeback projection")
	}

	for _, malformed := range []BattleResult{
		{Command: CommandPartsBreakDrop, Args: []int64{6, 0, 6, 1}},
		{Command: CommandEnemyBreakDrop, Args: []int64{5, 0, 4, 100}},
		{Command: CommandResumeEnemyDrop, Args: []int64{5, 0, 4, 100}},
	} {
		if _, boundaryErr := encodeBattleResult(malformed); boundaryErr == nil {
			return fmt.Errorf("drop command %d accepted a short row", malformed.Command)
		}
	}
	return nil
}
