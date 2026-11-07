package multiplayer

import "time"

// A completed outcome is immutable. GameClose keeps its native order and the
// client closes its own socket; these identities only admit in-flight traffic
// until that close (or a terminal comeback's native scene cleanup). This does
// not keep clients connected or reopen a completed gameplay/input phase.
const battleInteractionLifetime = 2 * time.Minute

// The room session must be held. A copied/stale socket or a not-yet-restored comeback
// cannot use another player's interaction channel.
func (c *clientConn) completedInteractionLocked(completed *completedBattle, now time.Time) *completedBattle {
	if completed == nil || c.memberType < 1 || c.memberType > maxRoomMembers ||
		completed.comebackConnections[c.memberType] != c || c.retired || c.comebackPending ||
		!c.finishingDeadline().After(now) || !completed.expiresAt.After(now) {
		return nil
	}
	return completed
}

func completedInteractionPeers(completed *completedBattle, now time.Time) []*clientConn {
	peers := make([]*clientConn, 0, maxRoomMembers)
	for member := 1; member <= maxRoomMembers; member++ {
		peer := completed.comebackConnections[member]
		if peer != nil && !peer.retired && !peer.comebackPending && peer.finishingDeadline().After(now) {
			peers = append(peers, peer)
		}
	}
	return peers
}

func (c *clientConn) readDeadline() time.Time {
	deadline := time.Now().Add(2 * time.Minute)
	if finishing := c.finishingDeadline(); !finishing.IsZero() && finishing.Before(deadline) {
		deadline = finishing
	}
	c.deadlineMu.Lock()
	if !c.recoveryUntil.IsZero() && c.recoveryUntil.Before(deadline) {
		deadline = c.recoveryUntil
	}
	c.deadlineMu.Unlock()
	return deadline
}

// An unconfirmed recovery cannot keep the phase barrier held indefinitely by
// sending Ping. The existing read loop enforces this absolute deadline.
func (c *clientConn) setRecoveryDeadline(deadline time.Time) {
	c.deadlineMu.Lock()
	c.recoveryUntil = deadline
	c.deadlineMu.Unlock()
}

func (c *clientConn) finishingDeadline() time.Time {
	c.deadlineMu.Lock()
	defer c.deadlineMu.Unlock()
	return c.finishingUntil
}

func (c *clientConn) setFinishingDeadline(deadline time.Time) {
	c.deadlineMu.Lock()
	c.finishingUntil = deadline
	c.deadlineMu.Unlock()
}
