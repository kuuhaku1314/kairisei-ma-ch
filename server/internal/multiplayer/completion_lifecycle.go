package multiplayer

import (
	"fmt"
	"time"
)

type completionPendingError struct{ cause error }

func (e *completionPendingError) Error() string {
	return fmt.Sprintf("commit completed battle: %v", e.cause)
}
func (e *completionPendingError) Unwrap() error { return e.cause }

// A failed terminal commit remains owned by the room, even after the final
// socket leaves. Retry for the result window; no account worker or new queue.
func (s *Server) scheduleCompletionRetry(roomID int64) {
	session := s.hub.lockRoomSession(roomID)
	defer session.Unlock()
	current := session.room
	if current == nil || current.closing || current.pendingCompletion == nil || current.completionRetry != nil {
		return
	}
	delay := time.Second
	for attempt := 1; attempt < min(current.completionFailures, 6); attempt++ {
		delay *= 2
	}
	current.completionRetry = time.AfterFunc(min(delay, 30*time.Second), func() {
		if !s.beginWork() {
			return
		}
		defer s.workers.Done()
		session := s.hub.lockRoomSession(roomID)
		defer session.Unlock()
		if session.room != current || current.closing {
			return
		}
		current.completionRetry = nil
		if current.completionDone != nil {
			return
		}
		if !current.pendingCompletion.expiresAt.After(time.Now()) {
			s.hub.removeRoom(current)
			s.logger.Error("multiplayer completion retry window expired", "room_id", roomID, "uncommitted_outcome", current.pendingCompletion.CompletedBattle)
			return
		}
		if err := s.completeGoBattleLocked(session, current); err != nil {
			s.logger.Error("multiplayer completion retry failed", "room_id", roomID, "error", err)
		}
	})
}

// Result DTOs outlive reconnect state. Timers release both even on an idle
// server; callbacks hold the room session before dropping engine references.
func (s *Server) scheduleCompletedExpiry(completed *completedBattle) {
	roomID, owner := completed.RoomID, completed.session
	time.AfterFunc(max(0, time.Until(completed.terminalExpiresAt)), func() {
		session := s.hub.lockRoomSession(roomID)
		defer session.Unlock()
		if session.owner != owner || session.completed == nil {
			return
		}
		current := session.completed
		current.terminalEngine = nil
		current.comebackTokens = nil
		current.pendingComebackTokens = nil
		current.disconnectedUntil = nil
		current.comebackConnections = nil
	})
	time.AfterFunc(max(0, time.Until(completed.expiresAt)), func() {
		s.hub.mu.Lock()
		defer s.hub.mu.Unlock()
		if current := s.hub.completed[roomID]; current != nil && current.session == owner {
			delete(s.hub.completed, roomID)
		}
	})
}
