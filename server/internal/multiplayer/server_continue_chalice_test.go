package multiplayer

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"kairisei.local/server/battlecore"
)

// An enemy direction is still visible after the core has computed the KO.
// Its reservation must survive the continuation snapshot and execute once only
// if the member revived; cancellation must not spend the dead member's sphere.
func TestContinueRestoresPublishedChaliceReservation(t *testing.T) {
	for _, revive := range []bool{true, false} {
		name := "cancel"
		if revive {
			name = "paid"
		}
		t.Run(name, func(t *testing.T) {
			engine, members := configuredRoomBattle(t, func(defs *battlecore.Definitions, config *battlecore.Config, players []battlecore.Participant) {
				config.ContinueAllowed = true
				enemy := defs.Enemies[1]
				enemy.HP = 1000000
				defs.Enemies[1] = enemy
				level := defs.EnemyLevels[1]
				level.ActionsPerTurn = 1
				level.Actions = []battlecore.CombatEnemyAction{{Slot: 0, Category: "normal", SkillID: 9001, Target: "USER_ALL", ActionCost: 1, MaxUses: 100, Rate: 100}}
				defs.EnemyLevels[1] = level
				defs.Skills[9001] = []battlecore.CombatSkillDefinition{{ID: 9001, FunctionID: 9001, Target: "USER_ALL"}}
				defs.SkillRoles[9001] = []battlecore.CombatSkillRole{{Function: "ATTACK_AA", Target: "SELECT", Parameters: [10]string{"200", "0", "0", "0", "1", "ATK", "0", "FIRE", "PHYSICS"}}}
				players[0].HP = 1
				maximum := 1000
				players[0].MaxHP = &maximum
				players[0].DeckSpheres = []BattleSphere{{SphereType: 1, SphereID: 9, Level: 1}}
				defs.Spheres = map[int]battlecore.CombatSphereDefinition{9: {ID: 9, Type: SphereTypeChalice, MaxLevel: 1, SkillID: 9, Count: 1, PlayCondition: "NONE"}}
				defs.Skills[9] = []battlecore.CombatSkillDefinition{{ID: 9, FunctionID: 9, Target: "ENEMY_ALL", Attribute: "FIRE", Kind: "ATTACK"}}
				defs.SkillRoles[9] = []battlecore.CombatSkillRole{{Function: "ATTACK_AA", Target: "SELECT", Parameters: [10]string{"200", "0", "0", "0", "1", "ATK", "0", "FIRE", "PHYSICS"}}}
			})
			openFixtureUserPhase(t, engine)
			finishFixtureUserAttack(t, engine, false)
			if _, err := engine.ExecuteChaliceUserPhase(); err != nil {
				t.Fatal(err)
			}
			hub := NewHub()
			charges := 0
			if err := hub.AttachContinueAuthorizer(func(BattleContinue) (ContinueBalance, error) {
				charges++
				return ContinueBalance{CoinFree: 50}, nil
			}); err != nil {
				t.Fatal(err)
			}
			s := &Server{hub: hub, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			current := &room{RoomSnapshot: RoomSnapshot{RoomID: 1, BossID: 11, State: RoomStateBattle, OwnerMemberType: 1, Members: members},
				continueAllowed: true, engine: engine, connections: make(map[int]*clientConn), comebackTokens: map[int]string{1: "token1", 2: "token2"},
				gameStarted: true, turnPhaseStarted: true, userPhaseStarted: true, userAttackStarted: true, chaliceUserStarted: true,
				enemyPhaseStarted: true, enemyPhaseFinished: make(map[int]bool), chaliceEnemyFinished: make(map[int]bool)}
			owner, guest := continueTestPeer(t, s, 1), continueTestPeer(t, s, 2)
			current.connections[1], current.connections[2] = owner, guest
			hub.rooms[1] = current
			t.Cleanup(current.continuationTimerStopForTest)
			current.openEnemyChaliceInput()
			if _, err := engine.EnemyPhase(); err != nil || !engine.ContinuePending() {
				t.Fatal("fixture did not pause at enemy KO", err)
			}
			if err := owner.handleChaliceSphrReserve("1"); err != nil || current.chaliceInput.reserved[0] != 1 {
				t.Fatal("KO during animation rejected published reservation", err)
			}
			for _, peer := range []*clientConn{owner, guest} {
				if err := peer.handleEnemyPhaseFinish(); err != nil {
					t.Fatal(err)
				}
			}
			if revive {
				if err := owner.handleContinue(""); err != nil {
					t.Fatal(err)
				}
			} else {
				for _, peer := range []*clientConn{owner, guest} {
					if err := peer.handleContinuePhaseFinish(""); err != nil {
						t.Fatal(err)
					}
				}
			}
			for _, peer := range []*clientConn{owner, guest} {
				if err := peer.handleGameOverPhaseFinish(""); err != nil {
					t.Fatal(err)
				}
			}
			output := guest.conn.(*hubCheckingConn).output.String()
			_, restored, found := strings.Cut(output, "RoomComeback{")
			snapshot, execution, advanced := strings.Cut(restored, "ApiChaliceSphrExecEnemyPhase{")
			if !found || !advanced || !strings.Contains(snapshot, "318,1,1\n") {
				t.Fatal("continuation snapshot lost the already broadcast reservation", output)
			}
			want := 0
			if revive {
				want = 1
			}
			if charges != want || strings.Count(execution, "\n56,1,1,") != want || engine.TurnNumber() != 1 {
				t.Fatal("continuation replayed the turn, charged twice or executed a retired sphere", charges, execution)
			}
			if err := guest.handleGameOverPhaseFinish(""); err != nil || guest.conn.(*hubCheckingConn).output.String() != output {
				t.Fatal("duplicate presentation ACK replayed the continuation or summon", err)
			}
		})
	}
}
