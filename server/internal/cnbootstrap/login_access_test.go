package cnbootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/operations"
	"kairisei.local/server/internal/protocol"
	"kairisei.local/server/internal/testfixture"
)

func TestLoginAccessPrecedenceAndSessionRevocation(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	runtime, err := operations.NewRuntime(accounts.Database())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.CloseMaintenance()
	if err := accounts.AttachCreationPolicy(runtime.AccountCreationPolicy); err != nil {
		t.Fatal(err)
	}
	const uuid = "abc00000-0000-4000-8000-000000000001"
	const otherUUID = "abc00000-0000-4000-8000-000000000002"
	const newUUID = "abc00000-0000-4000-8000-000000000003"
	binding, err := accounts.BindInternalAccount(uuid, "access_test", "access-password")
	if err != nil {
		t.Fatal(err)
	}
	other, err := accounts.ResolveInternalLogin(otherUUID)
	if err != nil {
		t.Fatal(err)
	}
	whitelist, err := accountstore.CompileLoginWhitelist(true, []int{binding.UserID})
	if err != nil {
		t.Fatal(err)
	}
	blacklist, err := operations.CompileIPBlacklist([]string{"198.51.100.18/24", "2001:db8::7"})
	if err != nil {
		t.Fatal(err)
	}
	published := operations.Publication{
		LoginWhitelist: whitelist, IPBlacklist: blacklist,
		Settings: game.RuntimeSettings{AccountCreationDailyLimit: 10},
	}
	runtime.Publish(published)
	app := &application{operations: runtime}
	login := localClientLogin(accounts, "http://127.0.0.1", "cpk", "image", func(localLoginPayload) (string, string, error) {
		return "patch", "version", nil
	})
	gameLogin := app.blockIPRequests(login)
	passwordLogin := app.blockIPRequests(newCNAccountGateway(accounts))
	call := func(h http.Handler, path, body, peer string, status, code int, denial string) map[string]any {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.RemoteAddr = peer
		r.Header.Set("X-Kairisei-Account", "1")
		r.Header.Set("X-Forwarded-For", "203.0.113.99")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var data map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil || w.Code != status {
			t.Fatalf("%s: HTTP %d %s", path, w.Code, w.Body.String())
		}
		if (denial != "" && (data["error_code"] != denial || data["sess_key"] != nil || data["userid"] != nil)) ||
			(status == 200 && data["res_code"] != float64(code)) {
			t.Fatalf("%s: unexpected reply %v", path, data)
		}
		return data
	}
	const gamePath, accountPath = "/local/client/login", "/local/account/login"
	const password = `{"username":"access_test","password":"access-password"}`
	const peer = "192.0.2.1:12345"
	body := func(id string) string { return fmt.Sprintf(`{"uuid":%q,"clver":%q}`, id, cnMinimumClientVersion) }
	loginData := call(gameLogin, gamePath, body(uuid), peer, 200, 0, "")
	session := loginData["sess_key"].(string)
	call(gameLogin, gamePath, body(otherUUID), peer, 200, -12007, "login_whitelist_required")
	if id, err := accounts.ResolveSession(other.SessionKey); err != nil || id != other.UserID {
		t.Fatalf("login whitelist affected existing session: %d %v", id, err)
	}
	call(gameLogin, gamePath, body(newUUID), peer, 200, -12007, "login_whitelist_required")
	bind := fmt.Sprintf(`{"uuid":%q,"username":"access_new","password":"access-password"}`, newUUID)
	call(passwordLogin, "/local/account/bind", bind, peer, 403, 0, "login_whitelist_required")
	if id, err := accounts.AccountBinding(newUUID); err != nil || id.UserID != 0 {
		t.Fatalf("whitelist rejection created an identity: %+v %v", id, err)
	}
	permanent := int64(0)
	if _, err := accounts.SetLoginBlock(context.Background(), binding.UserID, &permanent); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.ResolveSession(session); !errors.Is(err, accountstore.ErrInvalidSession) {
		t.Fatalf("blocked session was not revoked: %v", err)
	}
	if data := call(gameLogin, gamePath, body(uuid), peer, 200, -12005, "account_login_blocked"); data["until_unix"] != float64(0) {
		t.Fatalf("permanent block deadline missing: %v", data)
	}
	call(passwordLogin, accountPath, password, peer, 403, 0, "account_login_blocked")
	call(gameLogin, gamePath, body(uuid), "[::ffff:198.51.100.11]:12345", 200, -12006, "ip_blacklisted")
	call(passwordLogin, accountPath, password, "[2001:db8::7]:12345", 403, 0, "ip_blacklisted")
	for _, preflight := range []struct{ method, path string }{
		{http.MethodGet, "/local/server/default.list?time=1"},
		{http.MethodPost, "//mods_switch.php"},
	} {
		request := httptest.NewRequest(preflight.method, preflight.path, nil)
		request.RemoteAddr = "198.51.100.11:12345"
		response := httptest.NewRecorder()
		gameLogin.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden || strings.TrimSpace(response.Body.String()) != ipBlacklistMessage {
			t.Fatalf("blocked title request %s: HTTP %d %q", preflight.path, response.Code, response.Body.String())
		}
	}
	if _, err := accounts.SetLoginBlock(context.Background(), binding.UserID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.ResolveSession(session); !errors.Is(err, accountstore.ErrInvalidSession) {
		t.Fatalf("recovery resurrected old session: %v", err)
	}
	call(gameLogin, gamePath, body(uuid), peer, 200, 0, "")

	published.DisabledModules = protocol.ModulePVP
	runtime.Publish(published)
	w := httptest.NewRecorder()
	app.moduleSwitches(w, httptest.NewRequest(http.MethodPost, "/mods_switch.php", nil))
	var switches struct {
		State protocol.ModuleSwitch `json:"mods_state"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &switches); err != nil || switches.State != protocol.DefaultModuleSwitches()&^protocol.ModulePVP {
		t.Fatalf("PvP closure not delivered: %s (%v)", w.Body.String(), err)
	}
	start := app.admitPVPStart(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	request := httptest.NewRequest(http.MethodPost, "/PvpStart2", nil)
	w = httptest.NewRecorder()
	start(w, request)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"res_code":-1200`) {
		t.Fatalf("closed PvP admitted a new start: %d %s", w.Code, w.Body.String())
	}
	published.DisabledModules = 0
	runtime.Publish(published)
	w = httptest.NewRecorder()
	start(w, request)
	if w.Code != http.StatusNoContent {
		t.Fatalf("reopened PvP did not reach the start handler: %d", w.Code)
	}
}
