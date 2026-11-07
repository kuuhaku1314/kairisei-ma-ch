package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"kairisei.local/server/internal/accountstore"
	adminapi "kairisei.local/server/internal/admin"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

// Uses the existing isolated production-resource gate, never a player's DB.
func probeCompleteRuntimePurchase(t *testing.T, handler http.Handler, savePath, seedPath string, cards masterdata.CardRuntimeMaster, output string) {
	t.Helper()
	if !filepath.IsAbs(output) {
		t.Fatal("absolute probe output required")
	}
	if err := os.Mkdir(output, 0700); err != nil {
		t.Fatal(err)
	}
	storage, err := accountstore.OpenDatabase(savePath, seedPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := storage.Close(); err != nil {
			t.Error(err)
		}
	})
	accounts := testfixture.TestAccountRepository(t, storage)
	testfixture.AttachProbeCardCatalog(t, storage, cards)
	if _, err := accounts.ResolveInternalLogin("00000000-0000-0000-0004-000000000000"); err != nil {
		t.Fatal(err)
	}
	identity, err := accounts.ResolveInternalLogin("00000000-0000-0000-0004-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	state, err := accounts.LoadState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	state.Onboarding.Step = masterdata.OnboardingStepCount
	state.User.Coin, state.User.CoinFree, state.User.Gold = 0, 0, 0
	state.User.CardMax = 100
	if err := accounts.PersistState(identity.UserID, state); err != nil {
		t.Fatal(err)
	}
	call := func(route, payload, name string, code int) map[string]json.RawMessage {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, route, strings.NewReader(identity.SessionKey+payload)))
		if err := os.WriteFile(filepath.Join(output, name+".jsonl"), response.Body.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		lines := bytes.Split(response.Body.Bytes(), []byte{'\n'})
		var common struct {
			Code    int    `json:"res_code"`
			Message string `json:"res_str"`
			Action  int    `json:"res_err_action"`
			Delete  int    `json:"res_is_del_savedata"`
		}
		var method map[string]json.RawMessage
		if response.Code != 200 || len(lines) != 3 || json.Unmarshal(lines[0], &common) != nil || json.Unmarshal(lines[1], &method) != nil || common.Code != code {
			t.Fatalf("%s: HTTP %d %.1000s", name, response.Code, response.Body.String())
		}
		if code < 0 && (common.Action != 2 || common.Delete != 0 || common.Message == "") {
			t.Fatal("balance failure must display a business error without clearing login")
		}
		return method
	}
	call("/HomeShow", "", "home-before", 0)
	before, err := accounts.LoadState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		route, payload, name string
		code                 int
	}{
		{"/BuyNavi", `{"navi_id":18}`, "navi-poor", -1060},
		{"/CoinUse", `{"type":2,"param":0}`, "expansion-poor", -1060},
		{"/CoinUse", `{"type":10,"param":0}`, "warehouse-expansion-poor", -1060},
		{"/ItemShopBuy", `{"item_shop_lineupid":992001,"buy_num":1,"popupid":0}`, "coin-shop-poor", -1060},
		{"/ItemShopBuy", `{"item_shop_lineupid":602001,"buy_num":1,"popupid":0}`, "gold-shop-poor", -1030},
		{"/GachaPlay2", `{"0":60200011,"1":3,"2":"probe","3":[],"4":0}`, "gacha-poor", -1060},
	} {
		call(c.route, c.payload, c.name, c.code)
	}
	after, err := accounts.LoadState(identity.UserID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("rejected purchase changed the account: %v", err)
	}
	call("/HomeShow", "", "home-after", 0)
	full := call("/TeamBattleSoloShow", `{"0":1}`, "quests-all", 0)
	operations, err := adminapi.NewOperations(storage, nil)
	if err != nil {
		t.Fatal(err)
	}
	revision := 0
	if _, err := operations.SetTeamBattlePublication(adminapi.TeamBattlePublication{Mode: "allowlist", GroupIDs: []int{700300201}, ExpectedRevision: &revision}); err != nil {
		t.Fatal(err)
	}
	limited := call("/TeamBattleSoloShow", `{"0":1}`, "quests-ice-only", 0)
	if !bytes.Equal(full["9"], limited["9"]) || len(limited["9"]) < 10 {
		t.Fatal("BOSS publication removed normal quest progression")
	}
	for _, field := range []string{"10", "11", "12"} {
		var groups []struct {
			ID int `json:"0"`
		}
		if json.Unmarshal(limited[field], &groups) != nil {
			t.Fatal("invalid group list")
		}
		for _, group := range groups {
			if group.ID != 700300201 {
				t.Fatalf("unpublished activity %d remained", group.ID)
			}
		}
	}
	probeCompleteRuntimeAdmin(t, handler, accounts, identity.UserID, output)
	admin := handler.(interface{ AdminHandler() http.Handler }).AdminHandler()
	settings := func(method string, enabled bool, revision, want int) {
		t.Helper()
		body := fmt.Sprintf(`{"crystal_purchase_enabled":%t,"expected_revision":%d}`, enabled, revision)
		r := httptest.NewRequest(method, "http://127.0.0.1:39996/api/settings", strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:39997"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Kairisei-Admin-Action", "apply")
		w := httptest.NewRecorder()
		admin.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("settings HTTP %d: %s", w.Code, w.Body.String())
		}
		if want == 200 {
			var data struct {
				Settings struct {
					Enabled bool `json:"crystal_purchase_enabled"`
				}
				Revision int
			}
			if json.Unmarshal(w.Body.Bytes(), &data) != nil || data.Settings.Enabled != enabled {
				t.Fatal("wrong crystal purchase setting")
			}
		}
	}
	buy := func(index, added, balance int) {
		t.Helper()
		result := call("/QueryOrder", fmt.Sprintf(`{"order":"local:8:%032x"}`, index), fmt.Sprintf("purchase-switch-%d-%d", index, balance), 0)
		if string(result["code"]) != "1" || string(result["addcoin"]) != fmt.Sprint(added) || string(result["coin_free"]) != fmt.Sprint(balance) {
			t.Fatalf("purchase result: %v", result)
		}
	}
	settings("GET", false, 0, 200)
	buy(1, 0, 0)
	settings("PUT", true, 0, 200)
	buy(1, 0, 0) // opening the switch must not backfill a completed order
	buy(2, 6480, 6480)
	settings("PUT", false, 0, 409)
	settings("PUT", false, 1, 200)
	buy(3, 0, 6480)
	for _, kind := range []int{2, 10} {
		base := 100
		if kind == 10 {
			base = 3000
		}
		for option, extra := range []int{5, 30, 80} {
			result := call("/CoinUse", fmt.Sprintf(`{"type":%d,"param":%d}`, kind, option), fmt.Sprintf("capacity-%d-%d", kind, option), 0)
			var user map[string]json.RawMessage
			if err := json.Unmarshal(result["user"], &user); err != nil {
				t.Fatal(err)
			}
			field := "card_max"
			if kind == 10 {
				field = "card_container_max"
			}
			if string(user[field]) != fmt.Sprint(base+extra) {
				t.Fatalf("capacity response %s=%s", field, user[field])
			}
		}
	}
	expanded, err := accounts.LoadState(identity.UserID)
	if err != nil || expanded.User.CardMax != 180 || expanded.User.CardContainerMax != 3080 || expanded.User.CoinFree != 5200 {
		t.Fatalf("expansion persistence: cards=%d container=%d free=%d err=%v", expanded.User.CardMax, expanded.User.CardContainerMax, expanded.User.CoinFree, err)
	}

}

