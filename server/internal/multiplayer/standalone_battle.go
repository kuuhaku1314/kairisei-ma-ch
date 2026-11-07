package multiplayer

import (
	"kairisei.local/server/battlecodec"
	"kairisei.local/server/battlecore"
)

type StandaloneBattleConfig = battlecore.StandaloneBattleConfig
type StandaloneBattleMember = battlecore.StandaloneBattleMember
type StandaloneCardPlay = battlecore.StandaloneCardPlay
type StandaloneBattle = battlecore.StandaloneBattle

func NewStandaloneBattle(catalog *CombatCatalog, config StandaloneBattleConfig, members []StandaloneBattleMember) (*StandaloneBattle, error) {
	return battlecore.NewStandaloneBattle(catalog, config, members)
}
func StandaloneResultCSV(results []BattleResult) (string, error) {
	return battlecodec.EncodeOptional(results)
}
