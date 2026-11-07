package multiplayer

// Detach unrevived humans at the completed turn boundary, before opening card
// input. Keep every character and its HP in the engine; offline slots use the
// existing CPU path. A dead participant loses both comeback and reward rights.
func detachDeadParticipantsLocked(current *room) ([]*clientConn, bool) {
	peers := []*clientConn{}
	changed := false
	for slot := 1; slot <= maxRoomMembers; slot++ {
		if current.engine.MemberAlive(slot) {
			continue
		}
		if peer := current.connections[slot]; peer != nil {
			peer.retired = true
			peers = append(peers, peer)
			delete(current.connections, slot)
			delete(current.cardPlayPlans, slot)
			changed = true
		}
		if current.comebackTokens[slot] != "" || !current.disconnectedUntil[slot].IsZero() {
			delete(current.comebackTokens, slot)
			delete(current.pendingComebackTokens, slot)
			delete(current.disconnectedUntil, slot)
			changed = true
		}
	}
	return peers, changed
}

// The stock failure-end notification returns the removed client through its
// existing defeat flow. MemberGameOver only logs on the CN client and cannot
// perform the exit. Complete neither the shared room nor a reward receipt here.
func (s *Server) retireDeadAtTurnBoundary(session *lockedRoomSession, current *room, peers []*clientConn) error {
	roomID := current.RoomID
	deliveries := reserveRoomFramesLocked(peers, battleFrame{"ApiGameEnd", "2,2"}, battleFrame{"GameClose", ""})
	if len(current.connections) == 0 {
		s.hub.removeRoom(current)
	}
	session.Unlock()
	broadcastRoomFrames(s, roomID, deliveries)
	for _, peer := range peers {
		_ = peer.close(false)
	}
	return s.tryAdvanceTurnPhase(roomID)
}
