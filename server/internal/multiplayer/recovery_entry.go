package multiplayer

import (
	"sort"
	"time"
)

// RecoveryEntry contains only the existing room setup consumers. It does not
// start a room or replay gameplay. The authoritative live snapshot still comes
// through Comeback/ReadyToComeback after the scene is ready.
type RecoveryEntry struct {
	BossID      int      `json:"-"`
	RoomInfo    string   `json:"room_info"`
	Members     []string `json:"members"`
	Countdown   string   `json:"countdown"`
	BossGroup   any      `json:"boss_group"`
	BattleIndex int      `json:"battle_index"`
	Progress    int      `json:"progress"`
}

// RecoveryFor must receive the authenticated account identity, never a request
// body user ID. Querying does not rotate tokens or take over a connection.
// Login offers active rooms only: an already-finished battle must not be opened
// again on a normal later login. Existing saved result reports keep priority;
// a room completing after this query is handled by terminal TCP recovery.
func (h *Hub) RecoveryFor(userID int) *RecoveryEntry {
	if userID <= 0 {
		return nil
	}
	h.mu.RLock()
	if h.maintenance {
		h.mu.RUnlock()
		return nil
	}
	ids := make([]int64, 0, len(h.rooms))
	for id := range h.rooms {
		ids = append(ids, id)
	}
	h.mu.RUnlock()
	// If an account has left an older recoverable run behind, its most recently
	// created eligible room wins deterministically.
	sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
	now := time.Now()
	for _, id := range ids {
		session := h.lockRoomSession(id)
		var entry *RecoveryEntry
		if r := session.room; r != nil && r.State == RoomStateBattle && !r.closing {
			member := roomMemberTypeForUser(r, userID)
			connection := r.connections[member]
			if member > 0 && r.comebackTokens[member] != "" && recoveryMemberAlive(r, member) &&
				(connection != nil && !connection.retired || connection == nil && r.disconnectedUntil[member].After(now)) {
				entry = newRecoveryEntry(r.RoomSnapshot, roomCountdownPayload(r), r.battleIndex, r.progress, r.comebackTokens[member])
			}
		}
		session.Unlock()
		if entry != nil {
			return entry
		}
	}
	return nil
}

func newRecoveryEntry(snapshot RoomSnapshot, countdown string, index, progress int, token string) *RecoveryEntry {
	snapshot = cloneRoomSnapshot(snapshot)
	if snapshot.RoomID <= 0 || snapshot.BossGroup == nil || countdown == "" {
		return nil
	}
	members := make([]string, 0, len(snapshot.Members))
	for _, member := range snapshot.Members {
		members = append(members, memberCSV(member))
	}
	return &RecoveryEntry{BossID: snapshot.BossID, RoomInfo: roomInfoCSV(snapshot, token, false), Members: members,
		Countdown: countdown, BossGroup: snapshot.BossGroup, BattleIndex: index, Progress: progress}
}
