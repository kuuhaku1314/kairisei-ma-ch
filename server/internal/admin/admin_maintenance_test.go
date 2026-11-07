package admin

import (
	"context"
	"encoding/json"
	operationservice "kairisei.local/server/internal/operations"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/accounthttp"
	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/testfixture"
)

func TestMaintenanceAdmissionPersistenceAndCleanup(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	catalog, err := accounts.Database().CatalogState()
	if err != nil {
		t.Fatal(err)
	}
	o, err := NewOperations(accounts.Database(), catalog.Gachas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(o.CloseMaintenance)
	o.content = &contentStore{base: catalog, shops: map[int]exchangeShop{}}
	a := &API{accounts: accounts, operations: o, business: accounthttp.New(accounthttp.Config{}), multiplayerHub: multiplayer.NewHub()}
	call := func(fn http.HandlerFunc, body string, want int) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/api/maintenance", strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Kairisei-Admin-Action", "apply")
		w := httptest.NewRecorder()
		fn(w, r)
		if w.Code != want {
			t.Fatalf("HTTP %d want %d: %s", w.Code, want, w.Body.String())
		}
	}
	call(a.cleanupOperationalState, `{"mode":"preview"}`, 409)
	done, ok := o.AdmitGameRequest()
	if !ok {
		t.Fatal("normal request rejected")
	}
	call(a.setMaintenance, `{"enabled":true,"expected_revision":0}`, 200)
	if _, ok = o.AdmitGameRequest(); ok {
		t.Fatal("new game request admitted")
	}
	call(a.cleanupOperationalState, `{"mode":"preview"}`, 409)
	done()
	reloaded, err := NewOperations(accounts.Database(), catalog.Gachas)
	if err != nil || !reloaded.MaintenanceState().Enabled {
		t.Fatal("maintenance lost on restart", err)
	}
	release := make(chan struct{})
	err = o.RunCleanup(operationservice.CleanupJob{Scope: "test"}, func(ctx context.Context, _ *operationservice.CleanupJob) error {
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	blocked := a.maintenanceWrites(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("write passed cleanup barrier") }))
	w := httptest.NewRecorder()
	blocked.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/settings", nil))
	if w.Code != 409 {
		t.Fatal("admin mutation not blocked")
	}
	call(a.setMaintenance, `{"enabled":false,"expected_revision":1}`, 409)
	close(release)
	waitMaintenanceCleanup(t, o)
	// Exercise the asynchronous route without touching a running server or player.
	call(a.cleanupOperationalState, `{"mode":"preview"}`, 202)
	waitMaintenanceCleanup(t, o)
	state := o.MaintenanceState()
	if state.Busy || state.Cleanup.State != "complete" || state.Cleanup.Report.Scanned == 0 {
		t.Fatalf("preview job: %+v", state.Cleanup)
	}
	encoded, _ := json.Marshal(map[string]any{"mode": "apply", "digest": state.Cleanup.Report.Digest})
	call(a.cleanupOperationalState, string(encoded), 202)
	waitMaintenanceCleanup(t, o)
	if state = o.MaintenanceState(); state.Cleanup.State != "complete" || !state.Cleanup.Report.Applied {
		t.Fatalf("apply job: %+v", state.Cleanup)
	}
	call(a.setMaintenance, `{"enabled":false,"expected_revision":1}`, 200)
	done, ok = o.AdmitGameRequest()
	if !ok {
		t.Fatal("maintenance did not end")
	}
	done()
	o.CloseMaintenance()
}

func waitMaintenanceCleanup(t *testing.T, operations *Operations) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for operations.MaintenanceState().Busy {
		if time.Now().After(deadline) {
			t.Fatal("maintenance job did not complete")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestMaintenanceInterruptionDrainsHTTPAndDiscardsSoloSessions(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	initial, err := accounts.LoadState(accountstore.PrimaryUserID)
	if err != nil {
		t.Fatal(err)
	}
	o, err := NewOperations(accounts.Database(), initial.Gachas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(o.CloseMaintenance)
	builds := 0
	business := accounthttp.New(accounthttp.Config{IdleLimit: 32, Build: func(int) (http.Handler, error) {
		builds++
		return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), nil
	}})
	request := func() {
		r := httptest.NewRequest("POST", "/", nil)
		r.Header.Set(accounthttp.AccountUserHeader, strconv.Itoa(initial.User.UserID))
		business.ServeHTTP(httptest.NewRecorder(), r)
	}
	request()
	// Include a lightweight session whose account handler has already expired.
	accounts.RememberBattle(initial.User.UserID, &gamestate.TeamBattleActiveState{BossID: 30010102}, gamestate.StoryTeamBattleSession{})
	accounts.RememberBattle(initial.User.UserID+1, &gamestate.TeamBattleActiveState{BossID: 30010102}, gamestate.StoryTeamBattleSession{StoryID: 42})
	a := &API{accounts: accounts, operations: o, business: business, multiplayerHub: multiplayer.NewHub()}
	finish, ok := o.AdmitGameRequest()
	if !ok {
		t.Fatal("normal request rejected")
	}
	if _, err := o.SetMaintenance(true, 0, a.multiplayerHub.SetMaintenance); err != nil {
		t.Fatal(err)
	}
	makeRequest := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/maintenance/battles/interrupt", strings.NewReader(`{"expected_revision":1}`))
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Kairisei-Admin-Action", "apply")
		return r
	}
	// Waiting/canceling the operator request cannot discard an admitted settlement.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	w := httptest.NewRecorder()
	a.maintenanceWrites(http.HandlerFunc(a.interruptBattles)).ServeHTTP(w, makeRequest().WithContext(ctx))
	cancel()
	loaded, err := accounts.LoadState(initial.User.UserID)
	if err != nil || loaded.ActiveTeamBattle == nil || w.Code == 200 {
		t.Fatal("unfinished HTTP request was interrupted", err, w.Code)
	}
	// Finish the accepted outcome through the actual SQLite transaction.
	initial.User.Gold += 7
	sha := strings.Repeat("a", 64)
	initial.TeamBattleSoloResultReceipts = []gamestate.TeamBattleSoloResultReceipt{{BossID: 30010102, RequestSHA256: sha, ClaimedAtUnix: time.Now().Unix(), Snapshot: json.RawMessage(`{"is_clear":1}`)}}
	if err := accounts.PersistSoloBattle(initial, game.SoloBattleCommit{BossID: 30010102, ReceiptSHA: sha}); err != nil {
		t.Fatal(err)
	}
	finish()
	w = httptest.NewRecorder()
	a.maintenanceWrites(http.HandlerFunc(a.interruptBattles)).ServeHTTP(w, makeRequest())
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"discarded_solo":1`) {
		t.Fatal("interruption failed", w.Code, w.Body.String())
	}
	if accounts.DiscardUnsettledBattles() != 0 {
		t.Fatal("evicted solo session survived")
	}
	loaded, err = accounts.LoadState(initial.User.UserID)
	if err != nil || loaded.ActiveTeamBattle != nil || loaded.User.Gold != initial.User.Gold || len(loaded.TeamBattleSoloResultReceipts) != 1 {
		t.Fatal("maintenance lost committed reward/receipt", err)
	}
	request()
	if builds != 2 {
		t.Fatal("cached handler restored pre-maintenance battle")
	}
	business.InvalidateAll()
}
