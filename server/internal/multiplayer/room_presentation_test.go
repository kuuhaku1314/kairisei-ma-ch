package multiplayer

import (
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestRoomPresentationFollowsAuthorizedHost(t *testing.T) {
	for _, sample := range []struct {
		skip   bool
		speed  int
		policy int
	}{{false, 0, 0}, {true, 0, 100}, {false, 150, 0}, {true, 150, 0}, {false, 200, 0}, {true, 200, 100}} {
		skip := sample.skip
		t.Run(strconv.FormatBool(skip)+"/"+strconv.Itoa(sample.speed), func(t *testing.T) {
			_, members := nextBattleFixture(t)
			for i := range members {
				members[i].Level, members[i].LeaderCardID, members[i].LeaderLevel, members[i].LeaderFame = 1, 1, 1, 1
				members[i].PartsIDs, members[i].DeckHonorIDs = make([]int, 7), make([]int, 4)
			}
			h := NewHub()
			operatorSpeed := sample.policy
			if err := h.AttachGameSpeed(func() int { return operatorSpeed }); err != nil {
				t.Fatal(err)
			}
			s := &Server{hub: h, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			left, right := net.Pipe()
			defer left.Close()
			defer right.Close()
			output := &hubCheckingConn{Conn: left, hub: h}
			owner := &clientConn{server: s, conn: output}
			spec := RoomSpec{BossID: 1, EnemyPartyID: 1, HoldMax: 5, Owner: members[3], OwnerFallbackParty: members[:3], SkipBattleMessages: skip, GameSpeed: sample.speed}
			credential, err := h.IssueCreate(spec)
			if err != nil {
				t.Fatal(err)
			}
			spec.SkipBattleMessages = !skip // Authorization owns a snapshot, not the caller's mutable input.
			spec.GameSpeed = 999
			if err := owner.handleCreate(joinCSV(strconv.Itoa(spec.Owner.UserID), "1", "0", "", "4", "4", "0", "0", "0", credential.AuthToken, credential.Signature, "android")); err != nil {
				t.Fatal(err)
			}
			want := "0"
			if skip {
				want = "1"
			}
			frame := strings.SplitN(strings.SplitN(output.output.String(), "RoomCreateRequestResult{\n", 2)[1], "\n", 2)[0]
			wantSpeed := sample.speed
			if sample.policy != 0 {
				wantSpeed = sample.policy
			}
			if wantSpeed == 0 {
				wantSpeed = DefaultGameSpeed
			}
			if fields := splitCSV(frame); len(fields) != 10 || fields[9] != "2" || fields[4] != "4" || fields[8] != want || fields[5] != strconv.Itoa(wantSpeed) {
				t.Fatalf("create presentation tail: %s", frame)
			}
			current := h.rooms[owner.roomID]
			if current.SkipBattleMessages != skip || current.GameSpeed != wantSpeed {
				t.Fatal("authorized host setting lost")
			}
			operatorSpeed = 200
			if _, err := h.Reserve(owner.roomID, members[2].UserID, 3); err != nil {
				t.Fatal(err)
			}
			credential, err = h.IssueEnter(owner.roomID, members[2])
			if err != nil {
				t.Fatal(err)
			}
			guestOutput := &hubCheckingConn{Conn: left, hub: h}
			guest := &clientConn{server: s, conn: guestOutput}
			if err := guest.handleEnter(joinCSV(strconv.Itoa(members[2].UserID), strconv.FormatInt(owner.roomID, 10), "", "3", credential.AuthToken, credential.Signature, "android")); err != nil {
				t.Fatal(err)
			}
			frame = strings.SplitN(strings.SplitN(guestOutput.output.String(), "RoomEnterRequestResult{\n", 2)[1], "\n", 2)[0]
			if fields := splitCSV(frame); len(fields) != 9 || fields[8] != "2" || fields[4] != "4" || fields[7] != want || fields[5] != strconv.Itoa(wantSpeed) {
				t.Fatalf("guest received a different presentation setting: %s", frame)
			}
		})
	}
}

func TestRoomSpeedRejectsUnsupportedPercentages(t *testing.T) {
	_, members := nextBattleFixture(t)
	for _, speed := range []int{-1, 99, 125, 201, 500} {
		if err := validateRoomSpec(RoomSpec{GameSpeed: speed, Owner: members[0]}); err == nil || err.Error() != "room game speed is invalid" {
			t.Fatalf("accepted room speed %d", speed)
		}
	}
}
