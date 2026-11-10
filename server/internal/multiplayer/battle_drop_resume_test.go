package multiplayer

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/battlecore"
	"kairisei.local/server/internal/gamestate"
)

func TestCompletedComebackAfterFinalFrameDisconnect(t *testing.T) {
	for _, endType := range []int{1, 2, 4} {
		t.Run(strconv.Itoa(endType), func(t *testing.T) {
			var engine *BattleEngine
			var members []Member
			wantRewards := 0
			if endType == 1 {
				engine, members = wonRoomBattle(t, BattleDrop{RewardType: 4, Num: 10})
				wantRewards = 1
			} else if endType == 4 {
				engine, members = configuredRoomBattle(t, func(definitions *battlecore.Definitions, config *battlecore.Config, _ []battlecore.Participant) {
					level := definitions.EnemyLevels[1]
					level.InitialAwake = 1
					definitions.EnemyLevels[1] = level
					config.Drops = []BattleDrop{{RewardType: 4, Num: 10}}
				})
				openFixtureUserPhase(t, engine)
				finishFixtureUserAttack(t, engine, true)
				if engine.EndType() != 4 {
					t.Fatal("fixture did not reach an ordinary awakened-body defeat")
				}
				wantRewards = 1
			} else {
				engine, members = knockedOutRoomBattle(t, 4, false, false)
			}
			hub := NewHub()
			repository := &terminalCompletionRepository{}
			hub.repository = repository
			hub.completionCommit = repository.SaveCompleted
			server, err := NewServer(hub, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			left, right := net.Pipe()
			t.Cleanup(func() { left.Close(); right.Close() })
			old := &clientConn{server: server, conn: left, roomID: 123, memberType: 1, userID: 1001}
			current := &room{RoomSnapshot: RoomSnapshot{RoomID: 123, BossID: 1, State: RoomStateBattle, Members: members},
				engine: engine, engineBattleEnd: endType, gameStarted: true, hostCostPaid: true, connections: map[int]*clientConn{1: old}, comebackTokens: map[int]string{1: "final-token"},
				disconnectedUntil: map[int]time.Time{},
				dropPlan:          []gamestate.TeamBattleEnemyDrop{{EnemyIndex: 0, Reward: gamestate.Reward{Type: 4, Num: 10}}},
			}
			hub.rooms[123] = current
			// Drop the peer before delivery: the actual completion dispatcher commits
			// the outcome, then encounters the failed terminal write.
			if err := right.Close(); err != nil {
				t.Fatal(err)
			}
			session := hub.lockRoomSession(current.RoomID)
			err = server.completeGoBattleLocked(session, current)
			if err != nil {
				t.Fatal(err)
			}
			// The room is committed, but ApiGameEnd/GameClose have not reached this
			// peer. The ordinary disconnect path must retain its result comeback.
			if err := old.close(true); err != nil {
				t.Fatal(err)
			}
			before, err := hub.SettlementFor(123, 1001)
			if (endType != 2) != (err == nil) {
				t.Fatal(err)
			}
			if repository.saves != wantRewards {
				t.Fatal("defeat was persisted as a claimable victory")
			}
			state := engine.StateView()
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			served := make(chan error, 1)
			go func() { served <- server.Serve(listener) }()
			t.Cleanup(func() {
				server.Close()
				listener.Close()
				select {
				case err := <-served:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(time.Second):
					t.Error("temporary BattleSv listener did not stop")
				}
			})
			peer, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer peer.Close()
			_ = peer.SetDeadline(time.Now().Add(5 * time.Second))
			reader := bufio.NewReader(peer)
			if _, err := io.WriteString(peer, "Comeback{\n1001,123,final-token,0\n}\n"); err != nil {
				t.Fatal(err)
			}
			method, payload, err := readFrame(reader)
			if err != nil || method != "ComebackResult" || !strings.HasPrefix(payload, "0,") {
				t.Fatalf("completed result comeback rejected: %s %q %v", method, payload, err)
			}
			if _, err := io.WriteString(peer, "ReadyToComeback{\n}\n"); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"RoomComeback", "ApiGameEnd", "GameClose"} {
				method, payload, err = readFrame(reader)
				if err != nil || method != want {
					t.Fatalf("terminal comeback order: %s %q %v; want %s", method, payload, err, want)
				}
				if method == "RoomComeback" && (!strings.HasPrefix(payload, "0,0,") || strings.Contains(payload, "final-token")) ||
					method == "ApiGameEnd" && payload != "2,"+strconv.Itoa(endType) {
					t.Fatalf("incorrect terminal comeback payload: %s %q", method, payload)
				}
			}
			after, err := hub.SettlementFor(123, 1001)
			if (endType != 2) != (err == nil) || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(engine.StateView(), state) || len(after.ReleasedDrops) != wantRewards {
				t.Fatal("result recovery changed settlement, drops, or battle state", err)
			}
			if endType == 2 {
				hub.mu.Lock()
				retained := hub.completed[123]
				if retained == nil || len(retained.OnlineUserIDs) != 0 || retained.expiresAt.After(time.Now().Add(comebackLifetime)) {
					t.Error("defeat retained reward eligibility or an unbounded recovery window")
				}
				hub.pruneCompletedLocked(time.Now().Add(comebackLifetime + time.Second))
				_, exists := hub.completed[123]
				hub.mu.Unlock()
				if exists {
					t.Fatal("defeat recovery was not expired")
				}
			}
		})
	}
}

