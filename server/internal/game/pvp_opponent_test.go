package game

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestPVPDailyChallengesPersistOnceAtCNMidnight(t *testing.T) {
	beforeMidnight := time.Date(2026, 9, 7, 15, 59, 59, 0, time.UTC)
	afterMidnight := beforeMidnight.Add(time.Second)
	config := PVPConfig{ChallengeMax: 5}
	s := &Account{onboarding: completedTestOnboarding(), pvp: gamestate.PVPPlayerState{Challenge: 0, ChallengeDay: pvpChallengeDay(beforeMidnight)}}
	writes := 0
	var saved []byte
	persist := func(state gamestate.State) error {
		writes++
		var err error
		saved, err = json.Marshal(state.PVP)
		return err
	}
	if err := s.RefreshPVPChallenges(config, beforeMidnight, gamestate.State{}, persist); err != nil || writes != 0 || s.pvp.Challenge != 0 {
		t.Fatalf("allowance replenished before midnight: writes=%d state=%+v error=%v", writes, s.pvp, err)
	}
	failure := errors.New("save unavailable")
	if err := s.RefreshPVPChallenges(config, afterMidnight, gamestate.State{}, func(gamestate.State) error { return failure }); !errors.Is(err, failure) || s.pvp.Challenge != 0 || s.pvp.ChallengeDay != pvpChallengeDay(beforeMidnight) {
		t.Fatalf("failed refill was not rolled back: state=%+v error=%v", s.pvp, err)
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			if err := s.RefreshPVPChallenges(config, afterMidnight, gamestate.State{}, persist); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	if writes != 1 || s.pvp.Challenge != 5 {
		t.Fatalf("concurrent midnight refresh: writes=%d state=%+v", writes, s.pvp)
	}
	reopened := &Account{onboarding: completedTestOnboarding()}
	if err := json.Unmarshal(saved, &reopened.pvp); err != nil {
		t.Fatal(err)
	}
	reopened.pvp.Challenge = 4 // one challenge consumed after the refill
	for _, now := range []time.Time{afterMidnight.Add(time.Hour), beforeMidnight} {
		if err := reopened.RefreshPVPChallenges(config, now, gamestate.State{}, persist); err != nil || writes != 1 || reopened.pvp.Challenge != 4 {
			t.Fatalf("reopen or clock rollback refilled twice: writes=%d state=%+v error=%v", writes, reopened.pvp, err)
		}
	}
	// A retained save without the day field can recover after its last ranked
	// debit, even if later free battles were played on the current day.
	unstamped := &Account{onboarding: completedTestOnboarding(), pvp: gamestate.PVPPlayerState{Challenge: 0, History: []gamestate.PVPMatch{
		{BattleType: 1, StartedAtUnix: beforeMidnight.Unix()},
		{BattleType: 0, StartedAtUnix: afterMidnight.Unix()},
	}}}
	if err := unstamped.RefreshPVPChallenges(config, afterMidnight, gamestate.State{}, nil); err != nil || unstamped.pvp.Challenge != 5 {
		t.Fatalf("prior ranked day did not restore allowance: state=%+v error=%v", unstamped.pvp, err)
	}
}
