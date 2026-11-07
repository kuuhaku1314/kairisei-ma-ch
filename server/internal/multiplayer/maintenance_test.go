package multiplayer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMaintenanceRevokesRoomAdmissionWithoutInterruptingRooms(t *testing.T) {
	_, members := nextBattleFixture(t)
	for i := range members {
		members[i].Level, members[i].LeaderCardID, members[i].LeaderLevel, members[i].LeaderFame = 1, 1, 1, 1
		members[i].PartsIDs, members[i].DeckHonorIDs = make([]int, 7), make([]int, 4)
	}
	h := NewHub()
	h.rooms[1] = &room{RoomSnapshot: RoomSnapshot{RoomID: 1, State: RoomStateOpen, Members: members[:1]}, reservations: map[int]roomReservation{}}
	if _, err := h.Reserve(1, members[1].UserID, 2); err != nil {
		t.Fatal(err)
	}
	entry, err := h.IssueEnter(1, members[1])
	if err != nil {
		t.Fatal(err)
	}
	spec := RoomSpec{BossID: 1, EnemyPartyID: 1, HoldMax: 5, Owner: members[0]}
	if _, err := h.IssueCreate(spec); err != nil {
		t.Fatal(err)
	}
	if err := h.SetMaintenance(true); err != nil {
		t.Fatal(err)
	}
	if len(h.pending) != 0 || h.RoomCount() != 1 {
		t.Fatal("maintenance must revoke entry without waiting for rooms")
	}
	if _, err := h.IssueCreate(spec); !errors.Is(err, ErrMaintenance) {
		t.Fatal("create bypassed maintenance", err)
	}
	if _, err := h.Reserve(1, members[1].UserID, 2); !errors.Is(err, ErrMaintenance) {
		t.Fatal("reservation bypassed maintenance", err)
	}
	if _, err := h.IssueEnter(1, members[1]); !errors.Is(err, ErrMaintenance) {
		t.Fatal("entry bypassed maintenance", err)
	}
	s, _ := NewServer(h, slog.New(slog.NewTextHandler(io.Discard, nil)))
	out := &sessionTestConn{}
	c := &clientConn{server: s, conn: out}
	if err := c.handleEnter(joinCSV(strconv.Itoa(members[1].UserID), "1", "", "2", entry.AuthToken, entry.Signature, "Android")); err != nil {
		t.Fatal(err)
	}
	if c.roomID != 0 || !strings.Contains(out.output.String(), "-1,") {
		t.Fatal("old entry credential accepted")
	}
	current := h.rooms[1]
	current.State = RoomStateBattle
	current.comebackTokens = map[int]string{1: "token"}
	current.disconnectedUntil = map[int]time.Time{1: time.Now().Add(time.Minute)}
	out.output.Reset()
	if err := c.handleComeback(joinCSV(strconv.Itoa(members[0].UserID), "1", "token", "0")); err != nil {
		t.Fatal(err)
	}
	if c.roomID != 0 || !strings.Contains(out.output.String(), "-1,") {
		t.Fatal("comeback bypassed maintenance")
	}
	if err := h.SetMaintenance(false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.IssueCreate(spec); err != nil {
		t.Fatal("admission did not reopen", err)
	}
}

func TestMaintenanceInterruptionWaitsForCommitAndDropsUnsettledRooms(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(strconv.FormatBool(fail), func(t *testing.T) {
			h := NewHub()
			repository := &sessionCompletionRepository{entered: make(chan struct{}), release: make(chan struct{})}
			if fail {
				repository.failure = errors.New("injected transaction failure")
			}
			h.completionCommit = repository.SaveCompleted
			var once sync.Once
			release := func() { once.Do(func() { close(repository.release) }) }
			defer release()
			s, _ := NewServer(h, slog.New(slog.NewTextHandler(io.Discard, nil)))
			peer := &clientConn{server: s, conn: &sessionTestConn{}, roomID: 1, memberType: 1, userID: 1001}
			current := &room{RoomSnapshot: RoomSnapshot{RoomID: 1, State: RoomStateBattle}, connections: map[int]*clientConn{1: peer}, engineBattleEnd: 1}
			h.rooms[1] = current
			h.rooms[2] = &room{RoomSnapshot: RoomSnapshot{RoomID: 2, State: RoomStateOpen}}
			h.rooms[3] = &room{RoomSnapshot: RoomSnapshot{RoomID: 3, State: RoomStateBattle}}
			commitDone := make(chan error, 1)
			go func() { commitDone <- completeBattle(h.lockRoomSession(1), current, time.Now()) }()
			select {
			case <-repository.entered:
			case <-time.After(3 * time.Second):
				t.Fatal("commit did not start")
			}
			if err := h.SetMaintenance(true); err != nil {
				t.Fatal(err)
			}
			// A canceled wait must leave the committing room and transaction intact.
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			_, err := h.InterruptRooms(ctx)
			cancel()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("interruption did not wait for the transaction", err)
			}
			if h.rooms[1] != current {
				t.Fatal("committing room removed")
			}
			release()
			err = awaitSessionWork(t, commitDone)
			if (err != nil) != fail {
				t.Fatal("transaction outcome changed", err)
			}
			if _, err := h.InterruptRooms(context.Background()); err != nil {
				t.Fatal(err)
			}
			if h.RoomCount() != 0 || repository.calls != 1 {
				t.Fatal("unfinished rooms retained or interruption retried settlement")
			}
			if (h.completed[1] != nil) == fail {
				t.Fatal("committed receipt lost or failed transaction published")
			}
			if h.completed[2] != nil || h.completed[3] != nil {
				t.Fatal("interruption synthesized a result")
			}
			peer.writeMu.Lock()
			closed := peer.closed
			peer.writeMu.Unlock()
			if !closed {
				t.Fatal("player connection was retained")
			}
		})
	}
}

func TestMaintenanceRejectsRoomAlreadyBeingPrepared(t *testing.T) {
	_, members := nextBattleFixture(t)
	owner := members[0]
	owner.Level, owner.LeaderCardID, owner.LeaderLevel, owner.LeaderFame = 1, 1, 1, 1
	owner.PartsIDs, owner.DeckHonorIDs = make([]int, 7), make([]int, 4)
	h := NewHub()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	h.gameSpeed = func() int { close(entered); <-release; return DefaultGameSpeed }
	spec := RoomSpec{BossID: 1, EnemyPartyID: 1, HoldMax: 5, Owner: owner}
	credential, err := h.IssueCreate(spec)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := NewServer(h, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c := &clientConn{server: s, conn: &sessionTestConn{}}
	done := make(chan error, 1)
	go func() {
		done <- c.handleCreate(joinCSV(strconv.Itoa(owner.UserID), "1", "0", "", "1", "1", "0", "0", "0", credential.AuthToken, credential.Signature, "Android"))
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("create did not reach preparation")
	}
	if err := h.SetMaintenance(true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	_, err = h.InterruptRooms(ctx)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("unpublished room preparation was not drained", err)
	}
	unblock()
	if err := awaitSessionWork(t, done); err != nil {
		t.Fatal(err)
	}
	if rooms, creating := h.MaintenanceActivity(); rooms != 0 || creating != 0 || c.roomID != 0 {
		t.Fatal("room published after maintenance admission closed")
	}
}
