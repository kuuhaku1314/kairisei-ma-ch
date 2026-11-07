package multiplayer

import (
	"strconv"
	"time"
)

func newComebackToken() (string, error) {
	credential, err := newCredential()
	if err != nil {
		return "", err
	}
	return credential.AuthToken, nil
}

func (c *clientConn) handleComeback(payload string) error {
	fields := splitCSV(payload)
	if len(fields) != 4 && (len(fields) != 5 || fields[4] != "2") {
		return c.writeComebackRejected()
	}
	userID, err := parsePositiveInt(fields[0])
	if err != nil {
		return c.writeComebackRejected()
	}
	roomID, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || roomID <= 0 || fields[2] == "" {
		return c.writeComebackRejected()
	}
	_, err = parseRangeInt(fields[3], 0, 2147483647)
	if err != nil {
		return c.writeComebackRejected()
	}
	if c.roomID != 0 || c.memberType != 0 || c.userID != 0 {
		return c.writeComebackRejected()
	}

	if len(fields) == 5 {
		c.recoveryVersion = recoveryProtocolVersion
	}
	hub := c.server.hub
	session := hub.lockRoomSession(roomID)
	for session.room != nil && session.room.completionDone != nil {
		done := session.room.completionDone
		session.Unlock()
		<-done
		session = hub.lockRoomSession(roomID)
	}
	defer session.Unlock()
	hub.mu.RLock()
	if hub.maintenance {
		hub.mu.RUnlock()
		session.Unlock()
		return c.writeComebackRejected()
	}
	now := time.Now()
	current, exists := session.room, session.room != nil
	memberType := roomMemberTypeForUser(current, userID)
	deadline, disconnected := time.Time{}, false
	if exists && current != nil {
		deadline, disconnected = current.disconnectedUntil[memberType]
	}
	var replaced *clientConn
	if exists && memberType >= 1 {
		replaced = current.connections[memberType]
	}
	valid := exists && current.State == RoomStateBattle && memberType >= 1 &&
		(replaced != nil && !replaced.retired || replaced == nil && disconnected && deadline.After(now)) &&
		validRecoveryToken(current.comebackTokens[memberType], current.pendingComebackTokens[memberType], fields[2])
	if valid {
		if !recoveryMemberAlive(current, memberType) {
			hub.mu.RUnlock()
			return c.rejectKnockedOutRecovery(session, "ComebackResult", false)
		}
		current.connections[memberType] = c
		delete(current.gameNextFinished, memberType)
		delete(current.disconnectedUntil, memberType)
		c.roomID = roomID
		c.memberType = memberType
		c.userID = userID
		c.comebackPending = true
		if c.recoveryVersion == recoveryProtocolVersion {
			c.setRecoveryDeadline(now.Add(comebackLifetime))
		}
		c.completedComeback = false
		hub.mu.RUnlock()
		delivery := c.reserveFramesLocked([]battleFrame{{"ComebackResult", joinCSV("0", "")}})
		session.Unlock()

		c.server.logger.Info(
			"local multiplayer comeback credential accepted",
			"room_id", roomID,
			"member_type", memberType,
			"user_id", userID,
		)
		if replaced != nil {
			_ = replaced.conn.Close()
			_ = replaced.close(true)
		}
		return delivery.send()
	}

	completed, completedExists := session.completed, session.completed != nil
	memberType = completedMemberTypeForUser(completed, userID)
	deadline = time.Time{}
	disconnected = false
	if completedExists && completed != nil {
		deadline, disconnected = completed.disconnectedUntil[memberType]
	}
	replaced = nil
	if completedExists && memberType >= 1 {
		replaced = completed.comebackConnections[memberType]
	}
	completedValid := completedExists && completed.expiresAt.After(now) && completed.terminalEngine != nil &&
		completed.terminalBattleEndType != 0 && memberType >= 1 && disconnected && deadline.After(now) &&
		(replaced == nil || !replaced.retired) &&
		validRecoveryToken(completed.comebackTokens[memberType], completed.pendingComebackTokens[memberType], fields[2])
	if !completedValid {
		hub.mu.RUnlock()
		session.Unlock()
		return c.writeComebackRejected()
	}
	completed.comebackConnections[memberType] = c
	c.roomID = roomID
	c.memberType = memberType
	c.userID = userID
	c.comebackPending = true
	if c.recoveryVersion == recoveryProtocolVersion {
		c.setRecoveryDeadline(now.Add(comebackLifetime))
	}
	c.completedComeback = true
	hub.mu.RUnlock()
	delivery := c.reserveFramesLocked([]battleFrame{{"ComebackResult", joinCSV("0", "")}})
	session.Unlock()

	c.server.logger.Info(
		"local multiplayer completed battle comeback credential accepted",
		"room_id", roomID,
		"member_type", memberType,
		"user_id", userID,
	)
	if replaced != nil {
		_ = replaced.conn.Close()
		_ = replaced.close(true)
	}
	return delivery.send()
}

