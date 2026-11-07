package multiplayer

// NPC control is not participation eligibility: a temporary disconnect keeps
// its comeback token, whereas Retire/death-boundary removal clears it. A KO
// before that boundary must not revoke the token; a teammate may still revive.
func recoveryMemberAlive(current *room, memberType int) bool {
	if current == nil || memberType < 1 || memberType > maxRoomMembers {
		return false
	}
	if current.engine != nil {
		return current.engine.MemberAlive(memberType)
	}
	// During initial loading no engine exists yet. Use the admitted member's
	// starting HP, without starting/simulating the battle merely for a query.
	for _, member := range current.Members {
		if member.MemberType == memberType {
			return member.HP > 0
		}
	}
	return false
}

func (c *clientConn) rejectKnockedOutRecovery(session *lockedRoomSession, method string, attached bool) error {
	session.Unlock()
	err := c.writeFrame(method, joinCSV("-2", "角色当前已倒地，暂时无法续战。若在正式退场前被复活，可重新尝试。"))
	if attached {
		// Keep the ordinary disconnect reservation. This is not a voluntary
		// retirement and must not bypass the existing next-turn death decision.
		if closeErr := c.close(true); err == nil {
			err = closeErr
		}
	}
	return err
}

// AbandonRecoveryFor records an authenticated account's explicit departure.
// It is idempotent and affects no other account or engine character. HTTP
// callers already own an account lock, so do not synchronously run close(true)
// here: that can advance a room into an account transaction. Closing the socket
// wakes its existing reader, which owns normal phase/connection cleanup.
func (h *Hub) AbandonRecoveryFor(userID int, roomID int64) {
	if userID <= 0 || roomID <= 0 {
		return
	}
	session := h.lockRoomSession(roomID)
	var peer *clientConn
	if current := session.room; current != nil && current.State == RoomStateBattle {
		member := roomMemberTypeForUser(current, userID)
		if member > 0 {
			delete(current.comebackTokens, member)
			delete(current.pendingComebackTokens, member)
			delete(current.disconnectedUntil, member)
			peer = current.connections[member]
			if peer != nil {
				peer.retired = true
			}
		}
	} else if completed := session.completed; completed != nil {
		member := completedMemberTypeForUser(completed, userID)
		if member > 0 {
			delete(completed.comebackTokens, member)
			delete(completed.pendingComebackTokens, member)
			delete(completed.disconnectedUntil, member)
			peer = completed.comebackConnections[member]
			if peer != nil {
				peer.retired = true
			}
		}
	}
	session.Unlock()
	if peer != nil {
		_ = peer.conn.Close()
	}
}
