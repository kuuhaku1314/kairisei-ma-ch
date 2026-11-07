package multiplayer

import (
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func recoveryEntryFixture(r *room) {
	r.BossID = 42
	r.BossGroup = map[string]any{"0": 4, "10": []any{map[string]any{"0": 42}}}
	r.battles = []gamestate.TeamBattleReplayBattle{{EnemyPartyID: 1}}
}

func reviveRecoveryFixture(t *testing.T, r *room) {
	t.Helper()
	plan, err := r.engine.PrepareContinue(2)
	if err != nil {
		t.Fatal(err)
	}
	if err = plan.Commit(); err != nil {
		t.Fatal(err)
	}
	if !r.engine.MemberAlive(1) {
		t.Fatal("fixture was not revived")
	}
}

func TestRecoveryKOIsTemporaryUntilExistingDeathBoundary(t *testing.T) {
	for _, removed := range []bool{false, true} {
		r, s, old := recoveryFixture(t, true)
		recoveryEntryFixture(r)
		r.engine, r.Members = knockedOutRoomBattle(t, 1, true, false)
		if r.comebackTokens[1] == "" || old.retired {
			t.Fatal("fixture already retired")
		}
		if s.hub.RecoveryFor(1001) != nil {
			t.Fatal("HTTP offered a KO member before the next-turn cleanup")
		}
		_, output := recoveryConnect(t, s, "original-token")
		if !strings.Contains(output.output.String(), "ComebackResult{\n-2,") || r.connections[1] != old || old.retired || r.comebackTokens[1] == "" {
			t.Fatal("KO took over the connection or revoked its still-pending revival eligibility")
		}
		if removed {
			detachDeadParticipantsLocked(r)
		}
		reviveRecoveryFixture(t, r)
		entry := s.hub.RecoveryFor(1001)
		_, after := recoveryConnect(t, s, "original-token")
		if removed {
			if entry != nil || !strings.Contains(after.output.String(), "ComebackResult{\n-1,") {
				t.Fatal("NPC revival restored a formally removed player's participation")
			}
		} else if entry == nil || !strings.Contains(after.output.String(), "ComebackResult{\n0,") {
			t.Fatal("revival before retirement did not preserve temporary-disconnect recovery")
		}
	}
}

func TestRecoveryRechecksLifeAtSnapshotAndApplication(t *testing.T) {
	for _, afterSnapshot := range []bool{false, true} {
		r, s, _ := recoveryFixture(t, false)
		c, output := recoveryConnect(t, s, "original-token")
		if afterSnapshot {
			if err := c.handleReadyToComeback(""); err != nil {
				t.Fatal(err)
			}
		}
		r.engine, r.Members = knockedOutRoomBattle(t, 1, true, false)
		r.userAttackStarted, r.chaliceUserStarted, r.enemyPhaseStarted = true, true, true
		var err error
		if afterSnapshot {
			err = c.handleComebackApplied(c.recoveryToken)
		} else {
			err = c.handleReadyToComeback("")
		}
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.output.String(), "RoomComebackFailed{\n-2,") || r.connections[1] != nil || c.retired ||
			r.comebackTokens[1] != "original-token" || !r.disconnectedUntil[1].After(time.Now()) {
			t.Fatal("life changed during recovery but still rejoined, or temporary KO became Retire")
		}
	}
}

func TestRecoveryExplicitDepartureDoesNotChangeNPCCharacter(t *testing.T) {
	for _, attached := range []bool{false, true} {
		r, s, old := recoveryFixture(t, attached)
		recoveryEntryFixture(r)
		r.pendingComebackTokens = map[int]string{1: "pending-token"}
		hp := r.engine.StateView().Members[0].HP
		s.hub.AbandonRecoveryFor(9999, 123)
		if r.comebackTokens[1] == "" {
			t.Fatal("another account cancelled the member")
		}
		s.hub.AbandonRecoveryFor(1001, 123)
		s.hub.AbandonRecoveryFor(1001, 123) // lost HTTP response can be retried
		if r.comebackTokens[1] != "" || r.pendingComebackTokens[1] != "" || !r.disconnectedUntil[1].IsZero() ||
			r.engine.StateView().Members[0].HP != hp || s.hub.RecoveryFor(1001) != nil {
			t.Fatal("departure retained recovery rights or killed/changed the NPC character")
		}
		if attached {
			if !old.retired {
				t.Fatal("departure did not mark the attached participant")
			}
			if err := old.close(true); err != nil {
				t.Fatal(err)
			} // existing socket reader cleanup
		}
		_, output := recoveryConnect(t, s, "pending-token")
		if !strings.Contains(output.output.String(), "ComebackResult{\n-1,") {
			t.Fatal("living withdrawn participant rejoined with a pending token")
		}
	}
}
