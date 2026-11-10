package game

import (
	"errors"
	"sync"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestLocalShopRetryDailyBoundaryAndFailedCommit(t *testing.T) {
	s := &Account{onboarding: completedTestOnboarding(), localCrystalPurchaseEnabled: true}
	now := time.Date(2026, 9, 6, 15, 59, 0, 0, time.UTC) // 23:59 CN
	order := "local:1:00000000000000000000000000000001"
	var saved gamestate.State
	persist := func(state gamestate.State) error { saved = state; return nil }
	fail := func(gamestate.State) error { return errors.New("disk unavailable") }
	if _, err := s.LocalPurchase(order, now, gamestate.State{}, fail); err == nil || s.coinFree != 0 || len(s.localShop.Orders) != 0 || s.localShop.MonthEndDay != 0 {
		t.Fatal("failed commit granted a purchase")
	}
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := s.LocalPurchase(order, now, gamestate.State{}, persist); err != nil {
				t.Error(err)
			}
		}()
	}
	group.Wait()
	if s.coinFree != 250 || saved.User.CoinFree != 250 || len(saved.LocalShop.Orders) != 1 {
		t.Fatal("order retry granted crystals twice")
	}
	if _, err := s.LocalCardClaim(0, now, gamestate.State{}, fail); err == nil || s.coinFree != 250 || s.localShop.MonthClaimDay != 0 {
		t.Fatal("failed reward commit consumed daily claim")
	}
	for i, expected := range []int{0, 4, 0} {
		when := now
		if i == 2 {
			when = now.Add(2 * time.Minute)
		}
		result, err := s.LocalCardClaim(0, when, gamestate.State{}, persist)
		if err != nil || result["code"] != expected {
			t.Fatalf("claim %d: %v %v", i, result, err)
		}
	}
	if s.coinFree != 450 || saved.User.CoinFree != 450 {
		t.Fatal("daily boundary reward differs")
	}
	result, err := s.LocalCardClaim(0, now.AddDate(0, 0, 30), gamestate.State{}, persist)
	if err != nil || result["code"] != 3 || s.coinFree != 450 {
		t.Fatal("expired card still paid out")
	}
	if _, err := s.LocalPurchase("local:8:00000000000000000000000000000002", now, gamestate.State{}, persist); err != nil || s.coinFree != 450+6480 {
		t.Fatal("crystals do not match the displayed bundle")
	}
	s.localCrystalPurchaseEnabled = false
	blockedOrder := "local:8:00000000000000000000000000000003"
	balance := s.coinFree
	result, err = s.LocalPurchase(blockedOrder, now, gamestate.State{}, persist)
	if err != nil || result["code"] != 1 || result["addcoin"] != 0 || s.coinFree != balance {
		t.Fatal("disabled purchase must succeed without increasing crystals")
	}
	s.localShop = cloneLocalShop(saved.LocalShop) // retained after restart
	s.localCrystalPurchaseEnabled = true
	result, err = s.LocalPurchase(blockedOrder, now, gamestate.State{}, persist)
	if err != nil || result["addcoin"] != 0 || s.coinFree != balance {
		t.Fatal("enabling purchases backfilled a previously closed order")
	}
}

func TestLocalMonthlyAndForeverCardEntitlements(t *testing.T) {
	s := &Account{onboarding: completedTestOnboarding(), localCrystalPurchaseEnabled: true}
	now := time.Date(2026, 9, 10, 4, 0, 0, 0, time.UTC)
	day := localShopDay(now)
	var saved gamestate.State
	persist := func(state gamestate.State) error { saved = state; return nil }
	buy := func(order string, when time.Time) {
		t.Helper()
		if _, err := s.LocalPurchase(order, when, gamestate.State{}, persist); err != nil {
			t.Fatal(err)
		}
	}
	for cardType, want := range []int{2, 5} {
		result, err := s.LocalCardClaim(cardType, now, gamestate.State{}, persist)
		if err != nil || result["code"] != want || s.coinFree != 0 {
			t.Fatalf("unowned card %d: %v, %v", cardType, result, err)
		}
	}
	buy("local:1:00000000000000000000000000000010", now)
	buy("local:1:00000000000000000000000000000011", now.AddDate(0, 0, 1))
	buy("local:1:00000000000000000000000000000011", now.AddDate(0, 0, 1))
	if s.localShop.MonthEndDay != day+60 || s.coinFree != 500 {
		t.Fatal("monthly renewal or retry extended/granted incorrectly")
	}
	buy("local:2:00000000000000000000000000000012", now)
	buy("local:2:00000000000000000000000000000013", now)
	if !s.localShop.Forever || s.coinFree != 1100 {
		t.Fatal("forever card must pay its 600 initial crystals only once")
	}
	for cardType := range 2 {
		for _, want := range []int{0, 4 + 2*cardType} {
			result, err := s.LocalCardClaim(cardType, now, gamestate.State{}, persist)
			if err != nil || result["code"] != want {
				t.Fatalf("daily card %d: %v, %v", cardType, result, err)
			}
		}
	}
	// Restore the persisted account entitlement, as after an account reload.
	s.localShop, s.coinFree = cloneLocalShop(saved.LocalShop), saved.User.CoinFree
	month, forever, days := s.localCardFlagsLocked(now)
	if s.coinFree != 1300 || month != 60<<16|2 || forever != 2 || days != 60 {
		t.Fatalf("saved daily flags differ: %d, %d, %d", month, forever, days)
	}
	// A fresh order after claiming must not reset either daily claim marker.
	buy("local:1:00000000000000000000000000000015", now)
	buy("local:2:00000000000000000000000000000016", now)
	s.localShop, s.coinFree = cloneLocalShop(saved.LocalShop), saved.User.CoinFree
	if s.localShop.MonthEndDay != day+90 || s.coinFree != 1550 {
		t.Fatal("renewal must only extend the term and grant its initial purchase bonus")
	}
	var group sync.WaitGroup
	for i := range 16 {
		group.Add(1)
		go func(cardType int) {
			defer group.Done()
			result, err := s.LocalCardClaim(cardType, now, gamestate.State{}, persist)
			if err != nil || result["code"] != 4+2*cardType {
				t.Errorf("repurchase reset daily claim %d: %v, %v", cardType, result, err)
			}
		}(i % 2)
	}
	group.Wait()
	if s.coinFree != 1550 {
		t.Fatal("same-day concurrent claims after repurchase paid twice")
	}
	later := now.AddDate(2, 0, 0)
	result, err := s.LocalCardClaim(0, later, gamestate.State{}, persist)
	if err != nil || result["code"] != 3 {
		t.Fatal("monthly card did not expire")
	}
	result, err = s.LocalCardClaim(1, later, gamestate.State{}, persist)
	if err != nil || result["code"] != 0 || s.coinFree != 1650 {
		t.Fatal("forever card expired or backfilled missed days")
	}
	// Purchase payout is optional; it does not disable subscription entitlement.
	s.localCrystalPurchaseEnabled = false
	buy("local:1:00000000000000000000000000000014", later)
	if s.localShop.MonthEndDay != localShopDay(later)+30 || s.coinFree != 1650 {
		t.Fatal("disabled purchase payout changed monthly entitlement rules")
	}
	result, err = s.LocalCardClaim(0, later, gamestate.State{}, persist)
	if err != nil || result["code"] != 0 || s.coinFree != 1750 {
		t.Fatal("monthly daily reward should remain independently claimable")
	}
}
