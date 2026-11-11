package gamestate

const (
	BattleOpeningCostMin         = 3
	BattleOpeningCostMax         = 10
	BattleOpeningBurstGaugeReady = 100
	BattleOpeningBurstGaugeMax   = 300
)

// TeamBattleOpening is the accepted starting state for all party members.
type TeamBattleOpening struct {
	CostInitial       int `json:"cost_initial"`
	BurstGaugeInitial int `json:"burst_gauge_initial"`
}

func CloneTeamBattleOpening(source *TeamBattleOpening) *TeamBattleOpening {
	if source == nil {
		return nil
	}
	value := *source
	return &value
}
