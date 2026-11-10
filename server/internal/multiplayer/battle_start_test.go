package multiplayer

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/battlecore"
	"kairisei.local/server/internal/gamestate"
)

func TestLobbyVacanciesFollowMissingProfessions(t *testing.T) {
	_, members := nextBattleFixture(t)
	for i := range members {
		members[i].Level, members[i].LeaderCardID, members[i].LeaderLevel, members[i].LeaderFame = 1, 1, 1, 1
		members[i].PartsIDs, members[i].DeckHonorIDs = make([]int, 7), make([]int, 4)
	}
	h := NewHub()
	speed := 150
	if err := h.AttachGameSpeed(func() int { return speed }); err != nil {
		t.Fatal(err)
	}
	s := &Server{hub: h, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	left, peer := net.Pipe()
	defer left.Close()
	defer peer.Close()
	output := &hubCheckingConn{Conn: left, hub: h}
	owner := &clientConn{server: s, conn: output}
	spec := RoomSpec{BossID: 1, EnemyPartyID: 1, HoldMax: 5, Owner: members[3], OwnerFallbackParty: members[:3]}
	credential, err := h.IssueCreate(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.handleCreate(joinCSV(strconv.Itoa(spec.Owner.UserID), "1", "0", "", "4", "4", "0", "0", "0", credential.AuthToken, credential.Signature, "android")); err != nil {
		t.Fatal(err)
	}
	created := h.rooms[owner.roomID]
	if owner.memberType != 4 || created.OwnerMemberType != 4 || created.connections[4] != owner || created.comebackTokens[4] == "" ||
		created.Members[0].MemberType != 4 || created.Members[0].UserID != spec.Owner.UserID {
		t.Fatal("singer owner lost its profession slot, host identity or reconnect binding")
	}
	response := strings.SplitN(strings.SplitN(output.output.String(), "RoomCreateRequestResult{\n", 2)[1], "\n", 2)[0]
	if fields := splitCSV(response); len(fields) != 10 || fields[9] != "2" || fields[4] != "4" || fields[8] != "0" {
		t.Fatal("native room response did not identify the non-slot-one owner", response)
	}
	visible := map[string][]string{}
	check := func(humans int) {
		t.Helper()
		for _, frame := range strings.Split(output.output.String(), "RoomMember{\n")[1:] {
			fields := splitCSV(strings.SplitN(frame, "\n", 2)[0])
			visible[fields[0]] = fields
		}
		output.output.Reset()
		professions, actualHumans := map[string]bool{}, 0
		for _, fields := range visible {
			professions[fields[3]] = true
			if fields[0] != fields[3] {
				t.Fatal("native member order differs from profession order", fields)
			}
			if fields[1] != "0" {
				actualHumans++
			} else if fields[9] != "1" {
				t.Fatal("empty slot loads a human model")
			}
		}
		if len(visible) != 4 || len(professions) != 4 || actualHumans != humans || len(h.rooms[owner.roomID].Members) != humans {
			t.Fatalf("lobby lost a profession or added phantom members: humans=%d visible=%v", humans, visible)
		}
	}
	check(1)
	speed = 200
	if snapshot := h.ActivitySnapshot(); len(snapshot) != 1 || snapshot[0].GameSpeed != 150 || len(snapshot[0].Players) != 1 || snapshot[0].AI != 0 {
		t.Fatalf("existing lobby changed speed or counted empty seats as players: %+v", snapshot)
	}
	thief := members[2]
	if _, err := h.Reserve(owner.roomID, thief.UserID, 3); err != nil {
		t.Fatal(err)
	}
	credential, err = h.IssueEnter(owner.roomID, thief)
	if err != nil {
		t.Fatal(err)
	}
	guest := &clientConn{server: s, conn: &hubCheckingConn{Conn: left, hub: h}}
	if err := guest.handleEnter(joinCSV(strconv.Itoa(thief.UserID), strconv.FormatInt(owner.roomID, 10), "", "3", credential.AuthToken, credential.Signature, "android")); err != nil {
		t.Fatal(err)
	}
	if guest.memberType != 3 || created.connections[3] != guest {
		t.Fatal("thief joined a different profession slot")
	}
	check(2)
	if snapshot := h.ActivitySnapshot(); len(snapshot[0].Players) != 2 || snapshot[0].GameSpeed != 150 {
		t.Fatal("guest changed room speed or activity", snapshot)
	}
	if err := guest.close(true); err != nil {
		t.Fatal(err)
	}
	check(1)
}

func TestCountdownRequiresOnlyMissingProfessionDecks(t *testing.T) {
	_, members := nextBattleFixture(t)
	for i := range members {
		members[i].Level, members[i].LeaderCardID, members[i].LeaderLevel, members[i].LeaderFame = 1, 1, 1, 1
		members[i].PartsIDs, members[i].DeckHonorIDs = make([]int, 7), make([]int, 4)
		members[i].IsRoomLoading = 1
	}
	spec := RoomSpec{BossID: 1, EnemyPartyID: 1, HoldMax: 5, Owner: members[0]}
	if err := validateRoomSpec(spec); err != nil {
		t.Fatal("a human-only room must not require the owner's unused decks", err)
	}
	spec.AutoStart = true
	if err := validateRoomSpec(spec); err == nil {
		t.Fatal("an automatic AI room accepted missing decks")
	}
	h := NewHub()
	debits := 0
	if err := h.AttachStartAuthorizer(func(start BattleStart) error {
		if !start.CheckOnly {
			debits++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{hub: h, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	output := &hubCheckingConn{Conn: left, hub: h}
	owner := &clientConn{server: s, conn: output, roomID: 1, memberType: 1}
	guest := &clientConn{server: s, conn: &hubCheckingConn{Conn: left, hub: h}, roomID: 1, memberType: 2}
	current := &room{RoomSnapshot: RoomSnapshot{RoomID: 1, OwnerMemberType: 1, State: RoomStateOpen,
		GameStartMemberNum: 2, Members: append([]Member(nil), members[:2]...)},
		connections: map[int]*clientConn{1: owner, 2: guest}, ownerFallbackParty: []Member{members[2]}}
	h.rooms[1] = current
	if err := owner.handleCountdownStart(); err != nil {
		t.Fatal("missing AI deck must return a business rejection without disconnecting", err)
	}
	if output.output.String() != "RoomCountdownStartRequestRes{\n1\n}\n" || current.State != RoomStateOpen || len(current.Members) != 2 || debits != 0 {
		t.Fatalf("rejected start changed the room, charged BP or used the wrong response: %q", output.output.String())
	}
	// A real singer covers the unavailable deck; the owner's thief is enough.
	current.Members = append(current.Members, members[3])
	current.connections[4] = &clientConn{server: s, conn: &hubCheckingConn{Conn: left, hub: h}, roomID: 1, memberType: 4}
	if err := owner.handleCountdownStart(); err != nil {
		t.Fatal(err)
	}
	if current.State != RoomStateCountdown || len(current.Members) != 4 || current.Members[3].ArthurType != 3 || debits != 0 {
		t.Fatal("available owner deck did not fill the only missing role")
	}
	h.mu.Lock()
	delete(h.rooms, 1) // The scheduled callback must see no room after the test.
	h.mu.Unlock()
}

func TestCancelledCountdownAcceptsNewPlayer(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "owner_cancel", true: "debit_rejected"}[reject], func(t *testing.T) {
			_, members := nextBattleFixture(t)
			for i := range members {
				members[i].Level, members[i].LeaderCardID, members[i].LeaderLevel, members[i].LeaderFame = 1, 1, 1, 1
				members[i].PartsIDs, members[i].DeckHonorIDs = make([]int, 7), make([]int, 4)
				members[i].IsRoomLoading = 1
			}
			h := NewHub()
			s := &Server{hub: h, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			if reject {
				if err := h.AttachStartAuthorizer(func(start BattleStart) error {
					if start.CheckOnly {
						return nil
					}
					return errors.New("debit rejected")
				}); err != nil {
					t.Fatal(err)
				}
			}
			left, peer := net.Pipe()
			defer left.Close()
			defer peer.Close()
			output := &hubCheckingConn{Conn: left, hub: h}
			owner := &clientConn{server: s, conn: output, roomID: 1, memberType: 1, userID: 1001}
			guest := &clientConn{server: s, conn: &hubCheckingConn{Conn: left, hub: h}, roomID: 1, memberType: 2, userID: 1002}
			current := &room{RoomSnapshot: RoomSnapshot{RoomID: 1, BossID: 1, OwnerMemberType: 1, State: RoomStateOpen,
				GameStartMemberNum: 2, Members: append([]Member(nil), members[:2]...)},
				connections: map[int]*clientConn{1: owner, 2: guest}, ownerFallbackParty: members[1:],
				reservations: make(map[int]roomReservation), comebackTokens: make(map[int]string), disconnectedUntil: make(map[int]time.Time)}
			h.rooms[1] = current
			if err := owner.handleCountdownStart(); err != nil {
				t.Fatal(err)
			}
			if len(current.Members) != 4 {
				t.Fatal("countdown did not fill the combat party")
			}
			output.output.Reset()
			if reject {
				current.countdownDeadline = time.Time{}
				s.finishCountdown(1)
			} else if err := owner.handleCountdownCancel(); err != nil {
				t.Fatal(err)
			}
			if current.State != RoomStateOpen || len(current.Members) != 2 || len(h.List(RoomSearch{BossID: 1})) != 1 || current.hostCostPaid {
				t.Fatalf("cancelled lobby retains fallback occupants: state=%s members=%d", current.State, len(current.Members))
			}
			frames := output.output.String()
			for _, slot := range []int{3, 4} {
				prefix := "RoomMember{\n" + strconv.Itoa(slot) + ",0,"
				index := strings.Index(frames, prefix)
				if index < 0 || index > strings.Index(frames, "RoomCountdownCancel{") {
					t.Fatal("vacancy not delivered before reopening", frames)
				}
				line := strings.SplitN(frames[index+len("RoomMember{\n"):], "\n", 2)[0]
				if splitCSV(line)[9] != "1" {
					t.Fatal("vacancy tells client to load a human model", line)
				}
			}
			newMember := members[2]
			newMember.UserID, newMember.IsRoomLoading = 9003, 0
			if _, err := h.Reserve(1, newMember.UserID, 3); err != nil {
				t.Fatal("new player cannot reserve reopened room", err)
			}
			if _, err := h.Reserve(1, 9004, 4); err != nil {
				t.Fatal(err)
			}
			credential, err := h.IssueEnter(1, newMember)
			if err != nil {
				t.Fatal(err)
			}
			newOutput := &hubCheckingConn{Conn: left, hub: h}
			newcomer := &clientConn{server: s, conn: newOutput}
			if err := newcomer.handleEnter(joinCSV("9003", "1", "", "3", credential.AuthToken, credential.Signature, "android")); err != nil {
				t.Fatal(err)
			}
			if current.connections[3] != newcomer || len(current.Members) != 3 || allMembersReady(current) {
				t.Fatal("new player was replaced or inherited CPU readiness")
			}
			initialFrames := newOutput.output.String()
			reservationIndex := strings.Index(initialFrames, "RoomMemberReserve{\n4,1\n}")
			if reservationIndex < 0 || reservationIndex < strings.LastIndex(initialFrames, "RoomMember{") {
				t.Error("new player did not receive pending reservation after member snapshots", initialFrames)
			}
			newOutput.output.Reset()
			if err := newcomer.handleLoadingFinish(); err != nil {
				t.Fatal(err)
			}
			if strings.Count(newOutput.output.String(), "RoomMemberReserve{\n4,1\n}") != 1 {
				t.Error("loaded room did not receive reservations for its UI", newOutput.output.String())
			}
			output.output.Reset()
			if err := owner.handleCountdownStart(); err != nil {
				t.Fatal(err)
			}
			if current.State != RoomStateOpen || len(current.Members) != 3 ||
				!strings.Contains(output.output.String(), "RoomCountdownStartRequestRes{\n1\n}") {
				t.Fatal("countdown bypassed another player's pending entry", current.State, output.output.String())
			}
			newOutput.output.Reset()
			if err := h.CancelReservation(1, 9004, 4); err != nil {
				t.Fatal(err)
			}
			if strings.Count(newOutput.output.String(), "RoomMemberReserve{\n4,0\n}") != 1 {
				t.Fatal("reservation cancellation did not reach the newcomer", newOutput.output.String())
			}
			if err := owner.handleCountdownStart(); err != nil {
				t.Fatal(err)
			}
			if len(current.Members) != 4 || current.connections[3] != newcomer || output.locked {
				t.Fatal("second countdown lost player or wrote under Hub lock")
			}
			if err := owner.handleCountdownCancel(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCountdownDisconnectWaitsForStartCommit(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "committed", true: "rejected"}[reject], func(t *testing.T) {
			h := NewHub()
			s := &Server{hub: h, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			ownerSocket, ownerPeer := net.Pipe()
			guestSocket, guestPeer := net.Pipe()
			defer ownerPeer.Close()
			defer guestPeer.Close()
			defer guestSocket.Close()
			guestOutput := &hubCheckingConn{Conn: guestSocket, hub: h}
			owner := &clientConn{server: s, conn: ownerSocket, roomID: 1, memberType: 1, userID: 1000001}
			guest := &clientConn{server: s, conn: guestOutput, roomID: 1, memberType: 2, userID: 1000002}
			current := &room{RoomSnapshot: RoomSnapshot{RoomID: 1, BossID: 11, OwnerMemberType: 1, State: RoomStateCountdown,
				Members: []Member{{MemberType: 1, UserID: 1000001}, {MemberType: 2, UserID: 1000002}}},
				connections: map[int]*clientConn{1: owner, 2: guest}, comebackTokens: map[int]string{1: "owner", 2: "guest"},
				disconnectedUntil: make(map[int]time.Time)}
			entered, commit := make(chan struct{}), make(chan struct{})
			if err := h.AttachStartAuthorizer(func(BattleStart) error {
				close(entered)
				<-commit
				if reject {
					return errors.New("database rejected debit")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			h.rooms[1] = current
			finished, detached := make(chan struct{}), make(chan struct{})
			go func() { s.finishCountdown(1); close(finished) }()
			<-entered
			go func() { _ = owner.close(true); close(detached) }()
			// The socket closes promptly, while account persistence is still held.
			_ = ownerPeer.SetReadDeadline(time.Now().Add(time.Second))
			if _, err := ownerPeer.Read(make([]byte, 1)); err != io.EOF {
				t.Fatalf("socket did not close: %v", err)
			}
			premature := false
			select {
			case <-detached:
				premature = true
			case <-time.After(20 * time.Millisecond):
			}
			close(commit)
			for _, done := range []chan struct{}{finished, detached} {
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("start/close deadlocked")
				}
			}
			if premature {
				t.Fatal("room detached before the pending debit resolved")
			}
			h.mu.Lock()
			defer h.mu.Unlock()
			if reject {
				if h.rooms[1] != nil || current.hostCostPaid || !strings.Contains(guestOutput.output.String(), "RoomDelete{") {
					t.Fatal("unpaid abandoned lobby not dissolved")
				}
			} else if h.rooms[1] != current || current.State != RoomStateBattle || !current.hostCostPaid ||
				current.connections[1] != nil || current.connections[2] != guest || current.comebackTokens[1] != "owner" ||
				!current.disconnectedUntil[1].After(time.Now()) || strings.Contains(guestOutput.output.String(), "RoomDelete{") ||
				!strings.Contains(guestOutput.output.String(), "RoomCountdownFinish{") {
				t.Fatal("paid battle lost its guest, room, or owner recovery credential")
			}
		})
	}
}

func TestComebackDuringInitialLoading(t *testing.T) {
	for _, peerLoading := range []bool{false, true} {
		t.Run(map[bool]string{false: "only_player", true: "peer_loading"}[peerLoading], func(t *testing.T) {
			reference, members := nextBattleFixture(t)
			h := NewHub()
			h.combat = roomFixtureCatalog()
			s := &Server{hub: h, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			left, right := net.Pipe()
			defer right.Close()
			old := &clientConn{server: s, conn: left, roomID: 123, memberType: 1, userID: 1001}
			current := &room{RoomSnapshot: RoomSnapshot{RoomID: 123, BossID: 1, State: RoomStateBattle, Members: members},
				enemyPartyID: 1, costInitial: 3, holdMax: 2, seed: 602, hostCostPaid: true,
				connections: map[int]*clientConn{1: old}, comebackTokens: map[int]string{1: "loading-token"},
				disconnectedUntil: make(map[int]time.Time), battleLoading: make(map[int]bool), gameStartFinished: make(map[int]bool)}
			guestLeft, guestRight := net.Pipe()
			defer guestLeft.Close()
			defer guestRight.Close()
			guestOutput := &hubCheckingConn{Conn: guestLeft, hub: h}
			guest := &clientConn{server: s, conn: guestOutput, roomID: 123, memberType: 2, userID: 1002}
			if peerLoading {
				current.connections[2] = guest
			}
			h.rooms[123] = current
			if err := old.close(true); err != nil {
				t.Fatal(err)
			}
			resumedLeft, resumedRight := net.Pipe()
			defer resumedLeft.Close()
			defer resumedRight.Close()
			output := &hubCheckingConn{Conn: resumedLeft, hub: h}
			resumed := &clientConn{server: s, conn: output}
			if err := resumed.handleComeback("1001,123,loading-token,0"); err != nil {
				t.Fatal(err)
			}
			if !peerLoading {
				if h.rooms[123] != nil || !strings.Contains(output.output.String(), "ComebackResult{\n-1,") {
					t.Fatal("room without humans survived or accepted a stale comeback")
				}
				return
			}
			if !strings.Contains(output.output.String(), "ComebackResult{\n0,") {
				t.Fatalf("legal loading comeback rejected: %s", output.output.String())
			}
			if err := resumed.handleReadyToComeback(""); err != nil {
				t.Fatal(err)
			}
			if peerLoading {
				if current.gameStarted || strings.Contains(output.output.String(), "RoomComeback{") {
					t.Fatal("started before the other player's loading finish")
				}
				if err := guest.handleBattleLoadingFinish(); err != nil {
					t.Fatal(err)
				}
				if current.turnPhaseStarted || !strings.Contains(guestOutput.output.String(), "ApiGameStart{") {
					t.Fatal("ordinary peer lost its start/ack barrier")
				}
			}
			if _, err := reference.Start(); err != nil {
				t.Fatal(err)
			}
			snapshot, err := reference.ResumeResults()
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := encodeBattleResults(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			frames := output.output.String()
			if !current.gameStarted || resumed.comebackPending || strings.Count(frames, "RoomComeback{") != 1 ||
				!strings.Contains(frames, encoded) || strings.Contains(frames, "ApiGameStart{") {
				t.Fatal("loading recovery did not receive exactly the started-state snapshot")
			}
			if peerLoading {
				if err := guest.handleGameStartFinish(); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := reference.TurnPhase(); err != nil {
				t.Fatal(err)
			}
			actual, err := current.engine.ResumeResults()
			if err != nil {
				t.Fatal(err)
			}
			want, err := reference.ResumeResults()
			if err != nil {
				t.Fatal(err)
			}
			actualCSV, err := encodeBattleResults(actual)
			if err != nil {
				t.Fatal(err)
			}
			wantCSV, err := encodeBattleResults(want)
			if err != nil {
				t.Fatal(err)
			}
			if !current.turnPhaseStarted || actualCSV != wantCSV {
				t.Fatal("loading recovery restarted or changed the public battle snapshot")
			}
		})
	}
}

func TestBattleStartAuthorizationAndCancellation(t *testing.T) {
	h := NewHub()
	s := &Server{hub: h, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	calls := 0
	reject := false
	if err := h.AttachStartAuthorizer(func(start BattleStart) error {
		// A callback must be allowed to acquire Hub locks: account HTTP already
		// uses account->Hub order, so calling it under Hub would deadlock.
		if _, ok := h.Snapshot(start.RoomID); !ok {
			t.Fatal("start room missing")
		}
		calls++
		if reject {
			return errors.New("insufficient BP")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	room := &room{RoomSnapshot: RoomSnapshot{RoomID: 1, BossID: 11, OwnerMemberType: 1, State: RoomStateOpen,
		Members: []Member{{MemberType: 1, UserID: 1000001}, {MemberType: 2, UserID: 1000002}}},
		battlePointUse: 15, connections: make(map[int]*clientConn)}
	for slot := 1; slot <= 2; slot++ {
		left, right := net.Pipe()
		t.Cleanup(func() { left.Close(); right.Close() })
		room.connections[slot] = &clientConn{server: s, conn: &hubCheckingConn{Conn: left, hub: h}, roomID: 1, memberType: slot}
	}
	h.rooms[1] = room
	s.finishCountdown(1)
	if calls != 0 {
		t.Fatal("cancelled/open lobby consumed BP")
	}
	room.State = RoomStateCountdown
	room.countdownDeadline = time.Now().Add(time.Minute)
	s.finishCountdown(1)
	if calls != 0 || room.State != RoomStateCountdown {
		t.Fatal("stale timer committed a newer countdown before its deadline")
	}
	room.countdownDeadline = time.Time{}
	reject = true
	s.finishCountdown(1)
	if calls != 1 || room.State != RoomStateOpen || room.hostCostPaid {
		t.Fatal("rejected start did not restore the lobby")
	}
	room.State = RoomStateCountdown
	reject = false
	s.finishCountdown(1)
	s.finishCountdown(1)
	if calls != 2 || room.State != RoomStateBattle || !room.hostCostPaid {
		t.Fatal("accepted start did not commit exactly once")
	}
}

func TestCompletedRoomFreezesReleasedDropsForAllClaimants(t *testing.T) {
	h := NewHub()
	engine, _ := wonRoomBattle(t)
	current := &room{RoomSnapshot: RoomSnapshot{RoomID: 1, BossID: 11, StageType: gamestate.BattleStageScore, State: RoomStateBattle},
		battles:      []gamestate.TeamBattleReplayBattle{{EnemyPartyID: 1, EnemyType: 1}},
		engine:       engine,
		connections:  map[int]*clientConn{1: {userID: 1001}, 2: {userID: 1002}},
		hostCostPaid: true, engineBattleEnd: 1,
		destroyedEnemyBits: 7, waveOutcomes: map[int]battlecore.WaveOutcome{0: {DestroyedEnemyBits: 7}},
	}
	for index := 0; index < 3; index++ {
		current.dropPlan = append(current.dropPlan, gamestate.TeamBattleEnemyDrop{EnemyIndex: index, Reward: gamestate.Reward{Type: 4, Num: index + 1}})
	}
	// This boundary consumes an already recorded combat ledger. Freeze that
	// input independently of core damage/drop generation (covered in core).
	current.releasedDrops = current.dropPlan[:2]
	h.rooms[1] = current
	session := h.lockRoomSession(current.RoomID)
	err := completeBattle(session, current, time.Now())
	session.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	current.dropPlan[0].Reward.Num = 999
	for _, user := range []int{1001, 1002} {
		result, err := h.SettlementFor(1, user)
		if err != nil {
			t.Fatal(err)
		}
		if !result.HostCostPaid || result.DestroyedEnemyBits != 7 || len(result.ReleasedDrops) != 2 || result.ReleasedDrops[0].Reward.Num != 1 || result.StageType != gamestate.BattleStageScore || len(result.EnemyDeadBits) != 1 || result.EnemyDeadBits[0] != 7 {
			t.Fatalf("claimant %d received an unfrozen/incorrect drop ledger: %+v", user, result)
		}
		result.ReleasedDrops[0].Reward.Num = 888
		result.EnemyDeadBits[0] = 0
	}
}
