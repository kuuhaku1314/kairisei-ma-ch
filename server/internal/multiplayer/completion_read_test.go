package multiplayer

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type completionReadRepository struct {
	CompletionRepository
	load func() (CompletedBattle, time.Time, error)
}

func (r completionReadRepository) LoadCompleted(int64, time.Time) (CompletedBattle, time.Time, error) {
	return r.load()
}

func TestCompletionReadDoesNotBlockRoomsOrReplaceTerminalState(t *testing.T) {
	hub := NewHub()
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var calls atomic.Int32
	hub.repository = completionReadRepository{load: func() (CompletedBattle, time.Time, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return CompletedBattle{RoomID: 123, OnlineUserIDs: []int{1001}}, time.Now().Add(time.Hour), nil
	}}
	first := make(chan error, 1)
	go func() { _, err := hub.SettlementFor(123, 1001); first <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("completion lookup did not start")
	}
	later := make(chan error, 1)
	go func() {
		hub.List(RoomSearch{})
		if _, err := hub.SettlementFor(123, 1001); err != nil {
			later <- err
			return
		}
		session := hub.lockRoomSession(123)
		session.completed.terminalBattleEndType = 1
		session.Unlock()
		later <- nil
	}()
	select {
	case err := <-later:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("completion I/O blocked unrelated room access")
	}
	unblock()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	session := hub.lockRoomSession(123)
	defer session.Unlock()
	if session.completed.terminalBattleEndType != 1 {
		t.Fatal("late read replaced terminal interaction state")
	}
}

func TestCompletionExpiredDuringReadIsNotRepublished(t *testing.T) {
	hub := NewHub()
	hub.repository = completionReadRepository{load: func() (CompletedBattle, time.Time, error) {
		return CompletedBattle{RoomID: 123, OnlineUserIDs: []int{1001}}, time.Now().Add(-time.Second), nil
	}}
	if _, err := hub.SettlementFor(123, 1001); !errors.Is(err, ErrCompletedBattleUnavailable) {
		t.Fatalf("expired completion was republished: %v", err)
	}
}
