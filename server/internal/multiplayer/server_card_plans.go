package multiplayer

import (
	"fmt"
	"maps"
)

// Managed CPU callers skip KO members; a room still acknowledges their empty
// input. Living automatic inputs all use the catalog's original NPC program.
func npcCardSubmission(batch *CardSelectionBatch, member, enemyType int) (cardPlaySubmission, error) {
	if !batch.IsAlive(member) {
		return cardPlaySubmission{Automatic: true}, nil
	}
	return batch.NPCSubmission(member, enemyType)
}

// CPU and KO members submit as soon as input opens. Living connected humans
// retain manual/timeout selection. KO Submit intentionally emits no native
// rows; their transport acknowledgement must still release the input barrier.
func automaticRoomCardSubmissionFrames(current *room) ([]battleFrame, error) {
	submissions := maps.Clone(current.cardPlaySubmissions)
	if submissions == nil {
		submissions = make(map[int]cardPlaySubmission)
	}
	var results []BattleResult
	var payload string
	err := current.engine.WithCardSelections(func(engine *CardSelectionBatch) error {
		for memberType := 1; memberType <= maxRoomMembers; memberType++ {
			_, submitted := current.cardPlaySubmissions[memberType]
			if engine.HasSubmission(memberType) {
				continue
			}
			// Accepted selections are final. Empty timeout packets are already
			// converted to automatic choices at the transport ingress.
			if submitted || current.connections[memberType] != nil && engine.IsAlive(memberType) {
				continue
			}
			selection, err := npcCardSubmission(engine, memberType, current.enemyType)
			if err != nil {
				return fmt.Errorf("select automatic cards for member %d: %w", memberType, err)
			}
			rows, err := submitRoomCardPlay(engine, memberType, selection)
			if err != nil {
				return fmt.Errorf("submit automatic cards for member %d: %w", memberType, err)
			}
			submissions[memberType] = selection
			results = append(results, rows...)
		}
		var err error
		payload, err = encodeOptionalBattleResults(results)
		return err
	})
	if err != nil {
		return nil, err
	}
	current.cardPlaySubmissions = submissions
	if len(results) == 0 {
		return nil, nil
	}
	return []battleFrame{{"ApiCardPlayR", payload}}, nil
}

func (s *Server) submitAutomaticRoomCards(roomID int64) error {
	hub := s.hub
	session := hub.lockRoomSession(roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || !current.userPhaseStarted ||
		current.userAttackStarted || len(current.connections) == 0 {
		session.Unlock()
		return nil
	}
	if current.engine == nil {
		session.Unlock()
		return fmt.Errorf("automatic card submission has no battle engine")
	}
	frames, err := automaticRoomCardSubmissionFrames(current)
	if err != nil {
		session.Unlock()
		return err
	}
	deliveries := reserveRoomFramesLocked(roomConnections(current), frames...)
	session.Unlock()
	broadcastRoomFrames(s, roomID, deliveries)
	return nil
}
