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

func TestAwakeInheritsUnreleasedBodyDrops(t *testing.T) {
	waves := []gamestate.TeamBattleReplayBattle{{EnemyPartyID: 1, EnemyType: 1}, {EnemyPartyID: 1, EnemyType: 4}, {EnemyPartyID: 1, EnemyType: 4}}
	plan := []gamestate.TeamBattleEnemyDrop{
		{Reward: gamestate.Reward{Type: 4, Num: 900}},
		{Reward: gamestate.Reward{Type: 8, RewardTypeID: 4000, Num: 600}},
		{EnemyIndex: 1, Reward: gamestate.Reward{Type: 13, RewardTypeID: 20000003, Num: 1}},
	}
	current := &room{battles: waves, dropPlan: plan}
	drops := roomSpecWaveDrops(RoomSpec{DropLedgerVersion: 1, DropPlan: plan, Battles: waves}, 1)
	if len(drops) != 2 {
		t.Fatalf("awake drops: %+v", drops)
	}
	next, _ := wonRoomBattle(t, drops...)
	current.engine, current.battleIndex = next, 1
	recordRoomWaveDrops(current)
	recordRoomWaveDrops(current)
	if len(current.releasedDrops) != 2 || current.releasedDrops[0].BattleIndex != 1 || !roomBodyRewardAlreadyReleased(current, 2) {
		t.Fatalf("bad awake settlement ledger: %+v", current.releasedDrops)
	}
	if len(priorWaveResumeDrops(current.releasedDrops, 1)) != 0 || len(priorWaveResumeDrops(current.releasedDrops, 2)) != 2 {
		t.Fatal("resume duplicated or lost current-wave body drops")
	}
}

