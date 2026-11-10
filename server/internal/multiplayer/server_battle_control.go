package multiplayer

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"
)

// tryStartLoadedBattle is the single transition from battle loading into the
// authoritative Go engine. It is intentionally idempotent: both a normal
// LoadingFinish and a disconnect that shrinks the live loading quorum may
// satisfy the same condition.
func (s *Server) tryStartLoadedBattle(roomID int64) error {
	// The auto-start transport skips the lobby countdown. It uses the same
	// durable host debit; ordinary rooms already committed at countdown end.
	if err := s.authorizeBattleStart(roomID, RoomStateBattle, false); err != nil {
		return err
	}
	hub := s.hub
	session := hub.lockRoomSession(roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || current.gameStarted || current.startCommitting ||
		(hub.startAuthorizer != nil && !current.hostCostPaid) || len(current.connections) == 0 ||
		!roomBarrierReady(current.connections, current.battleLoading) {
		session.Unlock()
		return nil
	}
	spec := RoomSpec{
		ScorePolicy:  current.scorePolicy,
		EnemyPartyID: current.enemyPartyID, EnemyType: current.enemyType, CostInitial: current.costInitial,
		ContinueAllowed: current.continueAllowed,
		AwakeTakeover:   current.awakeTakeover,
		HoldMax:         current.holdMax, BurstGaugeInitial: current.burstGaugeInitial, Seed: current.seed,
		DropPlan: current.dropPlan,
		Battles:  current.battles,
	}
	engine, err := newBattleEngine(hub.combat, spec, current.Members)
	if err != nil {
		session.Unlock()
		return fmt.Errorf("initialize Go battle engine: %w", err)
	}
	results, err := engine.Start()
	if err != nil {
		session.Unlock()
		return fmt.Errorf("start Go battle engine: %w", err)
	}
	result, err := roomStartResult(current, results)
	if err != nil {
		session.Unlock()
		return err
	}
	current.engine = engine
	current.gameStarted = true
	connectedMembers := len(current.connections)
	seed := current.seed
	connections := append([]*clientConn(nil), roomConnections(current)...)
	var loadingComebacks []*clientConn
	for _, connection := range current.connections {
		if connection.comebackPending && connection.loadingComebackReady {
			loadingComebacks = append(loadingComebacks, connection)
		}
	}
	payload := strconv.FormatInt(time.Now().Unix(), 10) + "," + result
	deliveries := reserveRoomFramesLocked(connections, battleFrame{"ApiGameStart", payload})
	session.Unlock()

	// Pending recoveries consume a Start-state snapshot instead of ApiGameStart.
	// Send alongside normal peers so one slow socket cannot hold up the others.
	var recovering sync.WaitGroup
	for _, connection := range loadingComebacks {
		recovering.Add(1)
		go func() {
			defer recovering.Done()
			if err := connection.handleReadyToComeback(""); err != nil {
				s.logger.Warn("resume loaded battle failed", "room_id", roomID, "error", err)
				_ = connection.close(true)
			}
		}()
	}
	broadcastRoomFrames(s, roomID, deliveries)
	recovering.Wait()
	s.logger.Info(
		"local multiplayer Go battle start broadcast",
		"room_id", roomID,
		"connected_members", connectedMembers,
		"seed", seed,
		"result_rows", len(results),
	)
	return nil
}

func (s *Server) authorizeBattleStart(roomID int64, state RoomState, checkOnly bool) error {
	hub := s.hub
	session := hub.lockRoomSession(roomID)
	defer session.Unlock()
	current, ok := session.room, session.room != nil
	if ok && current.State == RoomStateCountdown && state == RoomStateCountdown && !current.hostCostPaid &&
		!current.startCommitting && len(current.connections) < max(defaultMemberCount, current.GameStartMemberNum) {
		deliveries := reserveRoomFramesLocked(roomConnections(current), reopenCountdownFramesLocked(current)...)
		session.Unlock()
		broadcastRoomFrames(s, roomID, deliveries)
		return errors.New("multiplayer start requires at least two real players")
	}
	if !ok || current.closing || current.State != state || current.hostCostPaid || hub.startAuthorizer == nil {
		session.Unlock()
		return nil
	}
	if current.startCommitting || (state == RoomStateCountdown && current.countdownSyncs > 0) {
		session.Unlock()
		return nil
	}
	if !checkOnly && state == RoomStateCountdown && (current.countdownChecking || time.Now().Before(current.countdownDeadline)) {
		session.Unlock()
		return nil
	}
	if state == RoomStateBattle && !roomBarrierReady(current.connections, current.battleLoading) {
		session.Unlock()
		return nil
	}
	start := BattleStart{RoomID: roomID, BossID: current.BossID, BPUse: current.battlePointUse, CheckOnly: checkOnly}
	for _, member := range current.Members {
		if member.MemberType == current.OwnerMemberType {
			start.OwnerUserID = member.UserID
		} else if current.connections[member.MemberType] != nil {
			start.GuestUserIDs = append(start.GuestUserIDs, member.UserID)
		}
	}
	current.startCommitting = true
	current.startDone = make(chan struct{})
	authorize := hub.startAuthorizer
	session.Unlock()
	err := authorize(start)
	session = session.relock()
	defer session.Unlock()
	if session.room != current {
		finishStartCommitLocked(current)
		return errors.New("battle room changed during account transaction")
	}
	if err == nil {
		if checkOnly {
			finishStartCommitLocked(current)
			return nil
		}
		current.hostCostPaid = true
		if state != RoomStateCountdown {
			finishStartCommitLocked(current)
		}
		// Countdown success stays reserved until finishCountdown commits the
		// battle state and outgoing frames, before a disconnected owner detaches.
		session.Unlock()
		return nil
	}
	finishStartCommitLocked(current)
	connections := append([]*clientConn(nil), roomConnections(current)...)
	var deliveries []frameDelivery
	if state == RoomStateCountdown {
		message := "暂时无法开始战斗，请稍后重试。"
		var denied *BattleStartDenied
		if errors.As(err, &denied) {
			message = denied.Message
			for _, member := range current.Members {
				if member.UserID == denied.UserID && member.Name != "" {
					message = member.Name + "：" + message
					break
				}
			}
		}
		frames := reopenCountdownFramesLocked(current)
		// TeamRoom consumes this as a native text dialog. Do not turn a
		// balance refusal into a socket exception or close the transport.
		frames = append(frames, battleFrame{"RoomCountdownFailed", joinCSV("-1", message)})
		deliveries = reserveRoomFramesLocked(connections, frames...)
	} else {
		current.State = RoomStateClosed
		hub.removeRoom(current)
	}
	session.Unlock()
	if state == RoomStateCountdown {
		broadcastRoomFrames(s, roomID, deliveries)
	} else {
		for _, connection := range connections {
			_ = connection.close(true)
		}
	}
	return err
}

func finishStartCommitLocked(current *room) {
	current.startCommitting = false
	if current.startDone != nil {
		close(current.startDone)
		current.startDone = nil
	}
}

// handleAwakeSkip owns only the synchronized presentation control consumed by
// BattlesvMgr.OnAwakeSkipExec. Enemy awake state and battle completion remain
// authoritative BattleEngine state; skipping a playlist must not mutate them.
func (c *clientConn) handleAwakeSkip(payload string) error {
	if payload != "" {
		return errors.New("AwakeSkip payload is not empty")
	}

	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	valid := exists && current.State == RoomStateBattle && current.OwnerMemberType == c.memberType &&
		current.connections[c.memberType] == c && !c.comebackPending
	if !valid {
		session.Unlock()
		return errors.New("AwakeSkip is not allowed")
	}
	connections := append([]*clientConn(nil), roomConnections(current)...)
	roomID := current.RoomID
	deliveries := reserveRoomFramesLocked(connections, battleFrame{"AwakeSkipExec", ""})
	session.Unlock()

	broadcastRoomFrames(c.server, roomID, deliveries)
	return nil
}
