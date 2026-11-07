package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/protocol"
	"kairisei.local/server/internal/testfixture"
)

func TestAdminLoginAccessPublicationAndFilter(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	id := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 11)
	o, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	a := &API{accounts: accounts, operations: o}
	router := chi.NewRouter()
	router.Get("/api/accounts", a.accountList)
	router.Put("/api/accounts/{userID}/login-block", a.changeLoginBlock)
	router.Delete("/api/accounts/{userID}/login-block", a.changeLoginBlock)
	router.Put("/api/settings/ip-blacklist", a.setIPBlacklist)
	router.Get("/api/settings/login-whitelist", a.loginWhitelist)
	router.Put("/api/settings/login-whitelist", a.setLoginWhitelist)
	router.Put("/api/settings/module-switches", a.setModuleSwitches)
	call := func(method, path string, body any, status int) []byte {
		t.Helper()
		return testfixture.CallContentAdmin(t, router, method, path, body, status)
	}
	blockPath := fmt.Sprintf("/api/accounts/%d/login-block", id)
	now := time.Now().Unix()
	call(http.MethodPut, blockPath, nil, 400)
	call(http.MethodPut, blockPath, map[string]any{"until_unix": now - 1}, 400)
	call(http.MethodPut, blockPath, map[string]any{"until_unix": now + 600}, 200)
	list := func(filter string) []accountstore.AccountListItem {
		t.Helper()
		raw := call(http.MethodGet, "/api/accounts?login_block="+filter+"&limit=1", nil, 200)
		var result struct {
			Accounts []accountstore.AccountListItem `json:"accounts"`
			Total    int                            `json:"total"`
		}
		if err := json.Unmarshal(raw, &result); err != nil || result.Total != len(result.Accounts) {
			t.Fatalf("filtered count differs from rows: %s (%v)", raw, err)
		}
		return result.Accounts
	}
	rows := list("blocked")
	if len(rows) != 1 || rows[0].UserID != id || !rows[0].LoginBlock.Active || rows[0].LoginBlock.RemainingSeconds <= 0 {
		t.Fatalf("timed block missing from list: %+v", rows)
	}
	// Set the fixture's clock boundary directly, without sleeping.
	db, _ := accounts.Database().Open()
	if _, err := db.Exec(`UPDATE cn_local_account SET login_block_until_unix=? WHERE user_id=?`, now-1, id); err != nil {
		t.Fatal(err)
	}
	if len(list("blocked")) != 0 || len(list("unblocked")) != 1 {
		t.Fatal("expired block was treated as active")
	}
	call(http.MethodDelete, blockPath, nil, 200)
	if rows := list("unblocked"); len(rows) != 1 || rows[0].LoginBlock.UntilUnix != nil {
		t.Fatalf("recovery did not clear the deadline: %+v", rows)
	}
	ipPath := "/api/settings/ip-blacklist"
	call(http.MethodPut, ipPath, map[string]any{"entries": []string{"bad"}, "expected_revision": 0}, 400)
	call(http.MethodPut, ipPath, map[string]any{"entries": []string{"192.0.2.21/24", "::ffff:192.0.2.0/120"}, "expected_revision": 0}, 200)
	call(http.MethodPut, ipPath, map[string]any{"entries": []string{}, "expected_revision": 0}, 409)
	whitelistPath := "/api/settings/login-whitelist"
	var initial loginWhitelistConfig
	if err := json.Unmarshal(call(http.MethodGet, whitelistPath, nil, 200), &initial); err != nil || initial.Enabled || initial.UserIDs == nil {
		t.Fatalf("default whitelist %+v %v", initial, err)
	}
	call(http.MethodPut, whitelistPath, map[string]any{"enabled": true, "user_ids": []int{id + 1}, "expected_revision": 0}, 400)
	call(http.MethodPut, whitelistPath, map[string]any{"enabled": true, "user_ids": []int{id, id}, "expected_revision": 0}, 200)
	modulePath := "/api/settings/module-switches"
	call(http.MethodPut, modulePath, map[string]any{"disabled": []string{"recommend_deck"}, "expected_revision": 0}, 400)
	call(http.MethodPut, modulePath, map[string]any{"disabled": []string{"pvp"}, "expected_revision": 0}, 200)
	call(http.MethodPut, modulePath, map[string]any{"disabled": []string{}, "expected_revision": 0}, 409)
	reopened, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	policy := reopened.AccountCreationPolicy().LoginWhitelist
	if !reopened.IPBlocked("192.0.2.55:1234") || reopened.IPBlocked("198.51.100.55:1234") ||
		!policy.Enabled() || !policy.Allows(id) || policy.Allows(id+1) || len(policy.UserIDs()) != 1 ||
		reopened.ModuleSwitches() != protocol.DefaultModuleSwitches()&^protocol.ModulePVP {
		t.Fatal("access rules did not reload from committed configuration")
	}
}
