package multiplayer

import (
	"errors"
	"testing"
)

func TestGameplayRoomThresholds(t *testing.T) {
	h := NewHub()
	h.rooms[1] = &room{RoomSnapshot: RoomSnapshot{RoomID: 1, State: RoomStateOpen,
		NeedHP: 99999, NeedDeckRank: 10, NeedFame: 100,
		Members: []Member{{MemberType: 1, UserID: 1, ArthurType: 1}}}, reservations: map[int]roomReservation{}}
	member := Member{MemberType: 2, UserID: 2, ArthurType: 2, Level: 1, HP: 100,
		LeaderCardID: 10165084, LeaderLevel: 1, LeaderFame: 1, DeckRank: 0,
		PartsIDs: make([]int, 7), DeckHonorIDs: make([]int, 4)}
	for i := 1; i <= 10; i++ {
		member.DeckCards = append(member.DeckCards, BattleCard{CardType: i, CardID: 10165084, Level: 1})
	}
	if _, err := h.Reserve(1, 2, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := h.IssueEnter(1, member); err == nil || errors.Is(err, ErrRoomUnavailable) || errors.Is(err, ErrRoomArthurUnavailable) {
		t.Fatalf("underqualified deck must retain its threshold rejection, got %v", err)
	}
	h.rooms[1].NeedHP, h.rooms[1].NeedDeckRank, h.rooms[1].NeedFame = 0, 0, 0
	if err := h.CancelReservation(1, member.UserID, member.ArthurType); err != nil {
		t.Fatal(err)
	}
	if _, err := h.IssueEnter(1, member); !errors.Is(err, ErrRoomUnavailable) {
		t.Fatalf("lost reservation did not return room availability rejection: %v", err)
	}
	h.rooms[1].Members = append(h.rooms[1].Members, Member{UserID: 3, ArthurType: member.ArthurType})
	if _, err := h.IssueEnter(1, member); !errors.Is(err, ErrRoomArthurUnavailable) {
		t.Fatalf("occupied profession did not return entry rejection: %v", err)
	}
	delete(h.rooms, 1)
	if err := h.CancelReservation(1, member.UserID, member.ArthurType); err != nil {
		t.Fatalf("room removal did not complete cancellation: %v", err)
	}
}
