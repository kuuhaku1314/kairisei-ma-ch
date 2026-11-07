package battlecore

import (
	"reflect"
	"testing"
)

func TestResumeBetweenTurnAndUserPhaseUsesPreparedHands(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	uninterrupted, _ := coreBattleFixture(t)
	for _, e := range []*BattleEngine{engine, uninterrupted} {
		if _, err := e.Start(); err != nil {
			t.Fatal(err)
		}
		if _, err := e.TurnPhase(); err != nil {
			t.Fatal(err)
		}
		for _, player := range e.players {
			if playerHandCount(&player) != 5 || player.remainingDeckCount() != 5 {
				t.Fatal("TurnPhase must prepare the authoritative hand before its ACK")
			}
		}
	}
	before := *engine
	rows, err := engine.ResumeResults()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*engine, before) {
		t.Fatal("resume changed live hand, RNG or display cache")
	}
	identities, handRows := 0, 0
	for index, row := range rows {
		switch row.Command {
		case 23:
			if index != identities {
				t.Fatal("CARD identities must be the leading family")
			}
			identities++
		case CommandResumeCardHand:
			handRows++
			if len(row.Args) != 11 {
				t.Fatal("resume must include all five hand slots")
			}
			for i := 1; i < len(row.Args); i += 2 {
				if row.Args[i] == 0 || row.Args[i+1] != 0 {
					t.Fatal("resume hand initializes identity with zero power before CARD_UPDATE")
				}
			}
		case CommandCardDeal:
			t.Fatal("resume must not replay the original DEAL event")
		}
	}
	if identities != 80 || handRows != 4 {
		t.Fatalf("incomplete identity/hand snapshot: %d/%d", identities, handRows)
	}
	repeated, err := engine.ResumeResults()
	if err != nil || !reflect.DeepEqual(rows, repeated) {
		t.Fatal("repeated snapshot changed", err)
	}
	actual, err := engine.UserPhase()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := uninterrupted.UserPhase()
	if err != nil || !reflect.DeepEqual(actual, expected) || engine.rng != uninterrupted.rng {
		t.Fatal("resume changed the subsequent UserPhase or RNG", err)
	}
	dealt := 0
	for _, row := range actual {
		if row.Command == CommandCardDeal {
			dealt++
		}
	}
	if dealt != 20 {
		t.Fatal("UserPhase lost the prepared draw notifications")
	}
}

func TestResumeBuffOrderAndNativeAttributeBits(t *testing.T) {
	effects := []battleEffect{
		{Function: "ATK_UP_FIXED", ListType: 6, Remaining: 1},
		{Function: "ENCHANT", ListType: 0, Remaining: 2, Attribute: "FIRE"},
		{Function: "DEF_UP_FIXED", ListType: 3, Remaining: 3, Attribute: "NEUTRAL"},
	}
	rows := appendResumeBuffs(nil, 1, effects, false)
	if len(rows) != 3 || rows[0].Args[1] != 0 || rows[1].Args[1] != 3 || rows[2].Args[1] != 6 ||
		rows[0].Args[6] != 2 || rows[1].Args[6] != 512 || rows[2].Args[6] != 1 {
		t.Fatalf("resume list order or enum attribute bits changed: %+v", rows)
	}
	hold := resumeHoldResult(1, battleBlessHold{CardType: 22, Repeat: true})
	if hold.Args[3] != 0 {
		t.Fatal("appended skill resume flag is not BLESS Repeat")
	}
}
