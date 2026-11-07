package multiplayer

import (
	"kairisei.local/server/battlecontent"
	"kairisei.local/server/battlecore"
)

// LoadCombatCatalog is the filesystem adapter. Runtime catalog queries do not
// depend on file loading; constructed definitions are validated before use.
func LoadCombatCatalog(cardMasterPath, battleMasterRoot string) (*CombatCatalog, error) {
	definitions, err := battlecontent.LoadDefinitions(cardMasterPath, battleMasterRoot)
	if err != nil {
		return nil, err
	}
	return battlecore.NewCatalog(definitions)
}
