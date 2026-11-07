package multiplayer

import (
	"slices"
	"sort"
	"time"
)

type RoomActivity struct {
	RoomID       int64     `json:"room_id"`
	BossID       int       `json:"boss_id"`
	State        RoomState `json:"state"`
	GameSpeed    int       `json:"game_speed"`
	Turn         int       `json:"turn"`
	Wave         int       `json:"wave"`
	Players      []int     `json:"players"`
	Disconnected int       `json:"disconnected"`
	AI           int       `json:"ai"`
}

// Never expose lobby passwords, comeback tokens or connection credentials.
func (h *Hub) ActivitySnapshot() []RoomActivity {
	views := h.roomViews()
	result := make([]RoomActivity, 0, len(views))
	for _, view := range views {
		row := view.activity
		row.Players = slices.Clone(row.Players)
		result = append(result, row)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].RoomID < result[j].RoomID })
	return result
}

// RoomSynchronization is an on-demand diagnostic of one room, sampled under
// its session lock. Different rooms and the ordinary activity view may have
// different sample times. No credential or engine snapshot is exposed.
type RoomSynchronization struct {
	RoomID          int64                           `json:"room_id"`
	SampledAt       time.Time                       `json:"sampled_at"`
	State           RoomState                       `json:"state"`
	Turn            int                             `json:"turn"`
	Wave            int                             `json:"wave"`
	EngineBattleEnd int                             `json:"engine_battle_end"`
	Closing         bool                            `json:"closing"`
	Phase           string                          `json:"phase"`
	ExpectedMessage string                          `json:"expected_message"`
	BarrierReady    *bool                           `json:"barrier_ready"`
	MissingMembers  []int                           `json:"missing_message_member_types"`
	Connections     []RoomConnectionSynchronization `json:"connections"`
}

type RoomConnectionSynchronization struct {
	MemberType      int   `json:"member_type"`
	UserID          int   `json:"user_id"`
	ComebackPending bool  `json:"comeback_pending"`
	RecoveryVersion int   `json:"recovery_version"`
	RecoveryApplied bool  `json:"recovery_applied"`
	RecoveryWaiting bool  `json:"waiting_for_recovery_application"`
	MessageReceived *bool `json:"expected_message_received"`
}

// This is deliberately separate from ActivitySnapshot: ordinary Admin polling
// keeps using the published read model and never waits for a battle session.
func (h *Hub) RoomSynchronizationSnapshot() []RoomSynchronization {
	h.mu.RLock()
	ids := make([]int64, 0, len(h.rooms))
	for id := range h.rooms {
		ids = append(ids, id)
	}
	h.mu.RUnlock()
	slices.Sort(ids)
	result := make([]RoomSynchronization, 0, len(ids))
	for _, id := range ids {
		session := h.lockRoomSession(id)
		if current := session.room; current != nil {
			result = append(result, roomSynchronizationSnapshot(current))
		}
		session.Unlock()
	}
	return result
}

// The caller holds the room session. Inspect only scalar fields and the small
// acknowledgement/connection maps; never run the engine or acquire account IO.
func roomSynchronizationSnapshot(current *room) RoomSynchronization {
	row := RoomSynchronization{
		RoomID: current.RoomID, SampledAt: time.Now().UTC(), State: current.State,
		Turn: current.turnNumber, Wave: current.battleIndex + 1,
		EngineBattleEnd: current.engineBattleEnd, Closing: current.closing,
		Phase: string(current.State), MissingMembers: []int{},
		Connections: make([]RoomConnectionSynchronization, 0, len(current.connections)),
	}
	var acknowledgements map[int]bool
	switch {
	case current.State != RoomStateBattle:
	case current.completionDone != nil:
		row.Phase = "completion_write"
	case current.pendingCompletion != nil:
		row.Phase = "completion_retry"
	case current.continueDone != nil:
		row.Phase = "continue_write"
	case current.continuation != nil:
		row.Phase = "continue_decision"
		if current.continuation.presentation != nil {
			row.Phase, row.ExpectedMessage = "continue_presentation", "GameOverPhaseFinish"
			acknowledgements = current.continuation.finished
		}
	case current.nextBattlePending:
		row.Phase, row.ExpectedMessage = "next_wave", "GameNextFinish"
		acknowledgements = current.gameNextFinished
	case !current.gameStarted:
		row.Phase = "battle_loading"
	default:
		// Share the recovery path's choice of presentation acknowledgement.
		row.ExpectedMessage = comebackAcknowledgement(current)
		switch row.ExpectedMessage {
		case "GameStartFinish":
			row.Phase, acknowledgements = "game_start", current.gameStartFinished
		case "TurnPhaseFinish":
			row.Phase, acknowledgements = "turn_start", current.turnPhaseFinished
		case "UserAttackFinish":
			row.Phase, acknowledgements = "user_attack", current.userAttackFinished
		case "ChaliceSphrExecUserPhaseFinish":
			row.Phase, acknowledgements = "chalice_user", current.chaliceUserFinished
		case "EnemyPhaseFinish":
			row.Phase, acknowledgements = "enemy_attack", current.enemyPhaseFinished
		case "ChaliceSphrExecEnemyPhaseFinish":
			row.Phase, acknowledgements = "chalice_enemy", current.chaliceEnemyFinished
		default:
			row.Phase, row.ExpectedMessage = "user_input", "CardPlay"
			acknowledgements = make(map[int]bool, len(current.cardPlaySubmissions))
			for memberType := range current.cardPlaySubmissions {
				acknowledgements[memberType] = true
			}
		}
	}
	if row.ExpectedMessage != "" {
		ready := roomBarrierReady(current.connections, acknowledgements)
		row.BarrierReady = &ready
	}
	for memberType, connection := range current.connections {
		entry := RoomConnectionSynchronization{
			MemberType: memberType, UserID: connection.userID,
			ComebackPending: connection.comebackPending, RecoveryVersion: connection.recoveryVersion,
			RecoveryApplied: connection.recoveryApplied, RecoveryWaiting: connection.waitingForRecoveryApplication(),
		}
		if row.ExpectedMessage != "" {
			_, received := acknowledgements[memberType]
			entry.MessageReceived = &received
			if !received {
				row.MissingMembers = append(row.MissingMembers, memberType)
			}
		}
		row.Connections = append(row.Connections, entry)
	}
	slices.Sort(row.MissingMembers)
	sort.Slice(row.Connections, func(i, j int) bool { return row.Connections[i].MemberType < row.Connections[j].MemberType })
	return row
}
