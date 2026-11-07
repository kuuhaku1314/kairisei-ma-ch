package multiplayer

import (
	"bufio"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/battlecore"
	"kairisei.local/server/internal/gamestate"
)

func TestRoomSynchronizationSeparatesPresentationAndRecoveryWaits(t *testing.T) {
	r, s, c := recoveryFixture(t, true)
	r.userAttackStarted, r.engineBattleEnd = true, 1
	// Presence, not the bool value or stale disconnected keys, is the barrier
	// contract. A received presentation ACK does not imply recovery application.
	r.userAttackFinished = map[int]bool{1: false, 3: true}
	c.recoveryVersion, c.recoveryToken = recoveryProtocolVersion, "diagnostic-test-token"
	before := time.Now()
	rows := s.hub.RoomSynchronizationSnapshot()
	if len(rows) != 1 {
		t.Fatalf("unexpected active rooms: %+v", rows)
	}
	row := rows[0]
	if row.SampledAt.Before(before) || row.SampledAt.After(time.Now()) || row.Phase != "user_attack" ||
		row.ExpectedMessage != "UserAttackFinish" || row.EngineBattleEnd != 1 || row.BarrierReady == nil || *row.BarrierReady ||
		!reflect.DeepEqual(row.MissingMembers, []int{2}) || len(row.Connections) != 2 {
		t.Fatalf("incorrect presentation barrier: %+v", row)
	}
	if row.Connections[0].MemberType != 1 || !row.Connections[0].RecoveryWaiting ||
		row.Connections[0].MessageReceived == nil || !*row.Connections[0].MessageReceived ||
		row.Connections[1].RecoveryWaiting || row.Connections[1].MessageReceived == nil || *row.Connections[1].MessageReceived {
		t.Fatalf("recovery and presentation waits were conflated: %+v", row.Connections)
	}
	row.MissingMembers[0] = 99
	*row.Connections[0].MessageReceived = false
	row.Connections[0].RecoveryApplied = true
	delete(r.connections, 2)
	row = s.hub.RoomSynchronizationSnapshot()[0]
	if len(row.MissingMembers) != 0 || *row.BarrierReady || !*row.Connections[0].MessageReceived || !row.Connections[0].RecoveryWaiting {
		t.Fatalf("snapshot aliases live state or derives waits from stale ACKs: %+v", row)
	}
	c.recoveryApplied = true
	row = s.hub.RoomSynchronizationSnapshot()[0]
	if !*row.BarrierReady || row.Connections[0].RecoveryWaiting || s.hub.completed[123] != nil || s.hub.rooms[123] != r ||
		c.conn.(*sessionTestConn).output.Len() != 0 {
		t.Fatal("diagnostic read changed battle progress or emitted frames")
	}
	r.nextBattlePending = true
	row = s.hub.RoomSynchronizationSnapshot()[0]
	if row.ExpectedMessage != "GameNextFinish" || row.Phase != "next_wave" || *row.BarrierReady || !reflect.DeepEqual(row.MissingMembers, []int{1}) {
		t.Fatalf("next-wave barrier was reported as the previous presentation: %+v", row)
	}
	delete(s.hub.rooms, 123)
	if len(s.hub.RoomSynchronizationSnapshot()) != 0 {
		t.Fatal("removed room was reported from an old read model")
	}
}