func (c *clientConn) writeComebackRejected() error {
	// A protocol-level failure must be returned as ComebackResult. Closing the
	// socket without it makes the CN client retry the same stale token forever.
	return c.writeFrame("ComebackResult", joinCSV("-1", "local room comeback unavailable"))
}

func (c *clientConn) handleReadyToComeback(payload string) error {
	if payload != "" {
		return c.writeFrame("RoomComebackFailed", joinCSV("-1", "local room comeback payload is invalid"))
	}
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	if session.owner == nil || !c.comebackPending {
		session.Unlock()
		return c.writeFrame("RoomComebackFailed", joinCSV("-1", "local room comeback is not pending"))
	}
	if c.completedComeback || session.completed != nil && session.completed.comebackConnections[c.memberType] == c {
		c.completedComeback = true
		session.Unlock()
		return c.handleReadyToCompletedComeback()
	}

	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || current.connections[c.memberType] != c {
		session.Unlock()
		return c.writeFrame("RoomComebackFailed", joinCSV("-1", "local room comeback state is unavailable"))
	}
	if !recoveryMemberAlive(current, c.memberType) {
		return c.rejectKnockedOutRecovery(session, "RoomComebackFailed", true)
	}
	if current.engine == nil {
		// The original client is already in battle when it sends LoadingFinish.
		// ReadyToComeback replaces that lost acknowledgement, but must still wait
		// for other live members before Start produces a resumable engine state.
		c.loadingComebackReady = true
		current.battleLoading[c.memberType] = true
		roomID := current.RoomID
		session.Unlock()
		return c.server.tryStartLoadedBattle(roomID)
	}
	results, err := current.engine.ResumeResults(priorWaveResumeDrops(current.releasedDrops, current.battleIndex)...)
	if err != nil {
		session.Unlock()
		return c.writeFrame("RoomComebackFailed", joinCSV("-1", "local battle resume snapshot failed"))
	}
	results = current.chaliceResumeResults(results)
	resultPayload, err := encodeBattleResults(results)
	if err != nil {
		session.Unlock()
		return err
	}
	rotatedToken, err := c.prepareRecoveryToken(&current.pendingComebackTokens)
	if err != nil {
		session.Unlock()
		return err
	}
	acknowledgement := comebackAcknowledgement(current)
	battleIndex, progress := current.battleIndex, current.progress
	nextPending, nextIndex := current.nextBattlePending, current.nextBattleIndex
	nextProgress := roomBattleProgress(current, nextIndex)
	roomID := current.RoomID
	memberType := c.memberType

	// Resume the actual wave, including its independent visible progress.
	// resultPayload is an embedded multi-line ResultCmd stream. It must bypass
	// joinCSV's user-text sanitizer, which intentionally replaces commas and
	// newlines and would otherwise destroy the RoomComeback tail contract.
	response := joinCSV(strconv.Itoa(battleIndex), strconv.Itoa(progress), rotatedToken) + "," + resultPayload
	frames := []battleFrame{{"RoomComeback", response}}
	if c.recoveryVersion == recoveryProtocolVersion {
		frames = append(frames, recoveryStateFrame(current, memberType, rotatedToken))
	}
	if state := current.continuation; state != nil {
		if state.presentation != nil {
			if state.payerUserID == c.userID {
				frames = append(frames, battleFrame{"ContinueResult", joinCSV("1", strconv.Itoa(state.balance.Coin), strconv.Itoa(state.balance.CoinFree), "0")})
			}
			frames = append(frames, *state.presentation)
		} else {
			frames = append(frames, battleFrame{"ApiContinuePhaseStart", "10"})
		}
	}
	if current.userPhaseStarted && !current.userAttackStarted && !nextPending {
		for slot := 1; slot <= maxRoomMembers; slot++ {
			if current.engine.HasSubmission(slot) {
				// RoomComeback already restores committed cards; a stale plan
				// would clear their decided state in the original consumer.
				continue
			}
			plan, exists := current.cardPlayPlans[slot]
			if !exists || slot == memberType {
				continue
			}
			payload, err := cardPlayPlanResult(current.engine, slot, plan)
			if err != nil {
				session.Unlock()
				return err
			}
			frames = append(frames, battleFrame{"ApiCardPlayPlanR", payload})
		}
	}
	delivery := c.reserveFramesLocked(frames)
	var nextDelivery frameDelivery
	if nextPending {
		nextDelivery = c.reserveFramesLocked([]battleFrame{{"GameNextStart", joinCSV(strconv.Itoa(nextIndex), strconv.Itoa(nextProgress))}})
	}
	// Include the connection in live broadcasts only after reserving its
	// snapshot. Later state changes must reach it after RoomComeback.
	c.comebackPending = false
	c.loadingComebackReady = false
	session.Unlock()
	snapshotErr := delivery.send()
	if snapshotErr == nil && c.recoveryVersion != recoveryProtocolVersion {
		session = hub.lockRoomSession(roomID)
		defer session.Unlock()
		if latest := session.room; latest != nil && latest.connections[memberType] == c {
			latest.comebackTokens[memberType] = rotatedToken
		}
		session.Unlock()
	}
	if nextPending {
		// The client must rebuild the next scene before acknowledging. A resume
		// snapshot is NOT an implicit GameNextFinish. Always release the reserved
		// tail, including on a failed snapshot; the socket is already closed then.
		nextErr := nextDelivery.send()
		if snapshotErr != nil {
			return snapshotErr
		}
		return nextErr
	}
	if snapshotErr != nil {
		return snapshotErr
	}
	c.server.logger.Info(
		"local multiplayer room comeback snapshot sent",
		"room_id", roomID,
		"member_type", memberType,
		"result_rows", len(results),
		"phase_ack", acknowledgement,
	)

	// RoomComeback is an authoritative state acknowledgement. If the server is
	// already waiting at a direction barrier, count the recovered member as
	// having consumed that snapshot; otherwise increasing connections here
	// would leave the room permanently one Finish short.
	if c.recoveryVersion == recoveryProtocolVersion {
		return nil
	}
	return c.acknowledgeRecoveryPhase(acknowledgement)
}

