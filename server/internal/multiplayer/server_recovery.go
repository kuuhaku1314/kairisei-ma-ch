package multiplayer

import "time"

const recoveryProtocolVersion = 2

// Recovery capability is advertised in the existing room-info tail. Legacy
// four-field Comeback requests retain their original response sequence.
func validRecoveryToken(committed, pending, presented string) bool {
	return committed != "" && (presented == committed || pending != "" && presented == pending)
}

func (c *clientConn) prepareRecoveryToken(tokens *map[int]string) (string, error) {
	if c.recoveryVersion != recoveryProtocolVersion {
		return newComebackToken()
	}
	if *tokens == nil {
		*tokens = make(map[int]string)
	}
	token := (*tokens)[c.memberType]
	if token == "" {
		var err error
		token, err = newComebackToken()
		if err != nil {
			return "", err
		}
		(*tokens)[c.memberType] = token
	}
	c.recoveryToken = token
	c.recoveryApplied = false
	return token, nil
}

// No engine operation is replayed to reopen the UI. This is the input
// permission at the same room-lock boundary as the authoritative snapshot.
func recoveryStateFrame(current *room, memberType int, token string) battleFrame {
	input := current.gameStarted && current.userPhaseStarted && !current.userAttackStarted &&
		!current.nextBattlePending && current.continuation == nil && current.engineBattleEnd == 0
	_, queued := current.cardPlaySubmissions[memberType]
	submitted := queued || current.engine.HasSubmission(memberType)
	canInput := input && !submitted && current.engine.MemberAlive(memberType)
	return battleFrame{"RoomRecoveryState", joinCSV(token, recoveryFlag(input), recoveryFlag(canInput), recoveryFlag(submitted))}
}

func recoveryFlag(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func (c *clientConn) waitingForRecoveryApplication() bool {
	return c.recoveryVersion == recoveryProtocolVersion && c.recoveryToken != "" && !c.recoveryApplied
}

func (c *clientConn) handleComebackApplied(token string) error {
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	if c.recoveryVersion != recoveryProtocolVersion || token == "" || token != c.recoveryToken {
		return c.writeRecoveryAckRejected(session)
	}
	var acknowledgement string
	var inputPhase bool
	var nextBattle bool
	var continuing bool
	if current := session.room; current != nil && current.State == RoomStateBattle && current.connections[c.memberType] == c {
		if c.recoveryApplied {
			return nil
		}
		if !recoveryMemberAlive(current, c.memberType) {
			return c.rejectKnockedOutRecovery(session, "RoomComebackFailed", true)
		}
		if current.pendingComebackTokens[c.memberType] != token || current.comebackTokens[c.memberType] == "" {
			return c.writeRecoveryAckRejected(session)
		}
		current.comebackTokens[c.memberType] = token
		delete(current.pendingComebackTokens, c.memberType)
		c.recoveryApplied = true
		c.setRecoveryDeadline(time.Time{})
		acknowledgement = comebackAcknowledgement(current)
		nextBattle = current.nextBattlePending
		continuing = current.continuation != nil
		inputPhase = !nextBattle && current.continuation == nil && current.userPhaseStarted && !current.userAttackStarted
	} else if completed := session.completed; completed != nil && completed.comebackConnections[c.memberType] == c {
		if c.recoveryApplied {
			return nil
		}
		if completed.pendingComebackTokens[c.memberType] != token || completed.comebackTokens[c.memberType] == "" {
			return c.writeRecoveryAckRejected(session)
		}
		completed.comebackTokens[c.memberType] = token
		delete(completed.pendingComebackTokens, c.memberType)
		c.recoveryApplied = true
		c.setRecoveryDeadline(time.Time{})
		// A completed snapshot has no gameplay to resume. Keep only the
		// original short-lived terminal interaction channel after application.
		delete(completed.comebackTokens, c.memberType)
		delete(completed.disconnectedUntil, c.memberType)
		c.setFinishingDeadline(time.Now().Add(battleInteractionLifetime))
	} else {
		return c.writeRecoveryAckRejected(session)
	}
	session.Unlock()
	if nextBattle {
		// GameNextFinish may have arrived first. Applying the snapshot must
		// release that already-recorded barrier, without fabricating its ACK.
		return c.server.tryAdvanceNextBattle(c.roomID)
	}
	if continuing {
		return c.server.advanceContinue(c.roomID)
	}
	if inputPhase {
		return c.server.tryAdvanceCardPlay(c.roomID)
	}
	return c.acknowledgeRecoveryPhase(acknowledgement)
}

func (c *clientConn) writeRecoveryAckRejected(session *lockedRoomSession) error {
	session.Unlock()
	return c.writeFrame("RoomComebackFailed", joinCSV("-1", "local battle recovery acknowledgement is unavailable"))
}

// A reconnecting client drains animations from its old socket before Ready.
// Only an applied snapshot may acknowledge the room's current phase for it.
func (c *clientConn) handlePresentationFinish(method string) error {
	if c.recoveryVersion == recoveryProtocolVersion {
		session := c.server.hub.lockRoomSession(c.roomID)
		attached := session.room != nil && session.room.connections[c.memberType] == c ||
			session.completed != nil && session.completed.comebackConnections[c.memberType] == c
		awaiting := attached && (c.comebackPending || c.waitingForRecoveryApplication())
		session.Unlock()
		if awaiting {
			return nil
		}
	}
	return c.acknowledgeRecoveryPhase(method)
}

func (c *clientConn) acknowledgeRecoveryPhase(acknowledgement string) error {
	switch acknowledgement {
	case "GameStartFinish":
		return c.handleGameStartFinish()
	case "TurnPhaseFinish":
		return c.handleTurnPhaseFinish()
	case "UserAttackFinish":
		return c.handleUserAttackFinish()
	case "ChaliceSphrExecUserPhaseFinish":
		return c.handleChaliceSphrExecUserPhaseFinish()
	case "EnemyPhaseFinish":
		return c.handleEnemyPhaseFinish()
	case "ChaliceSphrExecEnemyPhaseFinish":
		return c.handleChaliceSphrExecEnemyPhaseFinish()
	default:
		return nil
	}
}
