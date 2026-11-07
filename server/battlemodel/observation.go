package battlemodel

// CombatantView is a value snapshot for observers, with no references to live
// effects, decks or enemy state. Names and other presentation data stay outside.
type CombatantView struct {
	MemberType int
	ArthurType int
	EnemyID    int
	HP         int
	MaxHP      int
	Attack     int
	Magic      int
	BuffCount  int
	Cost       int
	Flags      uint32
	Variables  [5]int32
}

type StateView struct {
	Turn    int
	EndType int
	Ended   bool
	Members []CombatantView
}

// DeckOrderChange reports a completed shuffle by value. Opening distinguishes
// the initial deal from recycling a depleted draw pool; it is not a wire opcode.
type DeckOrderChange struct {
	State      StateView
	MemberType int
	Order      [10]int
	Opening    bool
}
