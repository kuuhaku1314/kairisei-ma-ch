package multiplayer

import (
	"errors"
	"fmt"
)

// roomBarrierReady checks the current live connection set rather than map
// lengths. A member may disconnect after acknowledging a direction, so stale
// keys must never keep a smaller live quorum blocked or make it appear larger.
func roomBarrierReady[T any](connections map[int]*clientConn, acknowledgements map[int]T) bool {
	if len(connections) == 0 {
		return false
	}
	for memberType, connection := range connections {
		if connection.waitingForRecoveryApplication() {
			return false
		}
		if _, acknowledged := acknowledgements[memberType]; !acknowledged {
			return false
		}
	}
	return true
}

func (s *Server) tryAdvanceGameStart(roomID int64) error {
	hub := s.hub
	session := hub.lockRoomSession(roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || !current.gameStarted || current.turnPhaseStarted ||
		!roomBarrierReady(current.connections, current.gameStartFinished) {
		session.Unlock()
		return nil
	}
	if current.engine == nil {
		session.Unlock()
		return errors.New("GameStartFinish Go battle engine is unavailable")
	}
	results, err := current.engine.TurnPhase()
	if err != nil {
		session.Unlock()
		return err
	}
	result, err := encodeBattleResults(results)
	if err != nil {
		session.Unlock()
		return err
	}
	current.turnPhaseStarted = true
	current.turnNumber = 1
	current.engineBattleEnd = current.engine.EndType()
	battleEnd := current.engineBattleEnd
	connectedMembers := len(current.connections)
	connections := append([]*clientConn(nil), roomConnections(current)...)
	deliveries := reserveRoomFramesLocked(connections, battleFrame{"ApiTurnPhase", result})
	session.Unlock()

	broadcastRoomFrames(s, roomID, deliveries)
	s.logger.Info(
		"local multiplayer Go turn phase broadcast",
		"room_id", roomID,
		"connected_members", connectedMembers,
		"result_rows", len(results),
		"battle_end", battleEnd,
	)
	return nil
}

func (s *Server) tryAdvanceTurnPhase(roomID int64) error {
	hub := s.hub
	session := hub.lockRoomSession(roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || !current.turnPhaseStarted || current.userPhaseStarted ||
		!roomBarrierReady(current.connections, current.turnPhaseFinished) {
		session.Unlock()
		return nil
	}
	if roomContinuePending(current) {
		return s.continueAtBarrierLocked(session, current)
	}
	if current.engine == nil {
		session.Unlock()
		return errors.New("TurnPhaseFinish Go battle engine is unavailable")
	}
	if current.engineBattleEnd != 0 {
		return s.completeGoBattleLocked(session, current)
	}
	if peers, changed := detachDeadParticipantsLocked(current); changed {
		return s.retireDeadAtTurnBoundary(session, current, peers)
	}
	results, err := current.engine.UserPhase()
	if err != nil {
		session.Unlock()
		return err
	}
	result, err := encodeBattleResults(results)
	if err != nil {
		session.Unlock()
		return err
	}
	current.userPhaseStarted = true
	submissions, err := automaticRoomCardSubmissionFrames(current)
	if err != nil {
		session.Unlock()
		return err
	}
	connectedMembers := len(current.connections)
	connections := append([]*clientConn(nil), roomConnections(current)...)
	frames := append([]battleFrame{{"ApiUserPhase", result}}, submissions...)
	deliveries := reserveRoomFramesLocked(connections, frames...)
	session.Unlock()

	broadcastRoomFrames(s, roomID, deliveries)
	s.logger.Info(
		"local multiplayer Go user phase broadcast",
		"room_id", roomID,
		"connected_members", connectedMembers,
		"result_rows", len(results),
	)
	// If every connected human is KO, no CardPlay request is required to
	// trigger the attack. Reserve/broadcast input and CPU confirmations first;
	// the usual barrier keeps living humans and animation ACKs authoritative.
	return s.tryAdvanceCardPlay(roomID)
}

func (s *Server) tryAdvanceCardPlay(roomID int64) error {
	if err := s.submitAutomaticRoomCards(roomID); err != nil {
		return err
	}
	hub := s.hub
	session := hub.lockRoomSession(roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || !current.userPhaseStarted || current.userAttackStarted ||
		!roomBarrierReady(current.connections, current.cardPlaySubmissions) {
		session.Unlock()
		return nil
	}
	if current.engine == nil {
		session.Unlock()
		return errors.New("CardPlay Go battle engine is unavailable")
	}
	cardPlayResults, err := commitRoomCardPlays(current)
	if err != nil {
		var inputErr *roomCardPlayError
		if errors.As(err, &inputErr) {
			owner := current.connections[inputErr.memberType]
			if owner != nil {
				session.Unlock()
				// A queued choice can become invalid before the last teammate
				// submits. Detach its owner, then let the existing disconnect
				// path fill that slot and retry the barrier outside the lock.
				s.logger.Warn("local multiplayer queued card play rejected",
					"room_id", roomID, "member_type", inputErr.memberType, "error", err)
				if closeErr := owner.close(true); closeErr != nil {
					s.logger.Warn("local multiplayer rejected input close failed", "room_id", roomID, "error", closeErr)
				}
				return nil
			}
		}
		session.Unlock()
		return err
	}
	cardPlayResult, err := encodeOptionalBattleResults(cardPlayResults)
	if err != nil {
		session.Unlock()
		return err
	}
	attackResults, err := current.engine.UserAttack()
	if err != nil {
		session.Unlock()
		return fmt.Errorf("execute Go user attack: %w", err)
	}
	attackResult, err := encodeBattleResults(attackResults)
	if err != nil {
		session.Unlock()
		return err
	}
	current.userAttackStarted = true
	current.openUserChaliceInput(attackResults)
	current.engineBattleEnd = current.engine.EndType()
	battleEnd := current.engineBattleEnd
	connectedMembers := len(current.connections)
	connections := append([]*clientConn(nil), roomConnections(current)...)
	// Earlier submissions already reserved their confirmation frames. Any
	// remaining automatic input must also precede the shared attack exactly once.
	var frames []battleFrame
	if cardPlayResult != "" {
		frames = append(frames, battleFrame{"ApiCardPlayR", cardPlayResult})
	}
	frames = append(frames, battleFrame{"ApiUserAttack", attackResult})
	deliveries := reserveRoomFramesLocked(connections, frames...)
	session.Unlock()

	broadcastRoomFrames(s, roomID, deliveries)
	s.logger.Info("local multiplayer Go user attack broadcast",
		"room_id", roomID, "connected_members", connectedMembers,
		"evaluated_members", maxRoomMembers,
		"card_play_result_rows", len(cardPlayResults),
		"attack_result_rows", len(attackResults),
		"battle_end", battleEnd)
	return nil
}

type roomCardPlayError struct {
	memberType int
	err        error
}

func (e *roomCardPlayError) Error() string {
	return fmt.Sprintf("evaluate cards for member %d: %v", e.memberType, e.err)
}

func (e *roomCardPlayError) Unwrap() error { return e.err }

func submitRoomCardPlay(preview *CardSelectionBatch, memberType int, submission cardPlaySubmission) ([]BattleResult, error) {
	return preview.SubmitClientSelection(memberType, submission)
}

func commitRoomCardPlays(current *room) ([]BattleResult, error) {
	// Accepted human and CPU submissions are already committed and broadcast.
	// Fill only remaining inputs before starting the shared UserAttack.
	cardPlayResults := make([]BattleResult, 0, 24)
	var submissions map[int]cardPlaySubmission
	err := current.engine.WithCardSelections(func(preview *CardSelectionBatch) error {
		for memberType := 1; memberType <= maxRoomMembers; memberType++ {
			if preview.HasSubmission(memberType) {
				continue
			}
			submission, submitted := current.cardPlaySubmissions[memberType]
			// Timeout-to-auto is resolved before storing a submission. Retain
			// re-selection for an uncommitted empty automatic input (e.g. KO),
			// while an accepted empty manual choice remains a pass.
			automaticInput := !submitted || (submission.Automatic && submission.ActionCount() == 0)
			if automaticInput {
				automatic, err := npcCardSubmission(preview, memberType, current.enemyType)
				if err != nil {
					return fmt.Errorf("select automatic cards for member %d: %w", memberType, err)
				}
				submission = automatic
			}
			cardResults, err := submitRoomCardPlay(preview, memberType, submission)
			if err != nil {
				if !automaticInput {
					return &roomCardPlayError{memberType: memberType, err: err}
				}
				return fmt.Errorf("evaluate cards for member %d: %w", memberType, err)
			}
			cardPlayResults = append(cardPlayResults, cardResults...)
		}
		submissions = preview.Submissions()
		return nil
	})
	if err != nil {
		return nil, err
	}
	current.cardPlaySubmissions = submissions
	return cardPlayResults, nil
}

func (s *Server) tryAdvanceUserAttack(roomID int64) error {
	hub := s.hub
	session := hub.lockRoomSession(roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || !current.userAttackStarted || current.chaliceUserStarted ||
		!roomBarrierReady(current.connections, current.userAttackFinished) {
		session.Unlock()
		return nil
	}
	if roomContinuePending(current) {
		return s.continueAtBarrierLocked(session, current)
	}
	if current.engineBattleEnd != 0 {
		return s.completeGoBattleLocked(session, current)
	}
	if current.engine == nil {
		session.Unlock()
		return errors.New("UserAttackFinish Go battle engine is unavailable")
	}
	if err := current.commitChaliceInput(); err != nil {
		session.Unlock()
		return err
	}
	results, err := current.engine.ExecuteChaliceUserPhase()
	if err != nil {
		session.Unlock()
		return fmt.Errorf("execute Go chalice user phase: %w", err)
	}
	result, err := encodeOptionalBattleResults(results)
	if err != nil {
		session.Unlock()
		return err
	}
	current.chaliceUserStarted = true
	current.engineBattleEnd = current.engine.EndType()
	battleEnd := current.engineBattleEnd
	connections := append([]*clientConn(nil), roomConnections(current)...)
	deliveries := reserveRoomFramesLocked(connections, battleFrame{"ApiChaliceSphrExecUserPhase", result})
	session.Unlock()

	broadcastRoomFrames(s, roomID, deliveries)
	s.logger.Info(
		"local multiplayer Go chalice user phase broadcast",
		"room_id", roomID,
		"result_rows", len(results),
		"battle_end", battleEnd,
	)
	return nil
}

func (s *Server) tryAdvanceChaliceUser(roomID int64) error {
	hub := s.hub
	session := hub.lockRoomSession(roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || !current.chaliceUserStarted || current.enemyPhaseStarted ||
		!roomBarrierReady(current.connections, current.chaliceUserFinished) {
		session.Unlock()
		return nil
	}
	if roomContinuePending(current) {
		return s.continueAtBarrierLocked(session, current)
	}
	if current.engineBattleEnd != 0 {
		return s.completeGoBattleLocked(session, current)
	}
	if current.engine == nil {
		session.Unlock()
		return errors.New("ChaliceSphrExecUserPhaseFinish Go battle engine is unavailable")
	}
	current.openEnemyChaliceInput()
	results, err := current.engine.EnemyPhase()
	if err != nil {
		session.Unlock()
		return fmt.Errorf("execute Go enemy phase: %w", err)
	}
	result, err := encodeBattleResults(results)
	if err != nil {
		session.Unlock()
		return err
	}
	current.enemyPhaseStarted = true
	current.engineBattleEnd = current.engine.EndType()
	battleEnd := current.engineBattleEnd
	connections := append([]*clientConn(nil), roomConnections(current)...)
	deliveries := reserveRoomFramesLocked(connections, battleFrame{"ApiEnemyPhase", result})
	session.Unlock()

	broadcastRoomFrames(s, roomID, deliveries)
	s.logger.Info(
		"local multiplayer Go enemy phase broadcast",
		"room_id", roomID,
		"result_rows", len(results),
		"battle_end", battleEnd,
	)
	return nil
}

func (s *Server) tryAdvanceEnemyPhase(roomID int64) error {
	hub := s.hub
	session := hub.lockRoomSession(roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || !current.enemyPhaseStarted || current.chaliceEnemyStarted ||
		!roomBarrierReady(current.connections, current.enemyPhaseFinished) {
		session.Unlock()
		return nil
	}
	if roomContinuePending(current) {
		return s.continueAtBarrierLocked(session, current)
	}
	if current.engineBattleEnd != 0 {
		return s.completeGoBattleLocked(session, current)
	}
	if current.engine == nil {
		session.Unlock()
		return errors.New("EnemyPhaseFinish Go battle engine is unavailable")
	}
	if err := current.commitChaliceInput(); err != nil {
		session.Unlock()
		return err
	}
	results, err := current.engine.ExecuteChaliceEnemyPhase()
	if err != nil {
		session.Unlock()
		return fmt.Errorf("execute Go chalice enemy phase: %w", err)
	}
	result, err := encodeOptionalBattleResults(results)
	if err != nil {
		session.Unlock()
		return err
	}
	current.chaliceEnemyStarted = true
	current.engineBattleEnd = current.engine.EndType()
	connections := append([]*clientConn(nil), roomConnections(current)...)
	deliveries := reserveRoomFramesLocked(connections, battleFrame{"ApiChaliceSphrExecEnemyPhase", result})
	session.Unlock()

	broadcastRoomFrames(s, roomID, deliveries)
	s.logger.Info(
		"local multiplayer Go chalice enemy phase broadcast",
		"room_id", roomID,
		"result_rows", len(results),
	)
	return nil
}

func (s *Server) tryAdvanceChaliceEnemy(roomID int64) error {
	hub := s.hub
	session := hub.lockRoomSession(roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || !current.chaliceEnemyStarted ||
		!roomBarrierReady(current.connections, current.chaliceEnemyFinished) {
		session.Unlock()
		return nil
	}
	if roomContinuePending(current) {
		return s.continueAtBarrierLocked(session, current)
	}
	if current.engineBattleEnd != 0 {
		return s.completeGoBattleLocked(session, current)
	}
	if current.engine == nil {
		session.Unlock()
		return errors.New("ChaliceSphrExecEnemyPhaseFinish Go battle engine is unavailable")
	}
	results, err := current.engine.TurnPhase()
	if err != nil {
		session.Unlock()
		return err
	}
	result, err := encodeBattleResults(results)
	if err != nil {
		session.Unlock()
		return err
	}
	current.engineBattleEnd = current.engine.EndType()
	battleEnd := current.engineBattleEnd
	current.turnNumber++
	current.turnPhaseFinished = make(map[int]bool, len(current.connections))
	current.userPhaseStarted = false
	current.cardPlayPlans = make(map[int]cardPlaySubmission, len(current.connections))
	current.cardPlaySubmissions = make(map[int]cardPlaySubmission, len(current.connections))
	current.userAttackStarted = false
	current.userAttackFinished = make(map[int]bool, len(current.connections))
	current.chaliceUserStarted = false
	current.chaliceUserFinished = make(map[int]bool, len(current.connections))
	current.enemyPhaseStarted = false
	current.enemyPhaseFinished = make(map[int]bool, len(current.connections))
	current.chaliceEnemyStarted = false
	current.chaliceEnemyFinished = make(map[int]bool, len(current.connections))
	turnNumber := current.turnNumber
	connections := append([]*clientConn(nil), roomConnections(current)...)
	deliveries := reserveRoomFramesLocked(connections, battleFrame{"ApiTurnPhase", result})
	session.Unlock()

	broadcastRoomFrames(s, roomID, deliveries)
	s.logger.Info(
		"local multiplayer next Go turn phase broadcast",
		"room_id", roomID,
		"turn", turnNumber,
		"result_rows", len(results),
		"battle_end", battleEnd,
	)
	return nil
}

// advanceBattleAfterDisconnect re-evaluates exactly the phase whose live
// quorum may have become complete. It never emits the client's Disconnect
// error callback; the disconnected character remains an engine-controlled CPU
// and may still reclaim its slot through the bounded comeback token.
func (s *Server) advanceBattleAfterDisconnect(roomID int64) error {
	hub := s.hub
	session := hub.lockRoomSession(roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || !current.gameStarted || len(current.connections) == 0 {
		session.Unlock()
		return nil
	}
	var advance func(int64) error
	switch {
	case current.continuation != nil:
		advance = s.advanceContinue
	case current.nextBattlePending:
		advance = s.tryAdvanceNextBattle
	case !current.turnPhaseStarted:
		advance = s.tryAdvanceGameStart
	case !current.userPhaseStarted:
		advance = s.tryAdvanceTurnPhase
	case !current.userAttackStarted:
		advance = s.tryAdvanceCardPlay
	case !current.chaliceUserStarted:
		advance = s.tryAdvanceUserAttack
	case !current.enemyPhaseStarted:
		advance = s.tryAdvanceChaliceUser
	case !current.chaliceEnemyStarted:
		advance = s.tryAdvanceEnemyPhase
	default:
		advance = s.tryAdvanceChaliceEnemy
	}
	session.Unlock()
	return advance(roomID)
}
