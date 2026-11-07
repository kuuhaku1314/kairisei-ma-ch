package multiplayer

import (
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

func TestRetireReleasesParticipationBeforeClientCloses(t *testing.T) {
	_, members := nextBattleFixture(t)
	hub := NewHub()
	server := &Server{hub: hub, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	left, peer := net.Pipe()
	defer left.Close()
	defer peer.Close()
	retiring := &clientConn{server: server, conn: left, roomID: 123, memberType: 1, userID: 1001}
	guestLeft, guestPeer := net.Pipe()
	defer guestLeft.Close()
	defer guestPeer.Close()
	guest := &clientConn{server: server, conn: guestLeft, roomID: 123, memberType: 2, userID: 1002}
	current := &room{RoomSnapshot: RoomSnapshot{RoomID: 123, BossID: 1, OwnerMemberType: 1, State: RoomStateBattle, Members: members},
		gameStarted: true, connections: map[int]*clientConn{1: retiring, 2: guest},
		comebackTokens: map[int]string{1: "retire-token", 2: "guest-token"}, disconnectedUntil: make(map[int]time.Time)}
	hub.rooms[123] = current
	// Deliberately keep the client side open: the received Retire itself must
	// release the participant without depending on TCP FIN reaching the server.
	if err := retiring.handleRetire(""); err != nil {
		t.Fatal(err)
	}
	if current.connections[1] != nil || current.comebackTokens[1] != "" || !current.disconnectedUntil[1].IsZero() {
		t.Fatal("Retire retained its live barrier or recovery credential until client FIN")
	}
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := peer.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("retired socket remained open: %v", err)
	}
	if current.connections[2] != guest || len(current.Members) != 4 {
		t.Fatal("retirement removed another player or the CPU character")
	}
	// A completion committed while the socket cleanup is pending must also
	// respect the retirement decision made under Hub lock.
	current.connections[1] = retiring
	current.engineBattleEnd = 1
	session := hub.lockRoomSession(current.RoomID)
	err := completeBattle(session, current, time.Now())
	session.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hub.SettlementFor(123, 1001); err == nil {
		t.Fatal("retired player retained victory reward eligibility")
	}
	if _, err := hub.SettlementFor(123, 1002); err != nil {
		t.Fatal("online guest lost victory reward eligibility", err)
	}
}
