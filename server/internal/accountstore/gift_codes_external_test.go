package accountstore_test

import (
	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
	"sync"
	"testing"
	"time"
)

func TestGiftCodeConcurrentClaimAndFailedSaveAreAtomic(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	db := accounts.Database()
	users := []int{testfixture.CreateNamedFriendCapacityAccount(t, accounts, 901), testfixture.CreateNamedFriendCapacityAccount(t, accounts, 902)}
	base := testfixture.RuntimeState(t)
	db.SetCatalog(base)
	for _, user := range users {
		state := base
		state.User.UserID = user
		if err := accounts.PersistState(user, state); err != nil {
			t.Fatal(err)
		}
	}
	registry, revision, err := db.SaveGiftBatch(0, game.GiftCodeBatch{Name: "礼包", Mode: "single", ClaimScope: "batch", Title: "奖励", Message: "领取", Enabled: true, Published: true, Rewards: []gamestate.Reward{{Type: 4, Num: 7}, {Type: 10, Num: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	generation := accountstore.GiftCodeGeneration{BatchID: registry.Batches[0].ID, ExpectedRevision: revision, RequestKey: "test-generation-unique-key", Codes: []string{"RACE-ONE", "ROLL-BACK"}}
	ids, err := db.GenerateGiftCodes(generation)
	if err != nil {
		t.Fatal(err)
	}
	again, err := db.GenerateGiftCodes(generation)
	if err != nil || len(again) != 2 || again[0] != ids[0] {
		t.Fatal("generation retry issued new codes", again, err)
	}
	now := time.Now()
	offers := make([]game.GiftCodeOffer, 2)
	states := make([]gamestate.State, 2)
	presents := make([][]int64, 2)
	original := make([]int, 2)
	for i, user := range users {
		state, err := accounts.LoadState(user)
		if err != nil {
			t.Fatal(err)
		}
		original[i] = len(state.Engagement.Presents)
		offer, code, err := accounts.PrepareGiftCode(user, " race-one ", now)
		if err != nil || code != 0 {
			t.Fatal(code, err)
		}
		offers[i] = offer
		account, err := game.New(state)
		if err != nil {
			t.Fatal(err)
		}
		presents[i], err = account.PrepareGiftPresents(offer.Batch)
		if err != nil {
			t.Fatal(err)
		}
		states[i] = account.Snapshot(state)
	}
	var wg sync.WaitGroup
	codes := make([]int, 2)
	errs := make([]error, 2)
	for i := range users {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i], errs[i] = accounts.CommitGiftCode(offers[i], states[i], presents[i], now)
		}(i)
	}
	wg.Wait()
	winners := 0
	loser := -1
	for i, user := range users {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		state, err := accounts.LoadState(user)
		if err != nil {
			t.Fatal(err)
		}
		if codes[i] == 0 {
			winners++
			if len(state.Engagement.Presents) != original[i]+2 {
				t.Fatal("incomplete gift mails")
			}
		} else {
			loser = i
			if codes[i] != 3 || len(state.Engagement.Presents) != original[i] {
				t.Fatal("loser retained partial gifts", codes[i])
			}
		}
	}
	if winners != 1 {
		t.Fatal("single code had multiple winners", codes)
	}
	user := users[loser]
	offer, code, err := accounts.PrepareGiftCode(user, "ROLL-BACK", now.Add(4*time.Second))
	if err != nil || code != 0 {
		t.Fatal(code, err)
	}
	state, err := accounts.LoadState(user)
	if err != nil {
		t.Fatal(err)
	}
	account, err := game.New(state)
	if err != nil {
		t.Fatal(err)
	}
	mailIDs, err := account.PrepareGiftPresents(offer.Batch)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sqlDB.Exec(`CREATE TRIGGER reject_gift_save BEFORE UPDATE ON cn_account_snapshot BEGIN SELECT RAISE(ABORT,'forced write failure'); END; CREATE TRIGGER reject_gift_primary BEFORE UPDATE ON cn_save_snapshot BEGIN SELECT RAISE(ABORT,'forced write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = accounts.CommitGiftCode(offer, account.Snapshot(state), mailIDs, now); err == nil {
		t.Fatal("forced save unexpectedly succeeded")
	}
	var used int
	if err = sqlDB.QueryRow(`SELECT count(*) FROM cn_gift_redemption WHERE code_id=?`, ids[1]).Scan(&used); err != nil || used != 0 {
		t.Fatal("failed save consumed code", used, err)
	}
	if _, err = sqlDB.Exec(`DROP TRIGGER reject_gift_save; DROP TRIGGER reject_gift_primary`); err != nil {
		t.Fatal(err)
	}
	if code, err = accounts.CommitGiftCode(offer, account.Snapshot(state), mailIDs, now); err != nil || code != 0 {
		t.Fatal("rollback did not leave code redeemable", code, err)
	}
	if _, code, err = accounts.PrepareGiftCode(user, "ROLL-BACK", now.Add(8*time.Second)); err != nil || code != 3 {
		t.Fatal("duplicate was not rejected", code, err)
	}
}

func TestGiftPublicQuotaAndClaimScopes(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	db := accounts.Database()
	users := []int{testfixture.CreateNamedFriendCapacityAccount(t, accounts, 911), testfixture.CreateNamedFriendCapacityAccount(t, accounts, 912)}
	base := testfixture.RuntimeState(t)
	db.SetCatalog(base)
	for _, user := range users {
		state := base
		state.User.UserID = user
		if err := accounts.PersistState(user, state); err != nil {
			t.Fatal(err)
		}
	}
	revision := 0
	create := func(name, scope, group string, limit int) game.GiftCodeBatch {
		t.Helper()
		registry, next, err := db.SaveGiftBatch(revision, game.GiftCodeBatch{Name: name, Mode: "public", ClaimScope: scope, Group: group, Limit: limit, Enabled: true, Published: true, Title: "奖励", Message: "领取", Rewards: []gamestate.Reward{{Type: 4, Num: 1}}})
		if err != nil {
			t.Fatal(err)
		}
		revision = next
		b := registry.Batches[len(registry.Batches)-1]
		if _, err = db.GenerateGiftCodes(accountstore.GiftCodeGeneration{BatchID: b.ID, ExpectedRevision: revision, RequestKey: "gift-scope-generation-" + name, Codes: []string{name + "-FIRST", name + "-SECOND"}}); err != nil {
			t.Fatal(err)
		}
		return b
	}
	create("QUOTA", "code", "", 1)
	now := time.Now()
	commit := func(offer game.GiftCodeOffer, user int) (int, error) {
		state, err := accounts.LoadState(user)
		if err != nil {
			return 0, err
		}
		account, err := game.New(state)
		if err != nil {
			return 0, err
		}
		ids, err := account.PrepareGiftPresents(offer.Batch)
		if err != nil {
			return 0, err
		}
		return accounts.CommitGiftCode(offer, account.Snapshot(state), ids, now)
	}
	offers := make([]game.GiftCodeOffer, 2)
	for i, codeText := range []string{"QUOTA-FIRST", "QUOTA-SECOND"} {
		offer, code, err := accounts.PrepareGiftCode(users[i], codeText, now)
		if err != nil || code != 0 {
			t.Fatal(code, err)
		}
		offers[i] = offer
	}
	var wg sync.WaitGroup
	codes := make([]int, 2)
	errs := make([]error, 2)
	for i := range users {
		wg.Add(1)
		go func(i int) { defer wg.Done(); codes[i], errs[i] = commit(offers[i], users[i]) }(i)
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || !((codes[0] == 0 && codes[1] == 3) || (codes[0] == 3 && codes[1] == 0)) {
		t.Fatal("global public quota raced", codes, errs)
	}
	create("SHARED-A", "group", "one-campaign", 0)
	create("SHARED-B", "group", "one-campaign", 0)
	create("PER-CODE", "code", "", 0)
	for i, step := range []struct {
		code string
		want int
	}{{"SHARED-A-FIRST", 0}, {"SHARED-B-FIRST", 6}, {"PER-CODE-FIRST", 0}, {"PER-CODE-SECOND", 0}} {
		offer, code, err := accounts.PrepareGiftCode(users[0], step.code, now.Add(time.Duration(i+1)*4*time.Second))
		if err != nil || code != step.want {
			t.Fatal(step, code, err)
		}
		if code == 0 {
			if code, err = commit(offer, users[0]); err != nil || code != 0 {
				t.Fatal(step, code, err)
			}
		}
	}
}
