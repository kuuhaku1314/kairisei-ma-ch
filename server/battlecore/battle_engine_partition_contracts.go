package battlecore

import "fmt"

func validateBattlePartitionContracts(encodeBattleResult func(BattleResult) (string, error)) error {
	for _, result := range []BattleResult{
		{Command: CommandHoldSkillEnd},
		{Command: CommandAttackPartition},
	} {
		row, err := encodeBattleResult(result)
		if err != nil {
			return fmt.Errorf("zero-argument structural ResultCmd %d failed its boundary: %w", result.Command, err)
		}
		if row != fmt.Sprintf("%d", result.Command) {
			return fmt.Errorf("zero-argument structural ResultCmd %d encoded as %q", result.Command, row)
		}
	}

	nonTerminal := appendAttackPartition([]BattleResult{{Command: CommandCardSkill}})
	if len(nonTerminal) != 2 || nonTerminal[0].Command != CommandCardSkill || nonTerminal[1].Command != CommandAttackPartition {
		return fmt.Errorf("ordinary user-attack partition ordering is %+v", nonTerminal)
	}
	return nil
}
