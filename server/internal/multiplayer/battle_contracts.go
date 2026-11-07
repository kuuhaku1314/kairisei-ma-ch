package multiplayer

import (
	"fmt"

	"kairisei.local/server/battlecodec"
)

// ValidateBattleEngineContracts composes the pure engine/consumer checks with
// the room-only transport checks. The existing audit entry retains both gates.
func ValidateBattleEngineContracts(catalog *CombatCatalog) error {
	if err := ValidateCoreContracts(catalog, ContractResultCodec{Row: battlecodec.EncodeRow, Group: battlecodec.Encode}); err != nil {
		return err
	}
	return validateRoomResumeBarrierContract()
}

func validateRoomResumeBarrierContract() error {
	connections := make(map[int]*clientConn, maxRoomMembers)
	for memberType := 1; memberType <= maxRoomMembers; memberType++ {
		connections[memberType] = &clientConn{}
	}
	roomState := &room{
		connections: connections,
		gameStarted: true, turnPhaseStarted: true, userPhaseStarted: true,
		userAttackStarted: true, chaliceUserStarted: true, enemyPhaseStarted: true,
		enemyPhaseFinished: map[int]bool{2: true, 3: true, 4: true},
	}
	acknowledgement := comebackAcknowledgement(roomState)
	before := roomBarrierReady(roomState.connections, roomState.enemyPhaseFinished)
	roomState.enemyPhaseFinished[1] = true
	after := roomBarrierReady(roomState.connections, roomState.enemyPhaseFinished)
	if acknowledgement != "EnemyPhaseFinish" || before || !after {
		return fmt.Errorf("resume EnemyPhase barrier is acknowledgement=%q before=%t after=%t", acknowledgement, before, after)
	}
	return nil
}
