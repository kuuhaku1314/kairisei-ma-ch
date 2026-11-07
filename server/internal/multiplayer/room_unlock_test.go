package multiplayer

import (
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestRoomUnlockPublishesRoomAndAdmitsWithoutPassword(t *testing.T) {
	_, members := nextBattleFixture(t)
	for i := range members {
		members[i].Level, members[i].LeaderCardID, members[i].LeaderLevel, members[i].LeaderFame = 1, 1, 1, 1
		members[i].PartsIDs, members[i].DeckHonorIDs = make([]int, 7), make([]int, 4)
	}
	h := NewHub()
	s := &Server{hub: h, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	ownerOut := &hubCheckingConn{Conn: left, hub: h}
	guestOut := &hubCheckingConn{Conn: left, hub: h}
	owner := &clientConn{server: s, conn: ownerOut}
	guest := &clientConn{server: s, conn: guestOut}
	spec := RoomSpec{BossID: 1, EnemyPartyID: 1, HoldMax: 5, Owner: members[0], RoomType: 1, Password: "1234", NeedFame: 1, Comment: "keep"}
	credential, err := h.IssueCreate(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.handleCreate(joinCSV(strconv.Itoa(spec.Owner.UserID), "1", "1", "1234", "1", "1", "0", "0", "0", credential.AuthToken, credential.Signature, "android")); err != nil {
		t.Fatal(err)
	}
	enter := func(c *clientConn, member Member, password string) {
		t.Helper()
		if _, err := h.Reserve(owner.roomID, member.UserID, member.ArthurType); err != nil {
			t.Fatal(err)
		}
		credential, err := h.IssueEnter(owner.roomID, member)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.handleEnter(joinCSV(strconv.Itoa(member.UserID), strconv.FormatInt(owner.roomID, 10), password, strconv.Itoa(member.ArthurType), credential.AuthToken, credential.Signature, "android")); err != nil {
			t.Fatal(err)
		}
		if c.roomID != owner.roomID {
			t.Fatal("guest was not admitted")
		}
	}
	enter(guest, members[1], "1234")
	current := h.rooms[owner.roomID]
	if len(h.List(RoomSearch{BossID: 1})) != 0 {
		t.Fatal("locked room is publicly listed")
	}
	if guest.handleRoomMatchingConditionReset("") == nil || !current.HasPassword || current.password() != "1234" {
		t.Fatal("non-owner unlocked the room")
	}
	ownerOut.output.Reset()
	guestOut.output.Reset()
	for attempt := 0; attempt < 2; attempt++ {
		if err := owner.handleRoomMatchingConditionReset(""); err != nil {
			t.Fatal(err)
		}
	}
	if current.HasPassword || current.password() != "" || current.RoomType != 0 || current.NeedFame != 1 || current.Comment != "keep" {
		t.Fatal("unlock failed or altered unrelated room requirements")
	}
	listed := h.List(RoomSearch{BossID: 1})
	if len(listed) != 1 || listed[0].HasPassword || len(h.List(RoomSearch{BossID: 1, Password: "1234"})) != 0 {
		t.Fatal("search retained the old password")
	}
	for _, out := range []*hubCheckingConn{ownerOut, guestOut} {
		if out.locked || strings.Count(out.output.String(), "RoomMatchingConditionReset{\n}\n") != 2 {
			t.Fatalf("peer did not receive native unlock callback outside lock: %q", out.output.String())
		}
	}
	third := &clientConn{server: s, conn: &hubCheckingConn{Conn: left, hub: h}}
	enter(third, members[2], "")
	if len(current.Members) != 3 {
		t.Fatal("unlocked room did not admit an empty-password guest")
	}
}