func probeCompleteRuntimeAdmin(t *testing.T, handler http.Handler, accounts *accountstore.Accounts, userID int, output string) {
	t.Helper()
	provider, ok := handler.(interface{ AdminHandler() http.Handler })
	if !ok || provider.AdminHandler() == nil {
		t.Fatal("production Admin handler absent")
	}
	admin := provider.AdminHandler()
	call := func(method, route string, body any, name string, want int) []byte {
		t.Helper()
		content, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, "http://127.0.0.1:39996"+route, bytes.NewReader(content))
		r.RemoteAddr = "127.0.0.1:39997"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Kairisei-Admin-Action", "apply")
		w := httptest.NewRecorder()
		admin.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s HTTP %d: %.1000s", name, w.Code, w.Body.String())
		}
		if err := os.WriteFile(filepath.Join(output, "admin-"+name+".json"), w.Body.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		return w.Body.Bytes()
	}
	var list struct {
		Accounts []accountstore.AccountListItem `json:"accounts"`
		Total    int                            `json:"total"`
	}
	if json.Unmarshal(call("GET", fmt.Sprintf("/api/accounts?q=%d&limit=1", userID), nil, "accounts", 200), &list) != nil || list.Total != 1 || len(list.Accounts) != 1 || list.Accounts[0].UserID != userID {
		t.Fatal("Admin account paging/search failed")
	}
	call("POST", "/api/accounts/resolve", map[string]any{"user_ids": []int{userID}}, "resolve-players", 200)
	var catalog struct {
		Entries []adminapi.AdminCatalogEntry `json:"entries"`
	}
	if json.Unmarshal(call("GET", "/api/catalog?kind=card&limit=120", nil, "cards", 200), &catalog) != nil {
		t.Fatal("Admin catalog invalid")
	}
	cardID := 0
	for _, card := range catalog.Entries {
		if card.ResourceState != "unavailable" {
			cardID = card.RewardTypeID
			break
		}
	}
	if cardID == 0 {
		t.Fatal("no deliverable card in production catalog")
	}
	batch := adminapi.AdminMailBatch{ID: "d466-production-batch", Title: "离线接口验证", Message: "仅隔离测试账号", UserIDs: []int{userID}, Rewards: []adminapi.AdminMailRequest{{RewardType: 6, RewardTypeID: cardID, Quantity: 1}, {RewardType: 10, Quantity: 50}}}
	call("POST", "/api/catalog/resolve", map[string]any{"rewards": batch.Rewards}, "resolve-rewards", 200)
	before, err := accounts.LoadState(userID)
	if err != nil {
		t.Fatal(err)
	}
	call("POST", "/api/mail-batches/preview", batch, "preview", 200)
	call("POST", "/api/mail-batches", batch, "create", 200)
	call("POST", "/api/mail-batches", batch, "create-retry", 200)
	batch.Title = "不能覆盖原批次"
	call("POST", "/api/mail-batches", batch, "create-conflict", 409)
	for i := 0; i < 2; i++ {
		var run struct {
			Results []struct {
				OK bool `json:"ok"`
			} `json:"results"`
		}
		if json.Unmarshal(call("POST", "/api/mail-batches/"+batch.ID+"/run", map[string]any{"user_ids": batch.UserIDs}, fmt.Sprintf("run-%d", i), 200), &run) != nil || len(run.Results) != 1 || !run.Results[0].OK {
			t.Fatal("production batch failed to deliver")
		}
	}
	after, err := accounts.LoadState(userID)
	if err != nil || len(after.Engagement.Presents) != len(before.Engagement.Presents)+2 {
		t.Fatal("production batch did not issue exactly two gifts once")
	}
	var status struct{ Done, Pending []int }
	if json.Unmarshal(call("GET", "/api/mail-batches/"+batch.ID, nil, "status", 200), &status) != nil || len(status.Done) != 1 || len(status.Pending) != 0 {
		t.Fatal("batch status lost completion")
	}
	call("GET", "/api/mail-batches?limit=1", nil, "batches", 200)
	call("GET", fmt.Sprintf("/api/accounts/%d", userID), nil, "detail", 200)
	call("GET", "/api/audit?operation=mail-delivery&limit=1", nil, "audit", 200)
}
