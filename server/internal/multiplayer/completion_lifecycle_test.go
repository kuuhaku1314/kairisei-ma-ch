package multiplayer

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// Uses an already-terminal room projection, no engine implementation changes.
// Exercise the real connection dispatcher and its deferred disconnect cleanup.
func TestTerminalCommitRetriesAfterLastConnectionLeaves(t *testing.T) {
	h := NewHub()
	var calls atomic.Int32
	firstFailure := make(chan struct{})
	committed := make(chan CompletedBattle, 1)
	h.completionCommit = func(result CompletedBattle, _ time.Time) error {
		if calls.Add(1) == 1 {
			close(firstFailure)
			return errors.New("injected transient save failure")
		}
		committed <- result
		return nil
	}
	s, err := NewServer(h, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	serverConn, client := net.Pipe()
	defer client.Close()
	c := &clientConn{server: s, conn: serverConn, roomID: 123, memberType: 1, userID: 1001}
	s.conns[c] = struct{}{}
	current := &room{RoomSnapshot: RoomSnapshot{RoomID: 123, BossID: 30010102, OwnerMemberType: 1, State: RoomStateBattle}, connections: map[int]*clientConn{1: c}, userAttackStarted: true, userAttackFinished: map[int]bool{}, engineBattleEnd: 1, hostCostPaid: true, comebackTokens: map[int]string{1: "fixture-token"}, disconnectedUntil: map[int]time.Time{}}
	for i := 1; i <= 4; i++ {
		current.Members = append(current.Members, Member{MemberType: i, UserID: 1000 + i, ArthurType: i, LeaderFame: 1})
	}
	h.rooms[123] = current
	done := make(chan struct{})
	go func() { defer close(done); c.serve() }()
	if _, err := io.WriteString(client, "UserAttackFinish{\n}\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-firstFailure:
	case <-time.After(3 * time.Second):
		t.Fatal("terminal commit did not run")
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("disconnect did not finish")
	}
	if _, err := h.SettlementFor(123, 1001); !errors.Is(err, ErrCompletedBattlePending) {
		t.Fatal("pending result lost", err)
	}
	select {
	case result := <-committed:
		if len(result.OnlineUserIDs) != 1 || result.OnlineUserIDs[0] != 1001 {
			t.Fatal("disconnect changed reward eligibility")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("terminal commit was not retried")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.SettlementFor(123, 1001); err != nil {
		t.Fatal("result unavailable after retry", err)
	}
	if calls.Load() != 2 {
		t.Fatal("duplicate commit", calls.Load())
	}
}
