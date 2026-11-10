package admin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

func TestAdminMissingBossImageUsesTextFallback(t *testing.T) {
	root := t.TempDir()
	url := "/assets/boss/60600204.webp"
	if image, err := adminOptionalImageURL(root, url); err != nil || image != "" {
		t.Fatalf("missing decorative image blocks the server: %q %v", image, err)
	}
	if err := os.Mkdir(filepath.Join(root, "boss"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "boss", "60600204.webp"), []byte("image"), 0600); err != nil {
		t.Fatal(err)
	}
	if image, err := adminOptionalImageURL(root, url); err != nil || image != url {
		t.Fatalf("existing image was hidden: %q %v", image, err)
	}
	if _, err := adminOptionalImageURL(root, "/assets/../../outside.webp"); err == nil {
		t.Fatal("unsafe image path accepted as a missing thumbnail")
	}
}

func TestAdminAccountListDoesNotDecodeSnapshots(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	identity, err := accounts.ResolveInternalLogin("00000000-0000-4000-8000-00000000a001")
	if err != nil {
		t.Fatal(err)
	}
	database, err := accounts.Database().Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`UPDATE cn_account_snapshot SET payload_json = ? WHERE user_id = ?`,
		[]byte("not-json"),
		identity.UserID,
	); err != nil {
		t.Fatal(err)
	}

	admin := &API{accounts: accounts}
	items, err := admin.loadAccounts()
	if err != nil {
		t.Fatalf("lightweight account list decoded a snapshot: %v", err)
	}
	found := false
	for _, item := range items {
		if item.UserID == identity.UserID {
			found = true
			if item.LoginUUID != identity.LoginUUID || item.Revision != 1 {
				t.Fatalf("unexpected lightweight account row: %+v", item)
			}
		}
	}
	if !found {
		t.Fatalf("account %d missing from lightweight list", identity.UserID)
	}
	count, err := admin.accounts.Count()
	if err != nil {
		t.Fatal(err)
	}
	if count != len(items) {
		t.Fatalf("account count = %d, list length = %d", count, len(items))
	}
	guest, err := accounts.ResolveInternalLogin("00000000-0000-4000-8000-00000000a002")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.BindInternalAccount(identity.LoginUUID, "operations_test", "temporary-test-password"); err != nil {
		t.Fatal(err)
	}
	for _, filter := range []accountstore.AccountFilter{
		{Binding: "bound", Search: "operations test", CreatedAfter: "2000-01-01T00:00:00Z", LoginBefore: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)},
		{ActiveOnly: true, ActiveIDs: []int{identity.UserID}},
	} {
		rows, total, err := accounts.QueryFilteredAccounts(filter, 1, 0)
		if err != nil || total != 1 || len(rows) != 1 || rows[0].UserID != identity.UserID || rows[0].Level <= 0 {
			t.Fatalf("filtered projection: %v %v %d", rows, err, total)
		}
		rows, total, err = accounts.QueryFilteredAccounts(filter, 1, 1)
		if err != nil || total != 1 || len(rows) != 0 {
			t.Fatal("filter pagination mismatch", err)
		}
	}
	rows, total, err := accounts.QueryFilteredAccounts(accountstore.AccountFilter{Binding: "guest", Search: guest.LoginUUID}, 10, 0)
	if err != nil || total != 1 || rows[0].Username != "" {
		t.Fatal("guest binding filter", err)
	}
	bound, _, err := accounts.QueryFilteredAccounts(accountstore.AccountFilter{Binding: "bound"}, 1, 0)
	if err != nil || len(bound) != 1 {
		t.Fatal("bound account lookup", err)
	}
	for _, c := range []struct {
		filter accountstore.AccountFilter
		want   int
	}{
		{accountstore.AccountFilter{Binding: "bound", LevelMin: bound[0].Level, LevelMax: bound[0].Level, ArthurType: bound[0].ArthurType, Sort: "level"}, 1},
		{accountstore.AccountFilter{Binding: "bound", LevelMin: bound[0].Level + 1}, 0},
	} {
		if rows, total, err := accounts.QueryFilteredAccounts(c.filter, 10, 0); err != nil || total != c.want || len(rows) != c.want {
			t.Fatalf("level/job filter %+v: %d rows, total %d, %v", c.filter, len(rows), total, err)
		}
	}
	rows, total, err = accounts.QueryFilteredAccounts(accountstore.AccountFilter{ActiveOnly: true}, 10, 0)
	if err != nil || total != 0 || len(rows) != 0 {
		t.Fatal("empty online set returned players", err)
	}
	summary, err := accounts.AccountSummary()
	if err != nil || summary.Bound != 1 || summary.Players != summary.Bound+summary.Guests || summary.Players+summary.System != count+1 {
		t.Fatal("account type counts disagree", summary, err)
	}
}

