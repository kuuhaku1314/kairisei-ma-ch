package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func TestEvolutionPolicyUsesDirectedEdgesAndSurvivesReload(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	catalog, err := accounts.Database().CatalogState()
	if err != nil {
		t.Fatal(err)
	}
	catalog.CardActions.EvolutionTransitions = []gamestate.EvolutionTransition{{FromCardID: 10, ToCardID: 11, Type: 1}, {FromCardID: 11, ToCardID: 10, Type: 0}}
	accounts.Database().SetCatalog(catalog)
	o, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(o.evolutionClosed) != 0 {
		t.Fatal("unconfigured evolutions are closed")
	}
	a := &API{operations: o}
	call := func(body string, want int) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPut, "/api/evolution-policy", strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Kairisei-Admin-Action", "apply")
		w := httptest.NewRecorder()
		a.saveEvolutionPolicy(w, r)
		if w.Code != want {
			t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
		}
	}
	call(`{"closed_paths":[{"from_cardid":10,"to_cardid":99}],"expected_revision":0}`, 400)
	call(`{"closed_paths":[{"from_cardid":10,"to_cardid":11}],"expected_revision":0}`, 200)
	call(`{"closed_paths":[],"expected_revision":0}`, 409)
	loaded, err := NewOperations(accounts.Database(), nil)
	if err != nil || len(loaded.evolutionClosed) != 1 || loaded.evolutionClosed[0] != (game.EvolutionPath{FromCardID: 10, ToCardID: 11}) {
		t.Fatal("directed policy did not reload", err)
	}
	call(`{"expected_revision":1}`, 400) // An incomplete request must not accidentally reopen everything.
	call(`{"closed_paths":[],"expected_revision":1}`, 200)
	loaded, err = NewOperations(accounts.Database(), nil)
	if err != nil || len(loaded.evolutionClosed) != 0 {
		t.Fatal("all-open reset did not persist", err)
	}
}

func TestAccountCreationLimitSettingsPublication(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	o, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if o.AccountCreationPolicy().DailyLimit != 10 {
		t.Fatal("new settings did not default to ten")
	}
	api := &API{operations: o}
	put := func(body string, want int) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Kairisei-Admin-Action", "apply")
		w := httptest.NewRecorder()
		api.setRuntimeSettings(w, r)
		if w.Code != want {
			t.Fatalf("settings HTTP %d: %s", w.Code, w.Body.String())
		}
	}
	for _, raw := range []string{`null`, `"10"`, `0.5`, `-1`, `10001`} {
		put(`{"crystal_purchase_enabled":false,"expected_revision":0,"account_creation_daily_limit":`+raw+`}`, 400)
		if o.AccountCreationPolicy().DailyLimit != 10 || o.runtimeSettingsRevision != 0 {
			t.Fatal("invalid config was published")
		}
	}
	put(`{"crystal_purchase_enabled":false,"expected_revision":0,"account_creation_daily_limit":0}`, 200)
	put(`{"crystal_purchase_enabled":true,"expected_revision":1}`, 200)
	if o.AccountCreationPolicy().DailyLimit != 0 {
		t.Fatal("omission reopened creation")
	}
	w := httptest.NewRecorder()
	api.runtimeSettings(w, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	var response struct {
		Settings map[string]json.RawMessage `json:"settings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || string(response.Settings["account_creation_daily_limit"]) != "0" {
		t.Fatal("explicit zero omitted from GET", w.Body.String(), err)
	}
	put(`{"crystal_purchase_enabled":false,"expected_revision":2,"account_creation_daily_limit":10000}`, 200)
	put(`{"crystal_purchase_enabled":false,"expected_revision":2,"account_creation_daily_limit":0}`, 409)
	if o.AccountCreationPolicy().DailyLimit != 10000 {
		t.Fatal("revision conflict changed policy")
	}
	// A final creation transaction may hold the writer while an Admin save holds
	// configMu. The runtime getter must still finish without that lock.
	o.configMu.Lock()
	got := make(chan int, 1)
	go func() { got <- o.AccountCreationPolicy().DailyLimit }()
	select {
	case limit := <-got:
		if limit != 10000 {
			t.Error("getter saw unpublished settings")
		}
	case <-time.After(time.Second):
		t.Error("runtime getter waited for Admin configMu")
	}
	o.configMu.Unlock()
	reloaded, err := NewOperations(accounts.Database(), nil)
	if err != nil || reloaded.AccountCreationPolicy().DailyLimit != 10000 {
		t.Fatal("limit did not reload", err)
	}
	db, _ := accounts.Database().OpenRead()
	var audits int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cn_admin_audit WHERE operation='runtime-settings'`).Scan(&audits); err != nil || audits != 3 {
		t.Fatal("settings audit/revision mismatch", audits, err)
	}
}

