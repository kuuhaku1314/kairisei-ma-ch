package multiplayer

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// No listener or real account is involved. Writes still follow clientConn's
// normal sequencing/write lock; inspect the buffer only after senders finish.
type sessionTestConn struct {
	net.Conn
	output strings.Builder
}

func (c *sessionTestConn) Write(p []byte) (int, error)    { return c.output.Write(p) }
func (*sessionTestConn) SetWriteDeadline(time.Time) error { return nil }
func (*sessionTestConn) Close() error                     { return nil }

type sessionCompletionRepository struct {
	CompletionRepository
	entered chan struct{}
	release chan struct{}
	failure error
	calls   int
}

func (r *sessionCompletionRepository) SaveCompleted(CompletedBattle, time.Time) error {
	r.calls++
	if r.calls == 1 {
		close(r.entered)
		<-r.release
		return r.failure
	}
	return nil
}

func awaitSessionWork(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("room operation deadlocked")
		return nil
	}
}

func TestCompletionIsolatesRoomsAndPublishesOnlyAfterPersistence(t *testing.T) {
	h := NewHub()
	s := &Server{hub: h, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	repository := &sessionCompletionRepository{entered: make(chan struct{}), release: make(chan struct{}), failure: errors.New("disk unavailable")}
	h.repository = repository
	h.completionCommit = repository.SaveCompleted
	var release sync.Once
	unblock := func() { release.Do(func() { close(repository.release) }) }
	defer unblock()
	out := &sessionTestConn{}
	peer := &clientConn{server: s, conn: out, roomID: 1, memberType: 1, userID: 1001}
	current := &room{RoomSnapshot: RoomSnapshot{RoomID: 1, State: RoomStateBattle},
		connections: map[int]*clientConn{1: peer}, userAttackStarted: true, userAttackFinished: map[int]bool{1: true}, engineBattleEnd: 1}
	h.rooms[1] = current
	engine, members := nextBattleFixture(t)
	if _, err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	other := &clientConn{server: s, conn: &sessionTestConn{}, roomID: 2, memberType: 1, userID: 1001}
	h.rooms[2] = &room{RoomSnapshot: RoomSnapshot{RoomID: 2, State: RoomStateBattle, Members: members},
		engine: engine, gameStarted: true, connections: map[int]*clientConn{1: other}, gameStartFinished: map[int]bool{1: true}}
	h.rooms[3] = &room{RoomSnapshot: RoomSnapshot{RoomID: 3, State: RoomStateOpen}}
	done := make(chan error, 1)
	go func() { done <- s.tryAdvanceUserAttack(1) }()
	select {
	case <-repository.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("completion did not start")
	}
	unrelated := make(chan error, 1)
	go func() {
		if _, err := h.SettlementFor(1, 1001); !errors.Is(err, ErrCompletedBattlePending) {
			unrelated <- errors.New("pending completion held its room lock or became visible")
			return
		}
		if err := s.tryAdvanceUserAttack(1); err != nil {
			unrelated <- err
			return
		}
		if got := h.List(RoomSearch{}); len(got) != 1 || got[0].RoomID != 3 {
			unrelated <- errors.New("search lost unrelated room")
			return
		}
		if len(h.ActivitySnapshot()) != 3 {
			unrelated <- errors.New("activity lost a room")
			return
		}
		if _, err := h.Reserve(3, 1002, 2); err != nil {
			unrelated <- err
			return
		}
		unrelated <- s.tryAdvanceGameStart(2)
	}()
	if err := awaitSessionWork(t, unrelated); err != nil {
		t.Fatal(err)
	}
	if engine.TurnNumber() != 1 {
		t.Fatal("unrelated engine did not advance")
	}
	if snapshot, exists := h.Snapshot(1); !exists || snapshot.State != RoomStateBattle {
		t.Fatal("uncommitted result became visible")
	}
	unblock()
	if err := awaitSessionWork(t, done); !errors.Is(err, repository.failure) {
		t.Fatal("persistence failure was hidden", err)
	}
	if out.output.Len() != 0 {
		t.Fatal("failure emitted terminal frames")
	}
	if _, err := h.SettlementFor(1, 1001); !errors.Is(err, ErrCompletedBattlePending) {
		t.Fatal("failed write published settlement", err)
	}
	owner := current.session
	if err := s.tryAdvanceUserAttack(1); err != nil {
		t.Fatal(err)
	}
	session := h.lockRoomSession(1)
	if session.room != nil || session.completed == nil || session.owner != owner {
		session.Unlock()
		t.Fatal("completion changed session ownership")
	}
	session.Unlock()
	if _, err := h.SettlementFor(1, 1001); err != nil {
		t.Fatal(err)
	}
	if repository.calls != 2 || strings.Count(out.output.String(), "GameClose{") != 1 {
		t.Fatal("retry duplicated completion")
	}
}

func TestConcurrentRoomsAndDuplicateAcknowledgementsMatchSerialBattle(t *testing.T) {
	_, members := nextBattleFixture(t)
	h := NewHub()
	s := &Server{hub: h, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	const roomCount = 12
	outputs := make([]*sessionTestConn, roomCount+1)
	for id := 1; id <= roomCount+1; id++ {
		engine, err := newBattleEngine(roomFixtureCatalog(), RoomSpec{EnemyPartyID: 1, CostInitial: 3, HoldMax: 2, Seed: 602}, members)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = engine.Start(); err != nil {
			t.Fatal(err)
		}
		outputs[id-1] = &sessionTestConn{}
		current := &room{RoomSnapshot: RoomSnapshot{RoomID: int64(id), BossID: 1, State: RoomStateBattle, Members: members}, engine: engine, gameStarted: true,
			connections: map[int]*clientConn{}, gameStartFinished: map[int]bool{}, turnPhaseFinished: map[int]bool{}, cardPlaySubmissions: map[int]cardPlaySubmission{}, userAttackFinished: map[int]bool{}}
		for slot := 1; slot <= 2; slot++ {
			out := &sessionTestConn{}
			if slot == 1 {
				out = outputs[id-1]
			}
			current.connections[slot] = &clientConn{server: s, conn: out, roomID: int64(id), memberType: slot, userID: 1000 + slot}
		}
		h.rooms[int64(id)] = current
	}
	run := func(id int64, concurrent bool) error {
		session := h.lockRoomSession(id)
		current := session.room
		peers := roomConnections(current)
		session.Unlock()
		acknowledge := func(step func(*clientConn) error, repeats int) error {
			if !concurrent {
				for _, peer := range peers {
					if err := step(peer); err != nil {
						return err
					}
				}
				return nil
			}
			results := make(chan error, len(peers)*repeats)
			for _, peer := range peers {
				for repeat := 0; repeat < repeats; repeat++ {
					go func() { results <- step(peer) }()
				}
			}
			var failure error
			for count := 0; count < len(peers)*repeats; count++ {
				if err := <-results; err != nil {
					failure = err
				}
			}
			return failure
		}
		if err := acknowledge((*clientConn).handleGameStartFinish, 4); err != nil {
			return err
		}
		if err := acknowledge((*clientConn).handleTurnPhaseFinish, 4); err != nil {
			return err
		}
		session = h.lockRoomSession(id)
		current.cardPlaySubmissions[1] = cardPlaySubmission{CardTypes: [5]int{firstFixtureHandCard(t, current.engine, 1)}, Targets: [5]int{5}}
		for slot := 2; slot <= 4; slot++ {
			current.cardPlaySubmissions[slot] = cardPlaySubmission{}
		}
		session.Unlock()
		if err := s.tryAdvanceCardPlay(id); err != nil {
			return err
		}
		return acknowledge((*clientConn).handleUserAttackFinish, 1)
	}
	if err := run(1, false); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, roomCount)
	for id := 2; id <= roomCount+1; id++ {
		go func() { done <- run(int64(id), true) }()
	}
	for id := 2; id <= roomCount+1; id++ {
		if err := awaitSessionWork(t, done); err != nil {
			t.Fatal(err)
		}
	}
	want := outputs[0].output.String()
	if strings.Count(want, "GameClose{") != 1 {
		t.Fatal("serial reference battle did not finish")
	}
	for index, output := range outputs {
		if output.output.String() != want {
			t.Fatalf("room %d changed native results or frame order", index+1)
		}
		if _, err := h.SettlementFor(int64(index+1), 1001); err != nil {
			t.Fatal(err)
		}
	}
}

func TestConcurrentReservationMovesStayExclusive(t *testing.T) {
	h := NewHub()
	for id := int64(1); id <= 2; id++ {
		h.rooms[id] = &room{RoomSnapshot: RoomSnapshot{RoomID: id, State: RoomStateOpen}}
	}
	done := make(chan error, 4)
	for worker := 0; worker < 4; worker++ {
		go func() {
			for turn := 0; turn < 30; turn++ {
				id := int64(1 + (turn+worker)%2)
				if _, err := h.Reserve(id, 1001, 2); err != nil {
					done <- err
					return
				}
				if worker == 0 {
					if err := h.CancelReservation(id, 1001, 2); err != nil {
						done <- err
						return
					}
				}
				h.ReservationFor(1001)
				h.List(RoomSearch{UserID: 1002, ArthurType: 2})
			}
			done <- nil
		}()
	}
	for worker := 0; worker < 4; worker++ {
		if err := awaitSessionWork(t, done); err != nil {
			t.Fatal(err)
		}
	}
	count := 0
	for _, view := range h.roomViews() {
		for _, reservation := range view.reservations {
			if reservation.UserID == 1001 {
				count++
			}
		}
	}
	if count > 1 {
		t.Fatal("concurrent reservation move duplicated one user across rooms")
	}
}
