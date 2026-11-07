package multiplayer

import (
	"errors"
	"kairisei.local/server/battlecodec"
	"kairisei.local/server/battlecore"
	"strconv"
	"sync"
	"time"
)

type battleFrame struct {
	method  string
	payload string
}

// Reserve under the room session, in state-transition order. Send only after
// releasing the session; each reservation must be sent exactly once.
type frameDelivery struct {
	connection *clientConn
	frames     []battleFrame
	prepared   []*preparedBattleFrame
	previous   <-chan struct{}
	done       chan struct{}
}

func (c *clientConn) reserveFramesLocked(frames []battleFrame) frameDelivery {
	return c.reservePreparedFramesLocked(frames, prepareBattleFrames(frames))
}

func (c *clientConn) reservePreparedFramesLocked(frames []battleFrame, prepared []*preparedBattleFrame) frameDelivery {
	delivery := frameDelivery{connection: c, frames: frames, prepared: prepared, previous: c.lastDelivery, done: make(chan struct{})}
	c.lastDelivery = delivery.done
	return delivery
}

func (delivery frameDelivery) send() error {
	if delivery.previous != nil {
		<-delivery.previous
	}
	defer close(delivery.done)
	c := delivery.connection
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	for _, frame := range delivery.prepared {
		if err := c.writePreparedFrameLocked(frame); err != nil {
			return err
		}
	}
	return nil
}

func reserveRoomFramesLocked(connections []*clientConn, frames ...battleFrame) []frameDelivery {
	if len(frames) == 0 {
		return nil
	}
	deliveries := make([]frameDelivery, 0, len(connections))
	prepared := prepareBattleFrames(frames)
	for _, connection := range connections {
		deliveries = append(deliveries, connection.reservePreparedFramesLocked(frames, prepared))
	}
	return deliveries
}

// Called only for a newly joined connection while holding its session.
func (c *clientConn) writeInitialRoomFramesAndUnlock(session *lockedRoomSession, frames []battleFrame) error {
	delivery := c.reserveFramesLocked(frames)
	session.Unlock()
	return delivery.send()
}

// Original Start emits recovery HP rows before 3af95's CARD identities.
// Keep the same assembly for first start, next wave and API comparisons.
func roomStartResult(current *room, results []BattleResult) (string, error) {
	if current == nil {
		return "", errors.New("battle room party is incomplete")
	}
	identities, err := battlecore.DeckIdentityResults(roomBattleParticipants(current.Members))
	if err != nil {
		return "", err
	}
	return battlecodec.EncodeStart(results, identities)
}

func roomDeckRows(current *room) ([]string, error) {
	if current == nil {
		return nil, errors.New("battle room party is incomplete")
	}
	identities, err := battlecore.DeckIdentityResults(roomBattleParticipants(current.Members))
	if err != nil {
		return nil, err
	}
	rows := make([]string, 0, len(identities))
	for _, result := range identities {
		row, err := battlecodec.EncodeRow(result)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func broadcastRoomFrames(server *Server, roomID int64, deliveries []frameDelivery) {
	for index, err := range sendRoomFrames(deliveries) {
		if err != nil {
			server.logger.Warn("broadcast room frames failed", "room_id", roomID, "method", deliveries[index].frames[0].method, "error", err)
		}
	}
}

func sendRoomFrames(deliveries []frameDelivery) []error {
	// At most four writers, with independent deadlines. A slow peer must not
	// delay directions to healthy peers. No persistent workers or output queue.
	failures := make([]error, len(deliveries))
	var pending sync.WaitGroup
	for index, delivery := range deliveries {
		pending.Add(1)
		go func() {
			defer pending.Done()
			failures[index] = delivery.send()
		}()
	}
	pending.Wait()
	return failures
}

// completeGoBattleLocked consumes the room lease and always releases it.
func (s *Server) completeGoBattleLocked(session *lockedRoomSession, current *room) error {
	current.chaliceInput = roomChaliceInput{}
	if current.nextBattlePending {
		session.Unlock()
		return nil
	}
	if current.completionDone != nil {
		session.Unlock()
		return nil
	}
	endType := current.engineBattleEnd
	connections := append([]*clientConn(nil), roomConnections(current)...)
	roomID := current.RoomID
	connectedMembers := len(connections)
	if next, ok := roomNextBattle(current); ok {
		recordRoomWaveDrops(current)
		current.nextBattlePending, current.nextBattleIndex = true, next
		current.gameNextFinished = make(map[int]bool)
		payload := joinCSV(strconv.Itoa(next), strconv.Itoa(roomBattleProgress(current, next)))
		deliveries := reserveRoomFramesLocked(connections, battleFrame{"GameNextStart", payload})
		session.Unlock()
		broadcastRoomFrames(s, roomID, deliveries)
		return nil
	}
	if err := completeBattle(session, current, time.Now()); err != nil {
		var pending *completionPendingError
		if errors.As(err, &pending) {
			s.scheduleCompletionRetry(roomID)
		}
		return err
	}
	session = s.hub.lockRoomSession(roomID)
	defer session.Unlock()
	if session.completed == nil {
		return nil
	}
	s.scheduleCompletedExpiry(session.completed)
	deliveries := reserveRoomFramesLocked(connections,
		battleFrame{"ApiGameEnd", joinCSV("2", strconv.Itoa(endType))}, battleFrame{"GameClose", ""})
	session.Unlock()
	broadcastRoomFrames(s, roomID, deliveries)
	s.logger.Info(
		"local multiplayer Go battle completed",
		"room_id", roomID,
		"connected_members", connectedMembers,
		"battle_end", endType,
	)
	return nil
}