func TestMultiplayerWavesFinishOnceAndShareAllDrops(t *testing.T) {
	engine, members := configuredRoomBattle(t, func(_ *battlecore.Definitions, c *battlecore.Config, _ []battlecore.Participant) {
		c.Drops = []BattleDrop{{RewardType: 4, Num: 1}}
	})
	hub := NewHub()
	server := &Server{hub: hub, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	current := &room{RoomSnapshot: RoomSnapshot{RoomID: 123, BossID: 1, State: RoomStateBattle, Members: members},
		engine: engine, gameStarted: true, hostCostPaid: true, dropLedgerVersion: 1, connections: map[int]*clientConn{}, gameStartFinished: map[int]bool{},
		turnPhaseFinished: map[int]bool{}, cardPlaySubmissions: map[int]cardPlaySubmission{}, userAttackFinished: map[int]bool{},
	}
	hub.rooms[123] = current
	for i := 0; i < 6; i++ {
		current.battles = append(current.battles, gamestate.TeamBattleReplayBattle{EnemyPartyID: 1, EnemyType: 0})
		current.dropPlan = append(current.dropPlan, gamestate.TeamBattleEnemyDrop{BattleIndex: i, EnemyIndex: 0, Reward: gamestate.Reward{Type: 4, Num: i + 1, CardSkillLevels: []int16{}}})
	}
	if fields := splitCSV(roomCountdownPayload(current)); len(fields) != 37 || fields[0] != "6" {
		t.Fatal("countdown truncated multi-wave enemies")
	}
	outputs := make([]*hubCheckingConn, 2)
	for i := range outputs {
		left, right := net.Pipe()
		t.Cleanup(func() { left.Close(); right.Close() })
		outputs[i] = &hubCheckingConn{Conn: left, hub: hub}
		current.connections[i+1] = &clientConn{conn: outputs[i], server: server, roomID: 123, memberType: i + 1, userID: 1001 + i}
	}

	if _, err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	for wave := 0; wave < 6; wave++ {
		for _, client := range current.connections {
			if err := client.handleGameStartFinish(); err != nil {
				t.Fatal(err)
			}
		}
		for _, client := range current.connections {
			if err := client.handleTurnPhaseFinish(); err != nil {
				t.Fatal(err)
			}
		}
		current.cardPlaySubmissions[1] = cardPlaySubmission{CardTypes: [5]int{firstFixtureHandCard(t, current.engine, 1)}, Targets: [5]int{5}}
		current.cardPlaySubmissions[2], current.cardPlaySubmissions[3], current.cardPlaySubmissions[4] = cardPlaySubmission{}, cardPlaySubmission{}, cardPlaySubmission{}
		if err := server.tryAdvanceCardPlay(123); err != nil {
			t.Fatal(err)
		}
		if current.engineBattleEnd != 1 {
			t.Fatalf("wave %d did not win: %d hp %d", wave, current.engineBattleEnd, current.engine.StateView().Members[4].HP)
		}
		for _, client := range current.connections {
			if err := client.handleUserAttackFinish(); err != nil {
				t.Fatal(err)
			}
		}
		if wave < 5 {
			if _, err := hub.SettlementFor(123, 1001); !errors.Is(err, ErrCompletedBattlePending) || !current.nextBattlePending {
				t.Fatal("intermediate wave settled as a completed quest")
			}
			// A repeated old direction ACK must not emit another transition.
			if err := current.connections[1].handleUserAttackFinish(); err != nil {
				t.Fatal(err)
			}
			if err := current.connections[1].handleGameNextFinish(""); err != nil {
				t.Fatal(err)
			}
			if current.battleIndex != wave {
				t.Fatal("next wave started before the second client was ready")
			}
			if err := current.connections[2].handleGameNextFinish(""); err != nil {
				t.Fatal(err)
			}
			if err := current.connections[2].handleGameNextFinish(""); err != nil {
				t.Fatal(err)
			}
			if current.battleIndex != wave+1 || current.nextBattlePending {
				t.Fatal("next wave transition failed")
			}
		}
	}
	for _, id := range []int{1001, 1002} {
		completed, err := hub.SettlementFor(123, id)
		if err != nil || len(completed.ReleasedDrops) != 6 || completed.BattleIndex != 5 || completed.Progress != 5 || !completed.HostCostPaid || len(completed.EnemyDeadBits) != 6 {
			t.Fatalf("incorrect completed multi-wave ledger: %+v %v", completed, err)
		}
		for i, drop := range completed.ReleasedDrops {
			if completed.EnemyDeadBits[i]&1 == 0 {
				t.Fatal("completed wave lost its actual body defeat")
			}
			if drop.BattleIndex != i || drop.Reward.Num != i+1 || drop.Reward.CardSkillLevels == nil {
				t.Fatal("drop repeated, reordered or lost")
			}
		}
	}
	for _, output := range outputs {
		frames := output.output.String()
		if output.locked || strings.Count(frames, "GameNextStart{") != 5 || strings.Count(frames, "ApiGameEnd{") != 1 || strings.Count(frames, "GameClose{") != 1 {
			t.Fatalf("invalid wave output: %s", frames)
		}
		for i := 1; i < 6; i++ {
			if !strings.Contains(frames, "GameNextStart{\n"+strconv.Itoa(i)+","+strconv.Itoa(i)+"\n}") {
				t.Fatal("incorrect wave index/progress")
			}
		}
	}
}

func TestOptionalAwakeWaveSelection(t *testing.T) {
	current := &room{battles: []gamestate.TeamBattleReplayBattle{{EnemyType: 1}, {EnemyType: 4}, {EnemyType: 0}}, engineBattleEnd: 1}
	if index, ok := roomNextBattle(current); !ok || index != 2 || roomBattleProgress(current, index) != 1 {
		t.Fatal("ordinary win must skip optional awake wave")
	}
	current.engineBattleEnd = 4
	if index, ok := roomNextBattle(current); !ok || index != 1 || roomBattleProgress(current, index) != 0 {
		t.Fatal("awake must not increment visible progress")
	}
	current.engineBattleEnd = 2
	if _, ok := roomNextBattle(current); ok {
		t.Fatal("defeat started another wave")
	}
}

func TestRoomAwakeningCarriesBossCostAndDiscard(t *testing.T) {
	for _, keep := range []bool{false, true} {
		t.Run(strconv.FormatBool(keep), func(t *testing.T) {
			_, members := nextBattleFixture(t)
			for i := range members {
				members[i].Level, members[i].LeaderCardID, members[i].LeaderLevel, members[i].LeaderFame = 1, 1, 1, 1
				members[i].PartsIDs, members[i].DeckHonorIDs = make([]int, 7), make([]int, 4)
			}
			catalog := roomFixtureCatalog()
			level := catalog.EnemyLevels[1]
			level.InitialAwake = 1
			catalog.EnemyLevels[1] = level
			hub := NewHub()
			hub.combat = catalog
			server := &Server{hub: hub, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			client := &clientConn{server: server, conn: &sessionTestConn{}}
			spec := RoomSpec{BossID: 1, EnemyPartyID: 1, CostInitial: 3, HoldMax: 6, Seed: 602, Owner: members[0],
				AwakeTakeover: battlecore.WaveTakeover{KeepCost: keep, KeepDeckTrash: keep}, DropLedgerVersion: 1,
				Battles: []gamestate.TeamBattleReplayBattle{{EnemyPartyID: 1}, {EnemyPartyID: 1, EnemyType: 4}}}
			credential, err := hub.IssueCreate(spec)
			if err != nil {
				t.Fatal(err)
			}
			if err := client.handleCreate(joinCSV(strconv.Itoa(spec.Owner.UserID), "1", "0", "", "1", "1", "0", "0", "0", credential.AuthToken, credential.Signature, "android")); err != nil {
				t.Fatal(err)
			}
			current := hub.rooms[client.roomID]
			current.State, current.Members = RoomStateBattle, members
			current.hostCostPaid, current.battleLoading = true, map[int]bool{1: true}
			if err := server.tryStartLoadedBattle(client.roomID); err != nil {
				t.Fatal(err)
			}
			engine := current.engine
			checkCapacity := func(engine *BattleEngine) {
				t.Helper()
				rows, err := engine.ResumeResults()
				if err != nil {
					t.Fatal(err)
				}
				count := 0
				for _, row := range rows {
					if row.Command == CommandHoldMax {
						if row.Args[1] != 6 {
							t.Fatal("room lost the configured six hold slots", row)
						}
						count++
					}
				}
				if count != 4 {
					t.Fatal("room omitted a member hold capacity", count)
				}
			}
			checkCapacity(engine)
			for turn := 1; turn <= 7; turn++ {
				for _, phase := range []func() ([]BattleResult, error){engine.TurnPhase, engine.UserPhase} {
					if _, err := phase(); err != nil {
						t.Fatal(err)
					}
				}
				if turn == 7 && engine.StateView().Members[0].Cost != 9 {
					t.Fatal("fixture did not reach 9 COST before awakening")
				}
				finishFixtureUserAttack(t, engine, turn == 7)
				if turn < 7 {
					for _, phase := range []func() ([]BattleResult, error){engine.ExecuteChaliceUserPhase, engine.EnemyPhase, engine.ExecuteChaliceEnemyPhase} {
						if _, err := phase(); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			if engine.EndType() != 4 {
				t.Fatal("fixture did not awaken")
			}
			remaining := func(engine *BattleEngine) int64 {
				t.Helper()
				rows, err := engine.ResumeResults()
				if err != nil {
					t.Fatal(err)
				}
				for _, row := range rows {
					if row.Command == CommandResumeCardDeck && row.Args[0] == 1 {
						return row.Args[1]
					}
				}
				t.Fatal("resume omitted the player's draw pool")
				return 0
			}
			before := remaining(engine)
			current.engineBattleEnd, current.nextBattlePending, current.nextBattleIndex = 4, true, 1
			current.gameNextFinished = map[int]bool{1: true}
			if err := server.tryAdvanceNextBattle(client.roomID); err != nil {
				t.Fatal(err)
			}
			wantCost, wantPool := 3, before+1
			if keep {
				wantCost, wantPool = 9, before
			}
			next := current.engine
			checkCapacity(next)
			if next.StateView().Members[0].Cost != wantCost || remaining(next) != wantPool {
				t.Fatalf("awakening lost the room policy: cost=%d pool=%d, want %d/%d", next.StateView().Members[0].Cost, remaining(next), wantCost, wantPool)
			}
			if _, err := next.TurnPhase(); err != nil {
				t.Fatal(err)
			}
			wantTurnCost := 3
			if keep {
				wantTurnCost = 10
			}
			if next.StateView().Turn != 1 || next.StateView().Members[0].Cost != wantTurnCost {
				t.Fatal("incoming turn failed to reset its counter or advance COST", next.StateView())
			}
		})
	}
}

func TestComebackBetweenWavesUsesActualIndexAndWaitsForScene(t *testing.T) {
	engine, members := wonRoomBattle(t)
	hub := NewHub()
	server := &Server{hub: hub, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	output := &hubCheckingConn{Conn: left, hub: hub}
	client := &clientConn{server: server, conn: output}
	current := &room{RoomSnapshot: RoomSnapshot{RoomID: 123, BossID: 1, State: RoomStateBattle, Members: members},
		engine: engine, engineBattleEnd: 1, gameStarted: true, dropLedgerVersion: 1,
		battleIndex: 3, progress: 3, nextBattlePending: true, nextBattleIndex: 4,
		connections: map[int]*clientConn{}, disconnectedUntil: map[int]time.Time{1: time.Now().Add(time.Minute)},
		comebackTokens: map[int]string{1: "resume-token"}, gameNextFinished: map[int]bool{},
	}
	for i := 0; i < 6; i++ {
		current.battles = append(current.battles, gamestate.TeamBattleReplayBattle{EnemyPartyID: 1})
	}
	hub.rooms[123] = current
	if err := client.handleComeback("1001,123,resume-token,0"); err != nil {
		t.Fatal(err)
	}
	if err := client.handleReadyToComeback(""); err != nil {
		t.Fatal(err)
	}
	frames := output.output.String()
	if output.locked || !strings.Contains(frames, "RoomComeback{\n3,3,") || !strings.Contains(frames, "GameNextStart{\n4,4\n}") || strings.Contains(frames, "ApiGameStart{") || current.battleIndex != 3 {
		t.Fatalf("incorrect mid-wave comeback %s", frames)
	}
	stale := &clientConn{server: server, roomID: 123, memberType: 1}
	if err := stale.handleGameNextFinish(""); err == nil {
		t.Fatal("stale socket acknowledged new connection's scene")
	}
	if err := client.handleGameNextFinish(""); err != nil {
		t.Fatal(err)
	}
	if current.battleIndex != 4 || current.nextBattlePending || !strings.Contains(output.output.String(), "ApiGameStart{") {
		t.Fatal("resumed room failed to start its pending wave")
	}
}
