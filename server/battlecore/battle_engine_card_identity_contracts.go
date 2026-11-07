package battlecore

import (
	"errors"
	"fmt"
)

func validateBattleCardIdentityContracts(encodeBattleResult func(BattleResult) (string, error)) error {
	row, err := encodeBattleResult(CardIdentityResult(2, BattleCard{CardType: 3, CardID: 10131011, Level: 60}))
	if err != nil {
		return err
	}
	if row != "23,2,3,10131011,60" {
		return fmt.Errorf("card identity projection is %q", row)
	}
	if _, boundaryErr := encodeBattleResult((BattleResult{
		Command: CommandCardIdentity,
		Args:    []int64{2, 3, 10131011},
	})); boundaryErr == nil {
		return errors.New("card identity accepted a short row")
	}
	return nil
}