func TestApplyAdminCatalogAssetCoverage(t *testing.T) {
	root := t.TempDir()
	catalog := []AdminCatalogEntry{
		{Kind: "currency", RewardType: 4, Name: "gold"},
		{Kind: "card", RewardType: 6, RewardTypeID: 1, Name: "card one", ImageURL: "/assets/card/1.webp"},
		{Kind: "card", RewardType: 6, RewardTypeID: 2, Name: "card source gap", ImageURL: "/assets/card/2.webp"},
		{Kind: "item", RewardType: 8, RewardTypeID: 3, Name: "item", ImageURL: "/assets/item/3.webp"},
		{Kind: "material", RewardType: 13, RewardTypeID: 4, Name: "material", ImageURL: "/assets/card/4.webp"},
		{Kind: "sphere", RewardType: 15, RewardTypeID: 5, Name: "sphere", ImageURL: "/assets/sphere/5.webp"},
		{Kind: "buddy", RewardType: 19, RewardTypeID: 6, Name: "buddy", ImageURL: "/assets/buddy/6.webp"},
	}
	for _, relative := range []string{"currency/4.webp", "card/1.webp", "item/3.webp", "card/4.webp", "sphere/5.webp", "buddy/6.webp"} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("webp"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := adminAssetManifest{
		SchemaVersion: 2, ClientProfile: "cn602-bootstrap",
		CatalogImageCoverage: map[string]adminCatalogImageCoverage{
			"card":     {SourceGapIDs: []int{2}},
			"item":     {},
			"material": {},
			"sphere":   {},
			"buddy":    {},
		},
	}
	content, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	publishable, err := applyAdminCatalogAssetCoverage(catalog, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(publishable) != len(catalog)-1 {
		t.Fatalf("publishable catalog count = %d, want %d", len(publishable), len(catalog)-1)
	}
	for _, entry := range publishable {
		if entry.Kind == "currency" && entry.ImageURL != "/assets/currency/4.webp" {
			t.Fatal("currency image was not projected from the resource set")
		}
		if entry.RewardTypeID == 2 {
			t.Fatal("source-gap card remains grantable")
		}
		if entry.Kind != "currency" && entry.ImageURL == "" {
			t.Fatalf("publishable %s/%d has no image", entry.Kind, entry.RewardTypeID)
		}
	}
	if err := os.Remove(filepath.Join(root, "buddy", "6.webp")); err != nil {
		t.Fatal(err)
	}
	if _, err := applyAdminCatalogAssetCoverage(catalog, root); err == nil {
		t.Fatal("missing resolved Buddy output passed the Admin asset gate")
	}
}

func TestAdminMailCardQuantityAcceptsUpToOneHundred(t *testing.T) {
	entry := AdminCatalogEntry{
		Kind: "card", RewardType: 6, RewardTypeID: 1001, Name: "test card",
		LevelMax: 60, FameMax: 90, LoveMax: 100,
	}
	admin := &API{catalogByKey: map[string]AdminCatalogEntry{
		adminCatalogKey(entry.RewardType, entry.RewardTypeID): entry,
	}}
	request := AdminMailRequest{
		RewardType: 6, RewardTypeID: entry.RewardTypeID,
		CardLevel: 1, CardFame: 1,
	}
	for _, quantity := range []int{1, adminMaximumInstanceRewardQuantity} {
		request.Quantity = quantity
		reward, _, err := admin.mailReward(request)
		if err != nil {
			t.Fatalf("card quantity %d was rejected: %v", quantity, err)
		}
		if reward.Num != quantity {
			t.Fatalf("card quantity = %d, want %d", reward.Num, quantity)
		}
	}
	request.Quantity = adminMaximumInstanceRewardQuantity + 1
	if _, _, err := admin.mailReward(request); err == nil {
		t.Fatal("card quantity above 100 was accepted")
	}
}

func TestApplyAdminTargetLevelRaisesCapsWithoutTouchingInventory(t *testing.T) {
	policy, err := masterdata.LoadPlayerProgressionRuntimeMaster(filepath.Join("..", "..", "config", "cn602-player-progression-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	state := gamestate.State{
		User:  gamestate.User{Level: 1, AP: 3, APMax: 20, BP: 20, BPMax: 20},
		Cards: []gamestate.Card{{UniqueID: 1, CardID: 1001, Level: 1}},
	}
	if err := applyAdminTargetLevel(&state, policy, 50); err != nil {
		t.Fatal(err)
	}
	if state.User.Level != 50 || state.User.BP != 45 || state.User.BPMax != 45 ||
		state.User.AP != state.User.APMax || state.User.Experience != policy.CumulativeExperience(50) ||
		state.User.NowLevelExperience != 0 || state.User.NextLevelExperience != policy.ExperienceRequired(50) {
		t.Fatalf("unexpected level grant result: %+v", state.User)
	}
	if len(state.Cards) != 1 || state.Cards[0].CardID != 1001 || state.Cards[0].Level != 1 {
		t.Fatalf("level grant changed inventory: %+v", state.Cards)
	}
	if err := applyAdminTargetLevel(&state, policy, 49); err == nil {
		t.Fatal("level grant allowed an account downgrade")
	}
}

func TestApplyAdminTargetArthurRankOnlyRaisesHighWaterMark(t *testing.T) {
	state := gamestate.State{
		User:  gamestate.User{ArthurRank: 1},
		Cards: []gamestate.Card{{UniqueID: 1, CardID: 1001, Level: 1}},
		Decks: []gamestate.Deck{{ArthurType: 1, Index: 1, CardUniqueIDs: []int64{1}}},
	}
	if err := applyAdminTargetArthurRank(&state, 10); err != nil {
		t.Fatal(err)
	}
	if state.User.ArthurRank != 10 || len(state.Cards) != 1 || state.Cards[0].Level != 1 ||
		len(state.Decks) != 1 || len(state.Decks[0].CardUniqueIDs) != 1 || state.Decks[0].CardUniqueIDs[0] != 1 {
		t.Fatalf("Arthur-rank grant changed unrelated account state: %+v", state)
	}
	if err := applyAdminTargetArthurRank(&state, 9); err == nil {
		t.Fatal("Arthur-rank grant allowed an account downgrade")
	}
	if err := applyAdminTargetArthurRank(&state, adminMaximumArthurRank+1); err == nil {
		t.Fatal("Arthur-rank grant allowed a value outside the CN client enum")
	}
}
