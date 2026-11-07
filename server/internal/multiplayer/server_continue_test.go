package multiplayer

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func continueRoomFixture(t *testing.T, authorize func(BattleContinue) (ContinueBalance, error), dead int) (*Server, *room) {
	t.Helper()
	h := NewHub()
	if err := h.AttachContinueAuthorizer(authorize); err != nil {
		t.Fatal(err)
	}
	s := &Server{hub: h, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	engine, members := knockedOutRoomBattle(t, dead, true, false)
	current := &room{RoomSnapshot: RoomSnapshot{RoomID: 1, BossID: 11, State: RoomStateBattle, OwnerMemberType: 1, Members: members},
		continueAllowed: true, engine: engine, connections: make(map[int]*clientConn), comebackTokens: map[int]string{1: "token1", 2: "token2"},
		disconnectedUntil: make(map[int]time.Time), gameStarted: true, turnPhaseStarted: true, userPhaseStarted: true,
		userAttackStarted: true, chaliceUserStarted: true, enemyPhaseStarted: true, enemyPhaseFinished: map[int]bool{1: true, 2: true},
		chaliceEnemyFinished: make(map[int]bool)}
	for slot := 1; slot <= 2; slot++ {
		current.connections[slot] = continueTestPeer(t, s, slot)
	}
	h.rooms[1] = current
	if err := s.tryAdvanceEnemyPhase(1); err != nil {
		t.Fatal(err)
	}
	if !engine.ContinuePending() || current.continuation == nil || engine.EndType() != 0 || current.chaliceEnemyStarted {
		t.Fatal("KO did not pause the room before the next phase")
	}
	t.Cleanup(func() { current.continuationTimerStopForTest() })
	return s, current
}

func (current *room) continuationTimerStopForTest() {
	if current.continuation != nil {
		current.continuation.timer.Stop()
	}
}

func continueTestPeer(t *testing.T, s *Server, slot int) *clientConn {
	t.Helper()
	left, right := net.Pipe()
	t.Cleanup(func() { left.Close(); right.Close() })
	return &clientConn{server: s, conn: &hubCheckingConn{Conn: left, hub: s.hub}, roomID: 1, memberType: slot, userID: 1000 + slot}
}

func TestContinuePaymentQuorumRetryAndReconnect(t *testing.T) {
	var calls atomic.Int32
	var reject atomic.Bool
	reject.Store(true)
	entered, releasePayment := make(chan struct{}), make(chan struct{})
	var s *Server
	s, current := continueRoomFixture(t, func(request BattleContinue) (ContinueBalance, error) {
		if _, ok := s.hub.Snapshot(request.RoomID); !ok {
			return ContinueBalance{}, errors.New("room vanished")
		}
		calls.Add(1)
		if reject.Load() {
			return ContinueBalance{}, errors.New("disk unavailable")
		}
		close(entered)
		<-releasePayment
		return ContinueBalance{Coin: 5, CoinFree: 7}, nil
	}, 1)
	owner, guest := current.connections[1], current.connections[2]
	before := current.engine.StateView()
	turn := current.engine.TurnNumber()
	if err := owner.handleContinue(""); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(current.engine.StateView(), before) || !current.engine.ContinuePending() {
		t.Fatal("rejected debit changed the battle")
	}
	reject.Store(false)
	done := make(chan error, 1)
	go func() { done <- owner.handleContinue("") }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("debit called under Hub lock")
	}
	if err := guest.handleContinue(""); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || current.engine.StateView().Members[0].HP != 0 {
		t.Fatal("concurrent payer charged or revived before commit")
	}
	close(releasePayment)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if current.engine.ContinuePending() || current.chaliceEnemyStarted {
		t.Fatal("revival advanced the phase before animation ACK")
	}
	for _, p := range current.engine.StateView().Members[:4] {
		if p.HP != p.MaxHP {
			t.Fatal("party not revived")
		}
	}
	if strings.Contains(guest.conn.(*hubCheckingConn).output.String(), "ContinueResult{") {
		t.Fatal("payer currency leaked to a teammate")
	}
	if err := owner.close(true); err != nil {
		t.Fatal(err)
	}
	recovered := continueTestPeer(t, s, 1)
	recovered.roomID, recovered.memberType, recovered.userID = 0, 0, 0
	if err := recovered.handleComeback("1001,1,token1,0,2"); err != nil {
		t.Fatal(err)
	}
	if err := recovered.handleReadyToComeback(""); err != nil {
		t.Fatal(err)
	}
	if current.chaliceEnemyStarted || current.engine.ContinuePending() {
		t.Fatal("reconnect bypassed continuation ACK or replayed gameplay")
	}
	if err := recovered.handleGameOverPhaseFinish(""); err != nil {
		t.Fatal(err)
	}
	if current.chaliceEnemyStarted {
		t.Fatal("one animation ACK advanced two-player room")
	}
	if err := guest.handleGameOverPhaseFinish(""); err != nil {
		t.Fatal(err)
	}
	if current.continuation == nil || current.chaliceEnemyStarted {
		t.Fatal("continuation bypassed the recovered client's application confirmation")
	}
	if err := recovered.handleComebackApplied(recovered.recoveryToken); err != nil {
		t.Fatal(err)
	}
	if current.continuation != nil || !current.chaliceEnemyStarted || current.engine.TurnNumber() != turn {
		t.Fatal("continuation did not resume the same turn after the quorum")
	}

	output := guest.conn.(*hubCheckingConn).output.String()
	if !(strings.Index(output, "ApiContinue{") < strings.Index(output, "RoomComeback{") && strings.Index(output, "RoomComeback{") < strings.Index(output, "ApiChaliceSphrExecEnemyPhase{")) {
		t.Fatal("revival, reset snapshot and next phase are out of order")
	}
	for _, peer := range []*clientConn{recovered, guest} {
		if err := peer.handleChaliceSphrExecEnemyPhaseFinish(); err != nil {
			t.Fatal(err)
		}
	}
	for _, peer := range []*clientConn{recovered, guest} {
		if err := peer.handleTurnPhaseFinish(); err != nil {
			t.Fatal(err)
		}
	}
	if _, submitted := current.cardPlaySubmissions[1]; submitted {
		t.Fatal("revived connected member was automatically skipped")
	}
	for _, peer := range []*clientConn{recovered, guest} {
		if strings.Contains(peer.conn.(*hubCheckingConn).output.String(), "28,1\n") {
			t.Fatal("revived member received a stale KO pass confirmation")
		}
	}
	if err := guest.handleCardPlay(strings.Repeat("0,", 11)+"0", false); err != nil {
		t.Fatal(err)
	}
	if current.userAttackStarted {
		t.Fatal("room did not wait for the revived member's cards")
	}
	card := firstFixtureHandCard(t, current.engine, 1)
	if card == 0 {
		t.Fatal("revived member has no selectable card")
	}
	if err := recovered.handleCardPlay(fmt.Sprintf("%d,5,0,0,0,0,0,0,0,0,0,0", card), false); err != nil {
		t.Fatal(err)
	}
	if !current.userAttackStarted {
		t.Fatal("revived member's submission did not release the input barrier")
	}
}

