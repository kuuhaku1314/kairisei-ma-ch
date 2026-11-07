package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accounthttp"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

func TestAdminMailDeleteAtomicRetryAndNoReward(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	identity, err := accounts.ResolveInternalLogin("00000000-0000-4000-8000-00000000a502")
	if err != nil {
		t.Fatal(err)
	}
	policy, err := masterdata.LoadPlayerProgressionRuntimeMaster(filepath.Join("..", "..", "config", "cn602-player-progression-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	admin := &API{accounts: accounts, business: accounthttp.New(accounthttp.Config{}), progression: policy,
		catalogByKey: map[string]AdminCatalogEntry{adminCatalogKey(4, 0): {Kind: "gold", RewardType: 4, Name: "金币"}}}
	userID := identity.UserID
	state, err := admin.loadAccountState(userID)
	if err != nil {
		t.Fatal(err)
	}
	const key = "mail-delete-retry-502"
	id := adminMailPresentID(userID, key)
	state.Engagement.Presents = []gamestate.Present{
		{PresentID: id, Title: "误发", Comment: "测试", Reward: gamestate.Reward{Type: 4, Num: 7}, AdminIdempotencyKey: key},
		{PresentID: 9007199254740993, Title: "已领取", State: 1, Reward: gamestate.Reward{Type: 4, Num: 5}},
		{PresentID: 42, Title: "保留", Reward: gamestate.Reward{Type: 4, Num: 3}},
	}
	if err := accounts.PersistState(userID, state); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(state)
	router := chi.NewRouter()
	router.Get("/{userID}/mail", admin.listAccountMail)
	router.Post("/{userID}/mail/delete", admin.deleteAccountMail)
	router.Post("/{userID}/mail", admin.sendAccountMail)
	call := func(method, path, body string, confirmed bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, fmt.Sprintf("http://localhost/%d%s", userID, path), strings.NewReader(body))
		request.RemoteAddr = "127.0.0.1:12345"
		request.Header.Set("Content-Type", "application/json")
		if confirmed {
			request.Header.Set("X-Kairisei-Admin-Action", "apply")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	listed := call(http.MethodGet, "/mail?q=已领取", "", false)
	if listed.Code != 200 || !strings.Contains(listed.Body.String(), `"present_id":"9007199254740993"`) {
		t.Fatalf("mail ID lost precision: %s", listed.Body.String())
	}
	body := fmt.Sprintf(`{"present_ids":["%d","9007199254740993","999"]}`, id)
	if got := call(http.MethodPost, "/mail/delete", body, false); got.Code != http.StatusForbidden {
		t.Fatal("mutation without confirmation accepted")
	}
	db, err := accounts.Database().Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TRIGGER reject_delete_audit BEFORE INSERT ON cn_admin_audit BEGIN SELECT RAISE(ABORT, 'test audit failure'); END"); err != nil {
		t.Fatal(err)
	}
	failed := call(http.MethodPost, "/mail/delete", body, true)
	after, err := accounts.LoadState(userID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(after)
	if failed.Code != 500 || string(raw) != string(before) {
		t.Fatalf("audit failure changed mail: status=%d", failed.Code)
	}
	if _, err = db.Exec("DROP TRIGGER reject_delete_audit"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	responses := make([]*httptest.ResponseRecorder, 2)
	for i := range responses {
		wg.Add(1)
		go func() { defer wg.Done(); responses[i] = call(http.MethodPost, "/mail/delete", body, true) }()
	}
	wg.Wait()
	for _, r := range responses {
		if r.Code != 200 {
			t.Fatalf("delete retry: %s", r.Body.String())
		}
	}
	after, err = accounts.LoadState(userID)
	if err != nil {
		t.Fatal(err)
	}
	if after.User.Gold != state.User.Gold || len(after.Engagement.Presents) != 1 || after.Engagement.Presents[0].PresentID != 42 {
		t.Fatal("deleted mail granted/reclaimed rewards or removed unrelated mail")
	}
	count := 0
	for _, p := range after.Engagement.Histories {
		if p.State == gamestate.PresentStateAdminDeleted {
			count++
		}
	}
	if count != 2 {
		t.Fatal("deletion tombstones lost")
	}
	var audits int
	if err = db.QueryRow("SELECT count(*) FROM cn_admin_audit WHERE operation='account-mail-delete'").Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("retry created %d audit records: %v", audits, err)
	}
	resent := call(http.MethodPost, "/mail", `{"idempotency_key":"`+key+`","title":"误发","message":"测试","reward_type":4,"quantity":7}`, true)
	if resent.Code != 200 || !strings.Contains(resent.Body.String(), `"duplicate":true`) {
		t.Fatalf("old send retry recreated withdrawn mail: %s", resent.Body.String())
	}
	after, _ = accounts.LoadState(userID)
	if len(after.Engagement.Presents) != 1 {
		t.Fatal("withdrawn mail reappeared")
	}
}