func TestPersistedAccountCreationLimitValidation(t *testing.T) {
	for _, test := range []struct {
		raw     string
		want    int
		invalid bool
	}{
		{`{}`, 10, false}, {`{"account_creation_daily_limit":0}`, 0, false}, {`{"account_creation_daily_limit":25}`, 25, false},
		{`{"account_creation_daily_limit":null}`, 0, true}, {`{"account_creation_daily_limit":"10"}`, 0, true},
		{`{"account_creation_daily_limit":0.5}`, 0, true}, {`{"account_creation_daily_limit":-1}`, 0, true}, {`{"account_creation_daily_limit":10001}`, 0, true},
	} {
		t.Run(test.raw, func(t *testing.T) {
			accounts := testfixture.NewFriendCapacityTestAccounts(t)
			if _, err := accounts.Database().WriteDocument(runtimeSettingsKey, 0, json.RawMessage(test.raw), "test-settings"); err != nil {
				t.Fatal(err)
			}
			o, err := NewOperations(accounts.Database(), nil)
			if test.invalid {
				if err == nil {
					t.Fatal("invalid persisted limit accepted")
				}
				return
			}
			if err != nil || o.AccountCreationPolicy().DailyLimit != test.want {
				t.Fatal("persisted limit", test.want, err)
			}
		})
	}
}

func TestCreationVerificationSettingsDefaultMergeAndValidation(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	o, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := o.AccountCreationPolicy(); got.DailyLimit != 10 || got.VerificationBits != 0 {
		t.Fatal("default creation policy", got)
	}
	api := &API{operations: o}
	put := func(body string, want int) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Kairisei-Admin-Action", "apply")
		w := httptest.NewRecorder()
		api.setRuntimeSettings(w, r)
		if w.Code != want {
			t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
		}
	}
	put(`{"crystal_purchase_enabled":false,"expected_revision":0,"account_creation_daily_limit":5,"account_creation_verification_bits":18}`, 200)
	put(`{"crystal_purchase_enabled":false,"expected_revision":1,"account_creation_daily_limit":7}`, 200)
	for _, value := range []string{`null`, `""`, `"18"`, `false`, `{}`, `-1`, `1`, `15`, `33`, `16.5`} {
		put(`{"crystal_purchase_enabled":false,"expected_revision":2,"account_creation_verification_bits":`+value+`}`, 400)
		if got := o.AccountCreationPolicy(); got.DailyLimit != 7 || got.VerificationBits != 18 {
			t.Fatal("invalid or omitted bits changed policy", got)
		}
	}
	put(`{"crystal_purchase_enabled":false,"expected_revision":2,"account_creation_daily_limit":5,"account_creation_verification_bits":19}`, 200)
	if got := o.AccountCreationPolicy(); got.DailyLimit != 5 || got.VerificationBits != 19 {
		t.Fatal("policy pair was not published", got)
	}
	put(`{"crystal_purchase_enabled":false,"expected_revision":3,"account_creation_verification_bits":0}`, 200)
	put(`{"crystal_purchase_enabled":false,"expected_revision":3,"account_creation_verification_bits":20}`, 409)
	reloaded, err := NewOperations(accounts.Database(), nil)
	if err != nil || reloaded.AccountCreationPolicy().VerificationBits != 0 {
		t.Fatal("explicit zero did not persist", err)
	}
	// Persisted settings use the same bounded integer contract; omission is off.
	for index, test := range []struct {
		raw     string
		bits    int
		invalid bool
	}{
		{`{}`, 0, false}, {`{"account_creation_verification_bits":0}`, 0, false},
		{`{"account_creation_verification_bits":16}`, 16, false}, {`{"account_creation_verification_bits":18}`, 18, false},
		{`{"account_creation_verification_bits":19}`, 19, false}, {`{"account_creation_verification_bits":20}`, 20, false},
		{`{"account_creation_verification_bits":32}`, 32, false}, {`{"account_creation_verification_bits":null}`, 0, true},
		{`{"account_creation_verification_bits":"18"}`, 0, true}, {`{"account_creation_verification_bits":15}`, 0, true},
		{`{"account_creation_verification_bits":33}`, 0, true}, {`{"account_creation_verification_bits":16.5}`, 0, true},
	} {
		if _, err := accounts.Database().WriteDocument(runtimeSettingsKey, index+4, json.RawMessage(test.raw), "test-settings"); err != nil {
			t.Fatal(err)
		}
		loaded, err := NewOperations(accounts.Database(), nil)
		if test.invalid {
			if err == nil {
				t.Fatal("invalid stored bits accepted", test.raw)
			}
			continue
		}
		if err != nil || loaded.AccountCreationPolicy().VerificationBits != test.bits {
			t.Fatal("stored bits", test.raw, err)
		}
	}
}

