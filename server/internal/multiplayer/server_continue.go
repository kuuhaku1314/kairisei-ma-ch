package multiplayer

import (
	"errors"
	"strconv"
	"time"
)

// BattleContinue comes only from the paused authoritative room, never from
// a client-supplied price, user ID or transaction ID.
type BattleContinue struct {
	RoomID   int64
	BossID   int
	UserID   int
	Sequence int
}

type ContinueBalance struct{ Coin, CoinFree int }

func (h *Hub) AttachContinueAuthorizer(authorize func(BattleContinue) (ContinueBalance, error)) error {
	if authorize == nil {
		return errors.New("multiplayer continue authorizer is required")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.configFrozen || h.continueAuthorizer != nil || len(h.rooms) != 0 || len(h.pending) != 0 {
		return errors.New("attach multiplayer continue authorizer before room activity")
	}
	h.continueAuthorizer = authorize
	return nil
}

type roomContinuation struct {
	sequence     int
	deadline     time.Time
	timer        *time.Timer
	declined     map[int]bool
	finished     map[int]bool
	presentation *battleFrame
	payerUserID  int
	balance      ContinueBalance
}

// The stock dialog has its own timer. Bound the room too, so a disconnected
// phone cannot leave the surviving team waiting for a missing cancel packet.
const continueDecisionTimeout = 20 * time.Second // Original dialog: 10 seconds, plus transport grace.

func roomContinuePending(current *room) bool {
	return current.continuation != nil || current.engine.ContinuePending()
}

// continueAtBarrierLocked consumes the room lease like completeGoBattleLocked.
// Existing phase acknowledgements are retained: resuming advances exactly the
// paused phase, without replaying attacks, drawing again or incrementing turns.
func (s *Server) continueAtBarrierLocked(session *lockedRoomSession, current *room) error {
	if current.continuation != nil {
		session.Unlock()
		return nil
	}
	current.continueSequence++
	state := &roomContinuation{sequence: current.continueSequence, deadline: time.Now().Add(continueDecisionTimeout),
		declined: make(map[int]bool), finished: make(map[int]bool)}
	current.continuation = state
	roomID := current.RoomID
	state.timer = time.AfterFunc(continueDecisionTimeout, func() {
		if err := s.advanceContinue(roomID); err != nil {
			s.logger.Error("continue timeout failed", "room_id", roomID, "error", err)
		}
	})
	deliveries := reserveRoomFramesLocked(roomConnections(current), battleFrame{"ApiContinuePhaseStart", "10"})
	session.Unlock()
	broadcastRoomFrames(s, roomID, deliveries)
	return nil
}

func (c *clientConn) handleContinue(payload string) error {
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, ok := session.room, session.room != nil
	if payload != "" || !ok || current.closing || current.State != RoomStateBattle || current.connections[c.memberType] != c || c.retired {
		session.Unlock()
		return c.writeFrame("ContinueFailed", joinCSV("-1", "当前无法续关"))
	}
	state := current.continuation
	if state == nil || state.presentation != nil || current.continueDone != nil {
		session.Unlock()
		return nil // A delayed/duplicate request must not charge again.
	}
	if !current.continueAllowed || !current.engine.ContinuePending() || hub.continueAuthorizer == nil || time.Now().After(state.deadline) {
		session.Unlock()
		if err := c.writeFrame("ContinueFailed", joinCSV("-1", "本次无法续关")); err != nil {
			return err
		}
		return c.server.advanceContinue(c.roomID)
	}
	plan, err := current.engine.PrepareContinue(c.memberType)
	if err != nil {
		session.Unlock()
		return c.writeFrame("ContinueFailed", joinCSV("-1", "当前角色无法续关"))
	}
	// Validate the post-revival snapshot before touching currency. It must be
	// sent through RoomComeback AFTER the animation: RESUME_BUFF adds entries
	// and requires notifyRoomComeback's prior state reset, unlike delta rows.
	_, err = plan.ResumeResults(priorWaveResumeDrops(current.releasedDrops, current.battleIndex)...)
	if err != nil {
		session.Unlock()
		return err
	}
	result, err := encodeBattleResults(plan.Results())
	if err != nil {
		session.Unlock()
		return err
	}
	request := BattleContinue{RoomID: current.RoomID, BossID: current.BossID, UserID: c.userID, Sequence: state.sequence}
	current.continueDone = make(chan struct{})
	authorize := hub.continueAuthorizer
	session.Unlock()
	// Account callbacks run outside the session. Detachment and phase advance
	// respect continueDone until this transaction commits or fails.
	balance, chargeErr := authorize(request)
	session = session.relock()
	defer session.Unlock()
	if session.room != current {
		close(current.continueDone)
		current.continueDone = nil
		return errors.New("continue room changed during account transaction")
	}
	if chargeErr != nil {
		close(current.continueDone)
		current.continueDone = nil
		delete(state.declined, c.memberType)
		delivery := c.reserveFramesLocked([]battleFrame{
			{"ContinueFailed", joinCSV("-1", "续关未成功，请确认水晶余额后重试")},
			{"ApiContinuePhaseStart", "10"}, // OnYesClick closed the original dialog; reopen for retry.
		})
		session.Unlock()
		c.server.logger.Warn("continue debit rejected", "room_id", request.RoomID, "user_id", c.userID, "error", chargeErr)
		if err := delivery.send(); err != nil {
			return err
		}
		return c.server.advanceContinue(request.RoomID)
	}
	// Socket detachment waits for continueDone. The room/engine remain frozen
	// while the account transaction runs, so a successful debit always revives.
	if err := plan.Commit(); err != nil {
		close(current.continueDone)
		current.continueDone = nil
		return err
	}
	current.engineBattleEnd = 0
	frame := battleFrame{"ApiContinue", result}
	state.presentation = &frame
	state.payerUserID, state.balance = c.userID, balance
	state.timer.Stop()
	var deliveries []frameDelivery
	for _, peer := range roomConnections(current) {
		frames := []battleFrame{}
		if peer == c {
			frames = append(frames, battleFrame{"ContinueResult", joinCSV("1", strconv.Itoa(balance.Coin), strconv.Itoa(balance.CoinFree), "0")})
		}
		frames = append(frames, frame)
		deliveries = append(deliveries, peer.reserveFramesLocked(frames))
	}
	close(current.continueDone)
	current.continueDone = nil
	session.Unlock()
	broadcastRoomFrames(c.server, request.RoomID, deliveries)
	return nil
}

func (c *clientConn) handleContinuePhaseFinish(payload string) error {
	if payload != "" {
		return errors.New("ContinuePhaseFinish payload must be empty")
	}
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, ok := session.room, session.room != nil
	if ok && current.connections[c.memberType] == c && current.continuation != nil && current.continuation.presentation == nil {
		current.continuation.declined[c.memberType] = true
	}
	session.Unlock()
	return c.server.advanceContinue(c.roomID)
}

func (c *clientConn) handleGameOverPhaseFinish(payload string) error {
	if payload != "" {
		return errors.New("GameOverPhaseFinish payload must be empty")
	}
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, ok := session.room, session.room != nil
	if ok && current.connections[c.memberType] == c && current.continuation != nil && current.continuation.presentation != nil {
		current.continuation.finished[c.memberType] = true
		// Disconnect removes the old phase ACK. The recovered member's
		// snapshot plus animation ACK also acknowledges that paused direction.
		acknowledgeContinuedDirection(current, c.memberType)
	}
	session.Unlock()
	return c.server.advanceContinue(c.roomID)
}

func acknowledgeContinuedDirection(current *room, memberType int) {
	var acknowledged *map[int]bool
	switch {
	case !current.turnPhaseStarted:
		acknowledged = &current.gameStartFinished
	case !current.userPhaseStarted:
		acknowledged = &current.turnPhaseFinished
	case !current.userAttackStarted:
		return
	case !current.chaliceUserStarted:
		acknowledged = &current.userAttackFinished
	case !current.enemyPhaseStarted:
		acknowledged = &current.chaliceUserFinished
	case !current.chaliceEnemyStarted:
		acknowledged = &current.enemyPhaseFinished
	default:
		acknowledged = &current.chaliceEnemyFinished
	}
	if *acknowledged == nil {
		*acknowledged = make(map[int]bool)
	}
	(*acknowledged)[memberType] = true
}

func (s *Server) advanceContinue(roomID int64) error {
	hub := s.hub
	session := hub.lockRoomSession(roomID)
	defer session.Unlock()
	current, ok := session.room, session.room != nil
	if !ok || current.State != RoomStateBattle || current.continuation == nil || current.continueDone != nil || len(current.connections) == 0 {
		session.Unlock()
		return nil
	}
	state := current.continuation
	if state.presentation != nil {
		if !roomBarrierReady(current.connections, state.finished) {
			session.Unlock()
			return nil
		}
		if current.engineBattleEnd != 0 {
			current.continuation = nil
			return s.completeGoBattleLocked(session, current)
		}
		rows, err := current.engine.ResumeResults(priorWaveResumeDrops(current.releasedDrops, current.battleIndex)...)
		if err != nil {
			session.Unlock()
			return err
		}
		// Just like reconnect, the reset snapshot must restore reservations
		// already broadcast during the paused direction. The engine consumes
		// them only after this continuation barrier, not during payment.
		rows = current.chaliceResumeResults(rows)
		payload, err := encodeBattleResults(rows)
		if err != nil {
			session.Unlock()
			return err
		}
		var deliveries []frameDelivery
		for _, peer := range roomConnections(current) {
			// The original callback resets user/enemy data before RESUME rows;
			// retain the current reconnect credential and actual wave identity.
			response := joinCSV(strconv.Itoa(current.battleIndex), strconv.Itoa(current.progress), current.comebackTokens[peer.memberType]) + "," + payload
			deliveries = append(deliveries, peer.reserveFramesLocked([]battleFrame{{"RoomComeback", response}}))
		}
		current.continuation = nil
		session.Unlock()
		broadcastRoomFrames(s, roomID, deliveries)
		return s.advanceBattleAfterDisconnect(roomID)
	}
	if time.Now().Before(state.deadline) && !roomBarrierReady(current.connections, state.declined) {
		session.Unlock()
		return nil
	}
	// Wait until nobody intends to pay before retiring KO members. CPU slots
	// never purchase a continuation and do not add votes to the live quorum.
	rows := current.engine.CancelContinue()
	result, err := encodeBattleResults(rows)
	if err != nil {
		session.Unlock()
		return err
	}
	current.engineBattleEnd = current.engine.EndType()
	frame := battleFrame{"ApiGameOver", result}
	state.presentation = &frame
	state.timer.Stop()
	deliveries := reserveRoomFramesLocked(roomConnections(current), frame)
	session.Unlock()
	broadcastRoomFrames(s, roomID, deliveries)
	return nil
}
