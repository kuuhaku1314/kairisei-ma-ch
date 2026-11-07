package battlecore

import (
	"kairisei.local/server/battlecodec"
	"kairisei.local/server/battlecontent"
)

// Native receipt checks exercise the same production adapters without making
// the runtime core depend on filesystem loading or CSV framing.
func LoadCombatCatalog(card, root string) (*CombatCatalog, error) {
	definitions, err := battlecontent.LoadDefinitions(card, root)
	if err != nil {
		return nil, err
	}
	return NewCatalog(definitions)
}

func encodeBattleResult(row BattleResult) (string, error)     { return battlecodec.EncodeRow(row) }
func encodeBattleResults(rows []BattleResult) (string, error) { return battlecodec.Encode(rows) }
func encodeOptionalBattleResults(rows []BattleResult) (string, error) {
	return battlecodec.EncodeOptional(rows)
}

func testStartResult(members []Participant, rows []BattleResult) (string, error) {
	identities, err := DeckIdentityResults(members)
	if err != nil {
		return "", err
	}
	return battlecodec.EncodeStart(rows, identities)
}
