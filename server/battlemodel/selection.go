package battlemodel

// CardSelection contains the chosen combat actions. Automatic controls the
// original native action-order semantics; callers must not accept it from an
// untrusted wire field. Network timeout and connection state are not inputs.
type CardSelection struct {
	CardTypes    [5]int
	Targets      [5]int
	SphereSlot   int
	SphereTarget int
	// Bits refer to positions in CardTypes, not card IDs or hand indices.
	SummonsCardMask int
	// NPC result order: 1 inserts SPHR_PLAY before the first CARD_PLAY;
	// zero keeps the ordinary manual-input order (sphere after all cards).
	SpherePosition int
	Automatic      bool
}

func (selection CardSelection) CardCount() int {
	count := 0
	for _, cardType := range selection.CardTypes {
		if cardType != 0 {
			count++
		}
	}
	return count
}

func (selection CardSelection) ActionCount() int {
	count := selection.CardCount()
	if selection.SphereSlot != 0 {
		count++
	}
	return count
}

type BurstSelection struct {
	Target    int
	CardTypes [5]int
	SkillSlot int
}
