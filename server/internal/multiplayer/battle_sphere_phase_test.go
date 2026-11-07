package multiplayer

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestEnemyAnimationChaliceReservationBroadcast(t *testing.T) {
	hub := NewHub()
	s := &Server{hub: hub, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	ownerOut, guestOut := &hubCheckingConn{Conn: left, hub: hub}, &hubCheckingConn{Conn: left, hub: hub}
	owner := &clientConn{server: s, conn: ownerOut, roomID: 1, memberType: 1}
	guest := &clientConn{server: s, conn: guestOut, roomID: 1, memberType: 2}
	engine, _ := coreBattleFixture(t)
	current := &room{RoomSnapshot: RoomSnapshot{RoomID: 1, State: RoomStateBattle}, engine: engine,
		enemyPhaseStarted: true, connections: map[int]*clientConn{1: owner, 2: guest}}
	hub.rooms[1] = current
	current.chaliceInput = roomChaliceInput{phase: PhaseEnemy}
	current.chaliceInput.playable[1][0] = true
	before := engine.StateView()
	for _, slot := range []string{"1", "0", "1"} {
		if err := guest.handle("ChaliceSphrReserve", slot); err != nil {
			t.Fatal("legal enemy-animation click must not reach connection rejection", err)
		}
	}
	for _, out := range []*hubCheckingConn{ownerOut, guestOut} {
		frames := out.output.String()
		if out.locked || strings.Count(frames, "ApiChaliceSphrReserve{") != 3 ||
			!strings.Contains(frames, "318,2,0") || strings.Contains(frames, "Disconnect") {
			t.Fatalf("reservation/cancellation did not reach both players outside the lock: %s", frames)
		}
	}
	if !reflect.DeepEqual(engine.StateView(), before) || current.chaliceInput.reserved[1] != 1 {
		t.Fatal("room intention mutated engine")
	}
	if rows := current.chaliceResumeResults(nil); len(rows) != 4 || !reflect.DeepEqual(rows[1].Args, []int64{2, 1}) {
		t.Fatal("comeback lost the broadcast reservation")
	}
	current.enemyPhaseFinished = map[int]bool{2: true}
	if err := guest.handle("ChaliceSphrReserve", "0"); err != nil || current.chaliceInput.reserved[1] != 1 {
		t.Fatal("input after the member's native Finish changed its closed reservation", err)
	}

}

func TestChaliceEnemyVictoryAdvancesWaveAfterAnimation(t *testing.T) {
	engine, members := wonRoomBattle(t)
	hub := NewHub()
	s := &Server{hub: hub, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	out := &hubCheckingConn{Conn: left, hub: hub}
	client := &clientConn{server: s, conn: out, roomID: 1, memberType: 1}
	current := &room{RoomSnapshot: RoomSnapshot{RoomID: 1, State: RoomStateBattle, Members: members},
		engine: engine, engineBattleEnd: 1, chaliceEnemyStarted: true,
		connections: map[int]*clientConn{1: client}, chaliceEnemyFinished: map[int]bool{},
		battles: []gamestate.TeamBattleReplayBattle{{EnemyPartyID: 1}, {EnemyPartyID: 1}}}
	hub.rooms[1] = current
	if err := s.tryAdvanceChaliceEnemy(1); err != nil || current.nextBattlePending {
		t.Fatal("victory advanced before the animation finished", err)
	}
	current.chaliceEnemyFinished[1] = true
	if err := s.tryAdvanceChaliceEnemy(1); err != nil {
		t.Fatal(err)
	}
	if !current.nextBattlePending || !strings.Contains(out.output.String(), "GameNextStart{") || strings.Contains(out.output.String(), "ApiTurnPhase{") || out.locked {
		t.Fatal("summon victory attempted another turn instead of the existing wave-completion flow", out.output.String())
	}
}

func TestTerminalAnimationChaliceClickDoesNotDisconnect(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		endType     int
		nextPending bool
		battleIndex int
	}{
		{"ongoing-animation", 0, false, 0},
		{"win-animation", 1, false, 0},
		{"loss-animation", 2, false, 0},
		{"awake-animation", 4, false, 0},
		{"win-next-barrier", 1, true, 0},
		{"awake-next-barrier", 4, true, 0},
		{"awake-next-start", 0, false, 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			hub := NewHub()
			s := &Server{hub: hub, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			left, right := net.Pipe()
			defer left.Close()
			defer right.Close()
			ownerOut, guestOut := &hubCheckingConn{Conn: left, hub: hub}, &hubCheckingConn{Conn: left, hub: hub}
			owner := &clientConn{server: s, conn: ownerOut, roomID: 1, memberType: 1}
			guest := &clientConn{server: s, conn: guestOut, roomID: 1, memberType: 2}
			engine, _ := coreBattleFixture(t)
			current := &room{RoomSnapshot: RoomSnapshot{RoomID: 1, State: RoomStateBattle}, engine: engine,
				engineBattleEnd: scenario.endType, nextBattlePending: scenario.nextPending, battleIndex: scenario.battleIndex,
				userAttackStarted: true, connections: map[int]*clientConn{1: owner, 2: guest}}
			hub.rooms[1] = current
			animating := !scenario.nextPending && scenario.battleIndex == 0
			if animating {
				current.openUserChaliceInput([]BattleResult{{Command: CommandChaliceSpherePlayable, Args: []int64{1, 1}}})
			}
			before := engine.StateView()
			for _, slot := range []string{"1", "0", "1"} {
				if err := owner.handle("ChaliceSphrReserve", slot); err != nil {
					t.Fatal("click while terminal results are still animating rejected", err)
				}
			}
			for _, out := range []*hubCheckingConn{ownerOut, guestOut} {
				wantReserved := 0
				if animating {
					wantReserved = 2
				}
				if out.locked || strings.Count(out.output.String(), "318,1,1") != wantReserved || strings.Count(out.output.String(), "318,1,0") != 3-wantReserved || strings.Contains(out.output.String(), "Disconnect") {
					t.Fatal("published reservation feedback depended on an unanimated battle outcome", out.output.String())
				}
			}
			if err := owner.handle("Chat", "17"); err != nil || !strings.Contains(guestOut.output.String(), "MemberChat{\n1,17") {
				t.Fatal("battle outcome suppressed chat while the native connection was active", err)
			}
			if !reflect.DeepEqual(engine.StateView(), before) || current.nextBattlePending != scenario.nextPending {
				t.Fatal("late reservation mutated engine or barrier")
			}
			owner.comebackPending = true
			if err := owner.handle("ChaliceSphrReserve", "1"); err == nil {
				t.Fatal("terminal handling bypassed member authentication")
			}
		})
	}
}

func TestCompletedBattleInteraction(t *testing.T) {
	for _, scoreDeath := range []bool{false, true} {
		t.Run(fmt.Sprint(scoreDeath), func(t *testing.T) {
			var engine *BattleEngine
			var members []Member
			if scoreDeath {
				engine, members = knockedOutRoomBattle(t, 1, false, true)
			} else {
				engine, members = wonRoomBattle(t)
			}
			hub := NewHub()
			repository := &terminalCompletionRepository{}
			hub.repository = repository
			hub.completionCommit = repository.SaveCompleted
			s := &Server{hub: hub, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			left, right := net.Pipe()
			t.Cleanup(func() { left.Close(); right.Close() })
			ownerOut, guestOut := &hubCheckingConn{Conn: left, hub: hub}, &hubCheckingConn{Conn: left, hub: hub}
			owner := &clientConn{server: s, conn: ownerOut, roomID: 1, memberType: 1, userID: 1001}
			guest := &clientConn{server: s, conn: guestOut, roomID: 1, memberType: 2, userID: 1002}
			current := &room{RoomSnapshot: RoomSnapshot{RoomID: 1, State: RoomStateBattle, OwnerMemberType: 1, Members: members},
				engine: engine, engineBattleEnd: engine.EndType(), connections: map[int]*clientConn{1: owner, 2: guest}}
			if scoreDeath {
				current.scorePolicy = &gamestate.TeamBattleScorePolicy{MemberDeadEnd: true}
			}
			hub.rooms[1] = current
			session := hub.lockRoomSession(current.RoomID)
			if err := s.completeGoBattleLocked(session, current); err != nil {
				t.Fatal(err)
			}
			before, err := hub.SettlementFor(1, 1001)
			if err != nil {
				t.Fatal(err)
			}
			state := engine.StateView()
			for _, request := range []battleFrame{{"Chat", "17"}, {"ChatNew", "18"}, {"ChaliceSphrReserve", "1"}, {"ChaliceSphrReserve", "0"}, {"ChaliceSphrSkip", ""}} {
				if err := owner.handle(request.method, request.payload); err != nil {
					t.Fatal("legitimate final-direction interaction rejected", request.method, err)
				}
			}
			frames := guestOut.output.String()
			if !strings.Contains(frames, "MemberChat{\n1,17") || !strings.Contains(frames, "MemberChatNew{\n1,18") ||
				strings.Index(frames, "GameClose{") > strings.Index(frames, "MemberChat{") ||
				strings.Contains(ownerOut.output.String(), "MemberChat{") || strings.Contains(frames, "ChaliceSphrSkipExec{") ||
				strings.Contains(frames, "ApiChaliceSphrReserve{") || guestOut.locked || ownerOut.locked {
				t.Fatal("final-direction chat did not reach the peer in order", frames)
			}
			if err := guest.handle("ChaliceSphrSkip", ""); err == nil {
				t.Fatal("guest skipped the owner's movie")
			}
			impostor := &clientConn{server: s, conn: guestOut, roomID: 1, memberType: 1, userID: 1001}
			if err := impostor.handle("Chat", "17"); err == nil {
				t.Fatal("stale socket impersonated a completed member")
			}
			if err := owner.handle("ChaliceSphrReserve", "100"); err == nil {
				t.Fatal("completion accepted malformed input")
			}
			if err := guest.handle("Chat", "19"); err != nil {
				t.Fatal("one player's settlement closed the other player's direction", err)
			}
			after, err := hub.SettlementFor(1, 1002)
			if err != nil || !reflect.DeepEqual(before, after) || repository.saves != 1 || !reflect.DeepEqual(engine.StateView(), state) || hub.rooms[1] != nil {
				t.Fatal("interaction reopened combat or modified immutable settlement", err)
			}
			deadline := owner.finishingDeadline()
			if !owner.readDeadline().Equal(deadline) {
				t.Fatal("ping could extend the fixed finishing lifetime")
			}
			owner.setFinishingDeadline(time.Now().Add(-time.Second))
			if err := owner.handle("Chat", "20"); err == nil {
				t.Fatal("expired interaction identity was accepted")
			}
			if err := guest.close(true); err != nil || hub.completed[1].comebackConnections[2] != nil {
				t.Fatal("final direction close retained a live identity", err)
			}

		})
	}
}
