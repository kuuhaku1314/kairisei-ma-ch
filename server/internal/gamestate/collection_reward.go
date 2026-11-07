package gamestate

// Native reward IDs: COSTUME=14, STAMP=16, HONOR=18. Definitions are public;
// ownership belongs to each account's Costume, Stamps and Honors collections.
type CollectionRewardDefinition struct {
	Type      int
	ID        int
	Name      string
	Detail    string
	PictID    int
	StampKind string
}

// Next instance IDs never move backwards when the highest inventory entry is
// sold or consumed. Zero initializes older accounts from their current rows.
type InventorySequenceState struct {
	Card   int64 `json:"card"`
	Sphere int64 `json:"sphere"`
	Buddy  int64 `json:"buddy"`
}

func IsCollectionReward(kind int) bool { return kind == 14 || kind == 16 || kind == 18 }

func (state *State) SetCollectionRewardDefinitions(kind int, definitions []CollectionRewardDefinition) {
	result := make([]CollectionRewardDefinition, 0, len(state.CollectionRewards)+len(definitions))
	for _, definition := range state.CollectionRewards {
		if definition.Type != kind {
			result = append(result, definition)
		}
	}
	state.CollectionRewards = append(result, definitions...)
}