func (c *clientConn) handleReadyToCompletedComeback() error {
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	completed, exists := session.completed, session.completed != nil
	if !exists || !completed.expiresAt.After(time.Now()) || completed.comebackConnections[c.memberType] != c || completed.terminalEngine == nil ||
		completed.terminalBattleEndType == 0 {
		session.Unlock()
		return c.writeFrame("RoomComebackFailed", joinCSV("-1", "local completed battle comeback is unavailable"))
	}
	results, err := completed.terminalEngine.ResumeResults(priorWaveResumeDrops(completed.ReleasedDrops, completed.BattleIndex)...)
	if err != nil {
		session.Unlock()
		return c.writeFrame("RoomComebackFailed", joinCSV("-1", "local completed battle snapshot failed"))
	}
	resultPayload, err := encodeBattleResults(results)
	if err != nil {
		session.Unlock()
		return err
	}
	rotatedToken, err := c.prepareRecoveryToken(&completed.pendingComebackTokens)
	if err != nil {
		session.Unlock()
		return err
	}
	roomID := c.roomID
	memberType := c.memberType
	endType := completed.terminalBattleEndType
	battleIndex, progress := completed.BattleIndex, completed.Progress
	response := joinCSV(strconv.Itoa(battleIndex), strconv.Itoa(progress), rotatedToken) + "," + resultPayload
	// BattleResultCmdTeamFunction.execComeback explicitly extracts a queued
	// RecvApiGameEnd after the RecvRoomComeback snapshot. Preserve that order,
	// then let the client choose its victory result or defeat return flow.
	frames := []battleFrame{{"RoomComeback", response}}
	if c.recoveryVersion == recoveryProtocolVersion {
		frames = append(frames, battleFrame{"RoomRecoveryState", joinCSV(rotatedToken, "0", "0", "1")})
	}
	delivery := c.reserveFramesLocked(frames)
	terminalDelivery := c.reserveFramesLocked([]battleFrame{
		{"ApiGameEnd", joinCSV("2", strconv.Itoa(endType))}, {"GameClose", ""}})
	session.Unlock()
	snapshotErr := delivery.send()
	if snapshotErr == nil && c.recoveryVersion != recoveryProtocolVersion {
		session = hub.lockRoomSession(roomID)
		defer session.Unlock()
		if latest := session.completed; latest != nil && latest.comebackConnections[memberType] == c {
			latest.comebackTokens[memberType] = rotatedToken
		}
		session.Unlock()
	}
	terminalErr := terminalDelivery.send()
	if snapshotErr != nil {
		return snapshotErr
	}
	if terminalErr != nil {
		return terminalErr
	}

	session = hub.lockRoomSession(roomID)
	defer session.Unlock()
	if latest := session.completed; latest != nil && latest.comebackConnections[memberType] == c {
		// The restored client can still be in the final direction. Retain only
		// its live interaction identity until it closes or reaches the deadline.
		c.setFinishingDeadline(time.Now().Add(battleInteractionLifetime))
		if c.recoveryVersion != recoveryProtocolVersion {
			delete(latest.comebackTokens, memberType)
			delete(latest.pendingComebackTokens, memberType)
			delete(latest.disconnectedUntil, memberType)
		}
		c.comebackPending = false
		c.completedComeback = false
	}
	session.Unlock()
	c.server.logger.Info(
		"local multiplayer completed battle comeback delivered",
		"room_id", roomID,
		"member_type", memberType,
		"result_rows", len(results),
		"battle_end", endType,
	)
	return nil
}

