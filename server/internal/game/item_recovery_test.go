package game

import (
	"errors"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestRecoveryItemFunctions(t *testing.T) {
	for _, tc := range []struct {
		function string
		bp, ap   int
	}{
		{"BP_HEAL_FULL", 107, 1}, {"BP_HEAL_HALF", 64, 1}, {"BP_HEAL_30", 50, 1},
		{"AP_HEAL_FULL", 20, 3}, {"AP_HEAL_1", 20, 2},
	} {
		t.Run(tc.function, func(t *testing.T) {
			s := &Account{onboarding: completedTestOnboarding(), bp: 20, bpMax: 87, ap: 1, apMax: 3, bpRecoveryInterval: time.Hour, apRecoveryInterval: time.Hour,
				items: map[int]gamestate.Item{1002: {ItemID: 1002, Num: 1}}, itemDefinitions: map[int]gamestate.ItemDefinition{1002: {ItemID: 1002, Function: tc.function}}}
			result, err := s.UseItem(1002)
			if err != nil {
				t.Fatal(err)
			}
			if result.BP.Current != tc.bp || result.AP.Current != tc.ap || result.Item.Num != 0 {
				t.Fatalf("result %+v", result)
			}
			if _, err = s.UseItem(1002); !errors.Is(err, ErrInsufficientMaterials) {
				t.Fatalf("empty inventory: %v", err)
			}
		})
	}
	t.Run("overflow pauses recovery and full rejects another item", func(t *testing.T) {
		s := &Account{onboarding: completedTestOnboarding(), bp: 80, bpMax: 87, ap: 3, apMax: 3, bpRecoveryInterval: time.Hour,
			items: map[int]gamestate.Item{1002: {ItemID: 1002, Num: 2}}, itemDefinitions: map[int]gamestate.ItemDefinition{1002: {ItemID: 1002, Function: "BP_HEAL_30"}}}
		if result, err := s.UseItem(1002); err != nil || result.BP.Current != 110 || !s.bpNextRecovery.IsZero() {
			t.Fatalf("cap: %+v %v", result, err)
		}
		_, err := s.UseItem(1002)
		var business *BusinessError
		if !errors.As(err, &business) || s.items[1002].Num != 1 {
			t.Fatalf("full: %v / %v", err, s.items)
		}
		now := time.Now().Add(24 * time.Hour)
		if got := s.battlePointStatusLocked(now); got.Current != 110 || got.NextSeconds != 0 {
			t.Fatalf("overflow was lost: %+v", got)
		}
		s.bp -= 24
		if got := s.battlePointStatusLocked(now); got.Current != 86 || got.NextSeconds != 3600 {
			t.Fatalf("recovery did not resume: %+v", got)
		}
		if got := s.battlePointStatusLocked(now.Add(2 * time.Hour)); got.Current != 87 || got.NextSeconds != 0 {
			t.Fatalf("natural recovery exceeded capacity: %+v", got)
		}
	})
}

// The original PointTimer uses heal_sec both for every subsequent point and
// for its full-recovery estimate. It must never be the missing points' total.
func TestPointTimerIntervals(t *testing.T) {
	now := time.Unix(1800000000, 0)
	s := &Account{onboarding: completedTestOnboarding(), bp: 53, bpMax: 74, bpRecoveryInterval: 3 * time.Minute,
		bpNextRecovery: now.Add(150 * time.Second), ap: 0, apMax: 3,
		apRecoveryInterval: 2 * time.Hour, apNextRecovery: now.Add(time.Hour)}
	bp := s.battlePointStatusLocked(now)
	ap := s.apStatusLocked(now)
	if bp.NextSeconds != 150 || bp.IntervalSeconds != 180 ||
		bp.NextSeconds+(bp.Max-bp.Current-1)*bp.IntervalSeconds != 3750 {
		t.Fatalf("21 missing BP: %+v", bp)
	}
	if ap.NextSeconds != 3600 || ap.IntervalSeconds != 7200 ||
		ap.NextSeconds+(ap.Max-ap.Current-1)*ap.IntervalSeconds != 18000 {
		t.Fatalf("3 missing AP: %+v", ap)
	}
	bp = s.battlePointStatusLocked(now.Add(150 * time.Second))
	if bp.Current != 54 || bp.NextSeconds != 180 || bp.IntervalSeconds != 180 {
		t.Fatalf("first BP recovered: %+v", bp)
	}
	ap = s.apStatusLocked(now.Add(time.Hour))
	if ap.Current != 1 || ap.NextSeconds != 7200 || ap.IntervalSeconds != 7200 {
		t.Fatalf("first AP recovered: %+v", ap)
	}
	bp = s.battlePointStatusLocked(now.Add(3750 * time.Second))
	ap = s.apStatusLocked(now.Add(5 * time.Hour))
	if bp.Current != 74 || bp.NextSeconds != 0 || bp.IntervalSeconds != 180 ||
		ap.Current != 3 || ap.NextSeconds != 0 || ap.IntervalSeconds != 7200 {
		t.Fatalf("full must retain cached intervals: BP=%+v AP=%+v", bp, ap)
	}
	// Leaving capacity starts a new next-point timer without changing cadence.
	s.bp -= 21
	s.ap--
	bp = s.battlePointStatusLocked(now.Add(6 * time.Hour))
	ap = s.apStatusLocked(now.Add(6 * time.Hour))
	if bp.NextSeconds != 180 || bp.IntervalSeconds != 180 ||
		ap.NextSeconds != 7200 || ap.IntervalSeconds != 7200 {
		t.Fatalf("consumption after full: BP=%+v AP=%+v", bp, ap)
	}
}