func TestContinueDeclineAndTimeout(t *testing.T) {
	for _, tc := range []struct {
		name   string
		wipe   bool
		onlyAI bool
	}{
		{name: "survivors"},
		{name: "wipe", wipe: true},
		{name: "only_ai_survivor", onlyAI: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dead := 1
			if tc.wipe {
				dead = 4
			} else if tc.onlyAI {
				dead = 3
			}
			s, current := continueRoomFixture(t, func(BattleContinue) (ContinueBalance, error) {
				t.Fatal("decline charged currency")
				return ContinueBalance{}, nil
			}, dead)
			owner, guest := current.connections[1], current.connections[2]
			peers := []*clientConn{owner, guest}
			if err := owner.handleContinuePhaseFinish(""); err != nil {
				t.Fatal(err)
			}
			if !current.engine.ContinuePending() {
				t.Fatal("early decline retired party before others could revive it")
			}
			current.continuation.deadline = time.Time{}
			if err := s.advanceContinue(1); err != nil {
				t.Fatal(err)
			}
			if current.engine.MemberAlive(1) || current.engine.ContinuePending() {
				t.Fatal("cancel did not retire KO members")
			}
			for _, peer := range peers {
				if err := peer.handleGameOverPhaseFinish(""); err != nil {
					t.Fatal(err)
				}
			}
			if tc.wipe {
				if current.engine.EndType() != 2 || !strings.Contains(guest.conn.(*hubCheckingConn).output.String(), "ApiGameEnd{") {
					t.Fatal("wipe cancellation did not end cleanly")
				}
				return
			}
			if !current.chaliceEnemyStarted {
				t.Fatal("survivors did not resume")
			}

			for _, peer := range peers {
				peer.conn.(*hubCheckingConn).output.Reset()
				if err := peer.handleChaliceSphrExecEnemyPhaseFinish(); err != nil {
					t.Fatal(err)
				}
			}
			// Removal waits for the next turn's presentation and all live ACKs.
			if current.connections[1] != owner {
				t.Fatal("KO participant left before the turn boundary")
			}
			for _, peer := range peers {
				if err := peer.handleTurnPhaseFinish(); err != nil {
					t.Fatal(err)
				}
			}
			if current.connections[1] != nil || !owner.retired || current.comebackTokens[1] != "" || !current.disconnectedUntil[1].IsZero() || current.engine.StateView().Members[0].HP != 0 {
				t.Fatal("KO participant retained identity or changed HP")
			}
			frames := owner.conn.(*hubCheckingConn).output.String()
			if strings.Contains(frames, "ApiUserPhase{") || strings.Count(frames, "ApiGameEnd{\n2,2\n}") != 1 || strings.Count(frames, "GameClose{") != 1 {
				t.Fatal("KO participant did not receive the native defeat exit", frames)
			}
			if tc.onlyAI {
				if _, ok := s.hub.Snapshot(1); ok || !guest.retired {
					t.Fatal("AI kept a room after the last human died")
				}
				if _, err := s.hub.SettlementFor(1, owner.userID); err == nil {
					t.Fatal("dead owner obtained a result")
				}
				return
			}
			if current.connections[2] != guest || !current.userPhaseStarted || current.userAttackStarted {
				t.Fatal("living human lost input")
			}
			if err := owner.handleCardPlay(strings.Repeat("0,", 11)+"0", false); err == nil {
				t.Fatal("removed human still submits")
			}
			for turn := 0; turn < 2; turn++ {
				beforeHP := current.engine.StateView().Members[4].HP
				if err := guest.handleCardPlay(strings.Repeat("0,", 11)+"0", false); err != nil {
					t.Fatal(err)
				}
				if !current.userAttackStarted || current.engine.StateView().Members[4].HP >= beforeHP {
					t.Fatal("CPU choices waited for removed human")
				}
				frames = guest.conn.(*hubCheckingConn).output.String()
				if strings.Contains(frames, "GameClose{") || strings.Contains(frames, fmt.Sprintf("28,%d\n", owner.memberType)) {
					t.Fatal("living peer got defeat or synthetic KO skip")
				}
				if turn == 0 {
					for _, finish := range []func(*clientConn) error{(*clientConn).handleUserAttackFinish, (*clientConn).handleChaliceSphrExecUserPhaseFinish, (*clientConn).handleEnemyPhaseFinish, (*clientConn).handleChaliceSphrExecEnemyPhaseFinish, (*clientConn).handleTurnPhaseFinish} {
						if err := finish(guest); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			if current.engine.StateView().Members[0].HP != 0 {
				t.Fatal("dead actor was revived by CPU takeover")
			}
			current.engineBattleEnd = 1
			session := s.hub.lockRoomSession(1)
			err := completeBattle(session, current, time.Now())
			session.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.hub.SettlementFor(1, owner.userID); err == nil {
				t.Fatal("dead owner retained reward eligibility")
			}
			settled, err := s.hub.SettlementFor(1, guest.userID)
			if err != nil || len(settled.OnlineUserIDs) != 1 || settled.OnlineUserIDs[0] != guest.userID {
				t.Fatal("fame eligibility includes removed owner", err)
			}
		})
	}
}