type terminalCompletionRepository struct {
	CompletionRepository
	saves int
}

func (repository *terminalCompletionRepository) SaveCompleted(CompletedBattle, time.Time) error {
	repository.saves++
	return nil
}

func (*terminalCompletionRepository) LoadCompleted(int64, time.Time) (CompletedBattle, time.Time, error) {
	return CompletedBattle{}, time.Time{}, ErrCompletedBattleUnavailable
}

func TestComebackRestoresPriorWaveRewardsWithoutDuplicatingCurrentDrops(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		name := "pending-next-wave"
		if terminal {
			name = "completed-room"
		}
		t.Run(name, func(t *testing.T) {
			engine, members := wonRoomBattle(t, BattleDrop{RewardType: 4, Num: 99})
			ledger := []gamestate.TeamBattleEnemyDrop{
				{BattleIndex: 0, Reward: gamestate.Reward{Type: 6, Num: 1, RewardTypeID: 101001}},
				{BattleIndex: 1, Reward: gamestate.Reward{Type: 4, Num: 10}},
				{BattleIndex: 1, Reward: gamestate.Reward{Type: 6, Num: 1, RewardTypeID: 101001}},
				{BattleIndex: 2, Reward: gamestate.Reward{Type: 4, Num: 99}},
			}
			before := cloneDropPlan(ledger)
			state := engine.StateView()
			hub := NewHub()
			server := &Server{hub: hub, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			left, right := net.Pipe()
			t.Cleanup(func() { left.Close(); right.Close() })
			output := &hubCheckingConn{Conn: left, hub: hub}
			client := &clientConn{server: server, conn: output, roomID: 123, memberType: 1,
				userID: 1001, comebackPending: true, completedComeback: terminal}
			if terminal {
				hub.completed[123] = &completedBattle{CompletedBattle: CompletedBattle{RoomID: 123, Members: members,
					BattleIndex: 2, Progress: 2, ReleasedDrops: ledger}, terminalEngine: engine, terminalBattleEndType: 1,
					expiresAt:           time.Now().Add(completedLifetime),
					comebackConnections: map[int]*clientConn{1: client}, comebackTokens: map[int]string{}}
			} else {
				hub.rooms[123] = &room{RoomSnapshot: RoomSnapshot{RoomID: 123, State: RoomStateBattle, Members: members},
					engine: engine, battleIndex: 2, progress: 2, releasedDrops: ledger, nextBattlePending: true, nextBattleIndex: 3,
					connections: map[int]*clientConn{1: client}, comebackTokens: map[int]string{}}
			}
			if err := client.handleReadyToComeback(""); err != nil {
				t.Fatal(err)
			}
			wire := output.output.String()
			if output.locked || !strings.Contains(wire, "109,6,1,101001\n109,4,10,0\n109,6,1,101001\n100,1,") ||
				strings.Count(wire, "\n109,") != 3 || strings.Contains(wire, "109,4,99,0") ||
				strings.Count(wire, "108,5,0,4,99,0") != 1 {
				t.Fatalf("incorrect reward restoration: %s", wire)
			}
			if !reflect.DeepEqual(ledger, before) || !reflect.DeepEqual(engine.StateView(), state) {
				t.Fatal("comeback changed the settlement ledger or battle state")
			}
		})
	}
}