func comebackAcknowledgement(current *room) string {
	if current == nil || !current.gameStarted || current.nextBattlePending || current.continuation != nil {
		return ""
	}
	if !current.turnPhaseStarted {
		return "GameStartFinish"
	}
	if !current.userPhaseStarted {
		return "TurnPhaseFinish"
	}
	if !current.userAttackStarted {
		return ""
	}
	if !current.enemyPhaseStarted {
		if current.chaliceUserStarted {
			return "ChaliceSphrExecUserPhaseFinish"
		}
		return "UserAttackFinish"
	}
	if !current.chaliceEnemyStarted {
		return "EnemyPhaseFinish"
	}
	return "ChaliceSphrExecEnemyPhaseFinish"
}

func roomMemberTypeForUser(current *room, userID int) int {
	if current == nil || userID <= 0 {
		return 0
	}
	for _, member := range current.Members {
		if member.UserID == userID {
			return member.MemberType
		}
	}
	return 0
}

func completedMemberTypeForUser(completed *completedBattle, userID int) int {
	if completed == nil || userID <= 0 {
		return 0
	}
	for _, member := range completed.Members {
		if member.UserID == userID {
			return member.MemberType
		}
	}
	return 0
}

func (s *Server) scheduleComebackExpiry(roomID int64, memberType int, deadline time.Time) {
	delay := time.Until(deadline)
	if delay < 0 {
		delay = 0
	}
	time.AfterFunc(delay, func() {
		hub := s.hub
		session := hub.lockRoomSession(roomID)
		defer session.Unlock()
		current, exists := session.room, session.room != nil
		if !exists || current.connections[memberType] != nil || !current.disconnectedUntil[memberType].Equal(deadline) {
			session.Unlock()
			return
		}
		delete(current.disconnectedUntil, memberType)
		delete(current.comebackTokens, memberType)
		delete(current.pendingComebackTokens, memberType)
		released := len(current.connections) == 0 && len(current.disconnectedUntil) == 0 && current.pendingCompletion == nil
		if released {
			hub.removeRoom(current)
		}
		session.Unlock()
		if released {
			s.logger.Info("local multiplayer expired abandoned battle released", "room_id", roomID)
		} else {
			s.logger.Info("local multiplayer comeback window expired; member remains CPU", "room_id", roomID, "member_type", memberType)
		}
	})
}