func recoveryFixture(t *testing.T, attached bool) (*room, *Server, *clientConn) {
	t.Helper()
	engine, members := nextBattleFixture(t)
	openFixtureUserPhase(t, engine)
	h := NewHub()
	s := &Server{hub: h, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	old := &clientConn{server: s, conn: &sessionTestConn{}, roomID: 123, memberType: 1, userID: 1001}
	peer := &clientConn{server: s, conn: &sessionTestConn{}, roomID: 123, memberType: 2, userID: 1002}
	r := &room{RoomSnapshot: RoomSnapshot{RoomID: 123, State: RoomStateBattle, Members: members},
		engine: engine, gameStarted: true, turnPhaseStarted: true, userPhaseStarted: true,
		connections: map[int]*clientConn{2: peer}, comebackTokens: map[int]string{1: "original-token"},
		disconnectedUntil: map[int]time.Time{}, gameNextFinished: map[int]bool{}}
	if attached {
		r.connections[1] = old
	} else {
		r.disconnectedUntil[1] = time.Now().Add(time.Minute)
	}
	h.rooms[123] = r
	return r, s, old
}

func recoveryConnect(t *testing.T, s *Server, token string) (*clientConn, *sessionTestConn) {
	t.Helper()
	output := &sessionTestConn{}
	c := &clientConn{server: s, conn: output}
	if err := c.handleComeback("1001,123," + token + ",0,2"); err != nil {
		t.Fatal(err)
	}
	return c, output
}

func TestRecoveryTakesOverOnlyAuthenticatedMember(t *testing.T) {
	r, s, old := recoveryFixture(t, true)
	_, wrong := recoveryConnect(t, s, "wrong-token")
	if !strings.Contains(wrong.output.String(), "ComebackResult{\n-1,") || r.connections[1] != old {
		t.Fatal("wrong token replaced the active member")
	}
	c, accepted := recoveryConnect(t, s, "original-token")
	if !strings.Contains(accepted.output.String(), "ComebackResult{\n0,") || r.connections[1] != c || !old.closed {
		t.Fatal("fast reconnect did not replace the stale socket")
	}
	if err := old.close(true); err != nil || r.connections[1] != c {
		t.Fatal("late old close detached the replacement")
	}
	if err := old.handleRetire(""); err == nil || r.connections[1] != c || r.comebackTokens[1] == "" {
		t.Fatal("stale old request retired the replacement")
	}
}

func TestRecoverySnapshotRestoresInputAndRequiresApplication(t *testing.T) {
	r, s, _ := recoveryFixture(t, false)
	c, output := recoveryConnect(t, s, "original-token")
	before, err := r.engine.ResumeResults()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handleReadyToComeback(""); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(strings.NewReader(output.output.String()))
	var state string
	var methods []string
	for {
		method, payload, err := readFrame(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		methods = append(methods, method)
		if method == "RoomRecoveryState" {
			state = payload
		}
	}
	if len(methods) != 3 || methods[0] != "ComebackResult" || methods[1] != "RoomComeback" || methods[2] != "RoomRecoveryState" || state != c.recoveryToken+",1,1,0" {
		t.Fatalf("unexpected recovery context/order: %v %q", methods, state)
	}
	if r.comebackTokens[1] != "original-token" || !c.waitingForRecoveryApplication() || roomBarrierReady(r.connections, map[int]bool{1: true, 2: true}) {
		t.Fatal("snapshot Write was treated as application acknowledgement")
	}
	if err := c.handleComebackApplied(c.recoveryToken); err != nil {
		t.Fatal(err)
	}
	if r.comebackTokens[1] != c.recoveryToken || r.pendingComebackTokens[1] != "" || !c.recoveryApplied || !roomBarrierReady(r.connections, map[int]bool{1: true, 2: true}) {
		t.Fatal("application did not commit the credential and release the barrier")
	}
	after, err := r.engine.ResumeResults()
	if err != nil {
		t.Fatal(err)
	}
	// Application may legitimately submit the already offline NPC members;
	// it must not draw again or submit the recovered living member.
	if r.engine.HasSubmission(1) || !reflect.DeepEqual(recoveryOwnHand(before), recoveryOwnHand(after)) || len(recoveryOwnHand(before)) != 11 {
		t.Fatal("recovery applied gameplay for the recovered member")
	}
	if err := c.handleComebackApplied(c.recoveryToken); err != nil {
		t.Fatal("duplicate apply acknowledgement was not idempotent", err)
	}
}

func recoveryOwnHand(results []BattleResult) []int64 {
	for _, result := range results {
		if result.Command == battlecore.CommandResumeCardHand && result.Args[0] == 1 {
			return result.Args
		}
	}
	return nil
}

func TestRecoveryBetweenWavesAcceptsEitherAcknowledgementOrder(t *testing.T) {
	for _, applyFirst := range []bool{false, true} {
		r, s, _ := recoveryFixture(t, false)
		r.engine, r.Members = wonRoomBattle(t)
		r.engineBattleEnd = 1
		r.battleIndex, r.progress, r.nextBattleIndex, r.nextBattlePending = 0, 0, 1, true
		r.battles = []gamestate.TeamBattleReplayBattle{{EnemyPartyID: 1}, {EnemyPartyID: 1}}
		r.gameNextFinished[2] = true
		c, output := recoveryConnect(t, s, "original-token")
		if err := c.handleReadyToComeback(""); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.output.String(), "GameNextStart{\n1,1") || !strings.Contains(output.output.String(), c.recoveryToken+",0,0,") {
			t.Fatal("wave recovery context missing")
		}
		first := func() error { return c.handleGameNextFinish("") }
		second := func() error { return c.handleComebackApplied(c.recoveryToken) }
		if applyFirst {
			first, second = second, first
		}
		if err := first(); err != nil {
			t.Fatal(err)
		}
		if r.battleIndex != 0 || !r.nextBattlePending {
			t.Fatal("wave advanced before both recovery and scene were ready")
		}
		if err := second(); err != nil {
			t.Fatal(err)
		}
		if r.battleIndex != 1 || r.nextBattlePending || !strings.Contains(output.output.String(), "ApiGameStart{") {
			t.Fatal("pending wave did not advance after both acknowledgements")
		}
	}
}

func TestRecoveryCompletionDuringHandshakeAndTerminalRetry(t *testing.T) {
	r, s, _ := recoveryFixture(t, false)
	r.engine, r.Members = wonRoomBattle(t)
	r.engineBattleEnd = 1
	c, _ := recoveryConnect(t, s, "original-token")
	// Simulate the completion boundary after ComebackResult but before Ready.
	session := s.hub.lockRoomSession(123)
	completed, err := freezeRoomCompletion(r, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	completed.session = session.owner
	s.hub.mu.Lock()
	delete(s.hub.rooms, 123)
	s.hub.completed[123] = completed
	s.hub.mu.Unlock()
	session.room = nil
	session.Unlock()
	if err = c.handleReadyToComeback(""); err != nil {
		t.Fatal(err)
	}
	if c.recoveryToken == "" || completed.comebackTokens[1] != "original-token" || completed.comebackConnections[1] != c {
		t.Fatal("completion discarded the in-flight recovery")
	}
	next, output := recoveryConnect(t, s, "original-token")
	if err = next.handleReadyToComeback(""); err != nil {
		t.Fatal(err)
	}
	frames := output.output.String()
	if !strings.Contains(frames, "RoomRecoveryState{\n"+next.recoveryToken+",0,0,1") || !strings.Contains(frames, "ApiGameEnd{") || !strings.Contains(frames, "GameClose{") {
		t.Fatal("terminal recovery frame order/state missing")
	}
	if err = next.handleComebackApplied(next.recoveryToken); err != nil {
		t.Fatal(err)
	}
	if !next.recoveryApplied || completed.comebackTokens[1] != "" {
		t.Fatal("terminal apply did not finish recovery")
	}
}

func TestRecoveryDiscoveryIsAccountScopedReadOnlyAndBounded(t *testing.T) {
	r, s, old := recoveryFixture(t, true)
	r.BossID = 42
	r.BossGroup = map[string]any{"0": 4, "10": []any{map[string]any{"0": 42}}}
	r.battles = []gamestate.TeamBattleReplayBattle{{EnemyPartyID: 1}, {EnemyPartyID: 1}}
	r.battleIndex, r.progress = 1, 1
	if entry := s.hub.RecoveryFor(1002); entry != nil {
		t.Fatal("another member received a token without comeback rights")
	}
	entry := s.hub.RecoveryFor(1001)
	if entry == nil || entry.BattleIndex != 1 || entry.Progress != 1 || len(entry.Members) != 4 || entry.BossID != 42 {
		t.Fatal("own live recovery entry missing")
	}
	if r.connections[1] != old || r.comebackTokens[1] != "original-token" || len(r.pendingComebackTokens) != 0 {
		t.Fatal("discovery mutated the live connection or credentials")
	}
	delete(r.connections, 1)
	r.disconnectedUntil[1] = time.Now().Add(time.Minute)
	if s.hub.RecoveryFor(1001) == nil {
		t.Fatal("valid offline reservation hidden")
	}
	r.disconnectedUntil[1] = time.Now().Add(-time.Second)
	if s.hub.RecoveryFor(1001) != nil {
		t.Fatal("expired room offered")
	}
	r.disconnectedUntil[1] = time.Now().Add(time.Minute)
	delete(r.comebackTokens, 1)
	r.pendingComebackTokens = map[int]string{1: "stale-pending"}
	if s.hub.RecoveryFor(1001) != nil {
		t.Fatal("retired member regained recovery from pending token")
	}
}

func TestRecoveryLostSnapshotCanRetryBothCredentialGenerations(t *testing.T) {
	for _, received := range []bool{false, true} {
		r, s, _ := recoveryFixture(t, false)
		c, _ := recoveryConnect(t, s, "original-token")
		if err := c.handleReadyToComeback(""); err != nil {
			t.Fatal(err)
		}
		pending := c.recoveryToken
		token := "original-token"
		if received {
			token = pending
		}
		next, output := recoveryConnect(t, s, token)
		if !strings.Contains(output.output.String(), "ComebackResult{\n0,") {
			t.Fatal("lost snapshot permanently invalidated a legitimate retry")
		}
		if err := next.handleReadyToComeback(""); err != nil {
			t.Fatal(err)
		}
		if next.recoveryToken != pending {
			t.Fatal("unacknowledged retry created another credential generation")
		}
		if err := next.handleComebackApplied(pending); err != nil {
			t.Fatal(err)
		}
		if r.connections[1] != next || r.comebackTokens[1] != pending {
			t.Fatal("retry did not become the current owner")
		}
		_, rejected := recoveryConnect(t, s, "original-token")
		if !strings.Contains(rejected.output.String(), "ComebackResult{\n-1,") || r.connections[1] != next {
			t.Fatal("acknowledged previous credential remained valid")
		}
	}
}

func TestRecoveryDoesNotReopenSubmittedInput(t *testing.T) {
	r, _, _ := recoveryFixture(t, false)
	r.cardPlaySubmissions = map[int]cardPlaySubmission{1: {}}
	if state := recoveryStateFrame(r, 1, "token"); state.payload != "token,1,0,1" {
		t.Fatal("already submitted player regained input", state)
	}
	r.userAttackStarted = true
	if state := recoveryStateFrame(r, 1, "token"); state.payload != "token,0,0,1" {
		t.Fatal("attack phase reopened input", state)
	}
}

func TestRecoveryDeadlineDoesNotSlideWithPingAndClearsAfterApply(t *testing.T) {
	_, s, _ := recoveryFixture(t, false)
	c, _ := recoveryConnect(t, s, "original-token")
	deadline := c.readDeadline()
	if deadline.IsZero() || deadline.After(time.Now().Add(comebackLifetime)) {
		t.Fatal("unbounded recovery handshake")
	}
	if err := c.handleReadyToComeback(""); err != nil {
		t.Fatal(err)
	}
	if !c.readDeadline().Equal(deadline) {
		t.Fatal("snapshot/ping extended recovery hold")
	}
	if err := c.handleComebackApplied(c.recoveryToken); err != nil {
		t.Fatal(err)
	}
	if !c.recoveryUntil.IsZero() {
		t.Fatal("applied live connection retained recovery timeout")
	}
}
