package multiplayer

import (
	"context"
	"errors"
)

var ErrMaintenance = errors.New("服务器维护中，请稍后再试")

// Admission closes immediately, independently of whether existing rooms end
// naturally or the operator explicitly interrupts them.
func (h *Hub) SetMaintenance(enabled bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.maintenance = enabled
	if enabled {
		clear(h.pending)
	}
	return nil
}

func (h *Hub) MaintenanceActivity() (rooms, creating int) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.rooms), h.creatingRooms
}

// InterruptRooms discards unsettled runs, but lets already-reserved account
// transactions commit or fail naturally. Never wait with a room/registry lock.
// The caller keeps maintenance enabled throughout this operation.
func (h *Hub) InterruptRooms(ctx context.Context) (int, error) {
	h.mu.RLock()
	enabled, creatingDone := h.maintenance, h.creatingDone
	h.mu.RUnlock()
	if !enabled {
		return 0, errors.New("请先开启服务器维护模式")
	}
	if creatingDone != nil {
		select {
		case <-creatingDone:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	h.mu.RLock()
	ids := make([]int64, 0, len(h.rooms)+len(h.completed))
	for id := range h.rooms {
		ids = append(ids, id)
	}
	for id := range h.completed {
		ids = append(ids, id)
	}
	h.mu.RUnlock()
	closed := 0
	for _, id := range ids {
		for {
			session := h.lockRoomSession(id)
			var peers []*clientConn
			if current := session.room; current != nil {
				current.closing = true
				done := current.completionDone
				if done == nil {
					done = current.startDone
				}
				if done == nil {
					done = current.continueDone
				}
				if done != nil {
					session.Unlock()
					select {
					case <-done:
						continue
					case <-ctx.Done():
						return closed, ctx.Err()
					}
				}
				if current.completionRetry != nil {
					current.completionRetry.Stop()
				}
				if current.continuation != nil && current.continuation.timer != nil {
					current.continuation.timer.Stop()
				}
				peers = roomConnections(current)
				h.removeRoom(current)
				closed++
			} else if completed := session.completed; completed != nil {
				// Keep the committed receipt/DTO; only recovery transport expires.
				for _, peer := range completed.comebackConnections {
					peers = append(peers, peer)
				}
				completed.terminalEngine = nil
				completed.comebackTokens = nil
				completed.pendingComebackTokens = nil
				completed.disconnectedUntil = nil
				completed.comebackConnections = nil
			}
			session.Unlock()
			for _, peer := range peers {
				// Closing the transport first also unblocks a slow in-flight write.
				_ = peer.conn.Close()
				_ = peer.close(false)
			}
			break
		}
	}
	return closed, nil
}
