package multiplayer

import "errors"

// The managed client opens this input during UserAttack/EnemyPhase direction,
// broadcasts 318 immediately, then closes it BEFORE sending the phase Finish.
// Engine results are computed ahead of that direction. Keep the published
// reservation window separate from the engine's future HP/end/cleanup state.
type roomChaliceInput struct {
	phase    Phase
	playable [maxRoomMembers][deckSphereSlots]bool
	reserved [maxRoomMembers]int
}

func (current *room) openUserChaliceInput(results []BattleResult) {
	current.chaliceInput = roomChaliceInput{phase: PhaseUserAttack}
	// The client extracts 319 at the START of ApiUserAttack, even if that
	// result stream ends in victory. Use those exact published slots.
	for _, result := range results {
		if result.Command == CommandChaliceSpherePlayable && len(result.Args) == 2 {
			member, slot := int(result.Args[0]), int(result.Args[1])
			if member >= 1 && member <= maxRoomMembers && slot >= 1 && slot <= deckSphereSlots {
				current.chaliceInput.playable[member-1][slot-1] = true
			}
		}
	}
}

func (current *room) openEnemyChaliceInput() {
	current.chaliceInput = roomChaliceInput{phase: PhaseEnemy}
	// Capture BEFORE EnemyPhase changes HP, usage or battle outcome.
	current.chaliceInput.playable = current.engine.ChaliceSlots()
}

func (current *room) reserveChaliceInput(memberType, slot int) BattleResult {
	input := &current.chaliceInput
	open := input.phase == PhaseUserAttack && !current.userAttackFinished[memberType] ||
		input.phase == PhaseEnemy && !current.enemyPhaseFinished[memberType]
	if open && (slot == 0 || input.playable[memberType-1][slot-1]) {
		input.reserved[memberType-1] = slot
	}
	return BattleResult{Command: CommandChaliceSphereReserve, Args: []int64{int64(memberType), int64(input.reserved[memberType-1])}}
}

func (current *room) commitChaliceInput() error {
	// Called only after every live client has finished the direction and only
	// when combat continues. Ended battles never commit these UI intentions.
	for index, slot := range current.chaliceInput.reserved {
		if slot == 0 {
			continue
		}
		_, err := current.engine.ReserveChaliceSphere(index+1, slot)
		if err != nil && !errors.Is(err, ErrChaliceSphereUnavailable) {
			return err
		}
	}
	current.chaliceInput = roomChaliceInput{}
	return nil
}

func (current *room) chaliceResumeResults(results []BattleResult) []BattleResult {
	// Engine ResumeResults deliberately sees committed execution state. Restore
	// the room's separately broadcast intentions after that snapshot, too.
	if current.chaliceInput.phase != 0 {
		for index, slot := range current.chaliceInput.reserved {
			results = append(results, BattleResult{Command: CommandChaliceSphereReserve, Args: []int64{int64(index + 1), int64(slot)}})
		}
	}
	return results
}