func TestAdminItemShopSettingsPersistOutsidePlayerSave(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	catalog, err := accounts.Database().CatalogState()
	if err != nil {
		t.Fatal(err)
	}
	catalog.ItemDefinitions = append(catalog.ItemDefinitions, gamestate.ItemDefinition{ItemID: 1000, Name: "恢复药", PictID: 10080, MaxOwned: 999})
	catalog.CollectionRewards = append(catalog.CollectionRewards, gamestate.CollectionRewardDefinition{Type: 16, ID: 123, Name: "表情"})
	accounts.Database().SetCatalog(catalog)
	operations, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	admin := &API{operations: operations}
	put := func(handler http.HandlerFunc, body string, want int) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Kairisei-Admin-Action", "apply")
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != want {
			t.Fatalf("settings HTTP %d: %s", w.Code, w.Body.String())
		}
	}
	call := func(body string, want int) { t.Helper(); put(admin.setRuntimeSettings, body, want) }
	call(`{"crystal_purchase_enabled":false,"expected_revision":0,"item_shop":[{"lineup_id":602003,"enabled":true,"price":2000},{"lineup_id":602004,"enabled":false,"price":12000}]}`, 200)
	call(`{"crystal_purchase_enabled":true,"expected_revision":0}`, 409)
	call(`{"crystal_purchase_enabled":false,"expected_revision":1,"item_shop":[{"lineup_id":992001,"enabled":false,"price":2}]}`, 400)
	if operations.runtimeSettings.TeamBattleSpeed != 0 {
		t.Fatal("default team speed does not follow the client")
	}
	call(`{"crystal_purchase_enabled":false,"expected_revision":1,"team_battle_speed":125}`, 400)
	for i, speed := range []int{150, 0, 200} {
		payload, _ := json.Marshal(map[string]any{"crystal_purchase_enabled": false, "expected_revision": i + 1, "team_battle_speed": speed})
		call(string(payload), 200)
		if speed == 0 {
			reloaded, err := NewOperations(accounts.Database(), nil)
			if err != nil || reloaded.runtimeSettings.TeamBattleSpeed != 0 {
				t.Fatalf("client speed policy lost on reload: %v", err)
			}
		}
	}
	restarted, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	admin.operations = restarted
	w := httptest.NewRecorder()
	admin.runtimeSettings(w, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	var response struct {
		Settings game.RuntimeSettings `json:"settings"`
		Revision int                  `json:"revision"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Revision != 4 || response.Settings.TeamBattleSpeed != 200 {
		t.Fatalf("settings reload: %s %v", w.Body.String(), err)
	}
	got := make(map[int]game.ItemShopSetting)
	for _, setting := range response.Settings.ItemShop {
		got[setting.LineupID] = setting
	}
	if !got[602003].Enabled || got[602003].Price != 2000 || got[602004].Enabled || got[602004].Price != 12000 {
		t.Fatalf("operator settings lost after restart: %+v", got)
	}
	call(`{"crystal_purchase_enabled":false,"expected_revision":4,"item_shop":[{"lineup_id":70000001,"enabled":true,"price":10,"item_id":1000,"buy_type":1,"name":"恢复药","quantity":2,"pay_type":1,"buy_num_max":5,"total_limit":20,"period":"week","period_limit":3},{"lineup_id":70000002,"enabled":true,"price":50,"item_id":123,"buy_type":2,"name":"表情","tab_type":2,"quantity":1,"pay_type":3,"buy_num_max":1}]}`, 200)
	call(`{"crystal_purchase_enabled":false,"expected_revision":5,"item_shop":[{"lineup_id":70000001,"enabled":true,"price":10,"item_id":123,"buy_type":2,"name":"替换","tab_type":2,"quantity":1,"pay_type":1,"buy_num_max":1}]}`, 400)
	// An older admin page omitting the new products cannot erase their IDs.
	call(`{"crystal_purchase_enabled":false,"expected_revision":5,"item_shop":[]}`, 200)
	restarted, err = NewOperations(accounts.Database(), nil)
	if err != nil || len(restarted.runtimeSettings.ItemShop) != 2 || restarted.runtimeSettings.ItemShop[0].Period != "week" {
		t.Fatal("new products did not survive restart", err)
	}
	// The shop page saves only the shop; a missing list must not read as "remove everything".
	put(admin.setItemShop, `{"expected_revision":6}`, 400)
	put(admin.setItemShop, `{"expected_revision":6,"item_shop":[{"lineup_id":602003,"enabled":false,"price":3000}]}`, 200)
	if saved := admin.operations.runtimeSettings; saved.TeamBattleSpeed != 200 || len(saved.ItemShop) != 3 || saved.ItemShop[0].Price != 3000 {
		t.Fatalf("item shop save changed other settings or dropped products: %+v", saved)
	}
	uid := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 77)
	state, err := accounts.LoadState(uid)
	if err != nil {
		t.Fatal(err)
	}
	state.ItemShopPurchases = map[int]int{70000001: 7}
	state.ItemShopPeriods = map[int]gamestate.ItemShopPeriodCounts{70000001: {Day: "2026-09-30", DayCount: 2, Week: "2026-09-28", WeekCount: 3, Month: "2026-09", MonthCount: 7}}
	state.User.BP = state.User.BPMax + 30
	state.BattlePoint.NextRecoveryUnix = 0
	if err := accounts.PersistState(uid, state); err != nil {
		t.Fatal("persist shop counters and overflow BP", err)
	}
	loaded, err := accounts.LoadState(uid)
	if err != nil || loaded.ItemShopPurchases[70000001] != 7 || loaded.ItemShopPeriods[70000001].WeekCount != 3 || loaded.User.BP != state.User.BP {
		t.Fatal("player progress lost on reload", err)
	}
}
