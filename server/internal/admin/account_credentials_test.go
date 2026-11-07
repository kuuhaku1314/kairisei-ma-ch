package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/testfixture"
)

func TestAccountBindingRecoveryAndAdminReset(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	uuid := "d4640000-0000-4000-8000-000000000001"
	identity, err := accounts.ResolveInternalLogin(uuid)
	if err != nil {
		t.Fatal(err)
	}
	state, err := accounts.LoadState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	state.User.Name = "原来的角色"
	if err := accounts.PersistState(identity.UserID, state); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.BindInternalAccount(uuid, " Arthur_1 ", "correct-password"); err != nil {
		t.Fatal(err)
	}
	if id, err := accounts.ResolveSession(identity.SessionKey); err != nil || id != identity.UserID {
		t.Fatalf("binding interrupted game session: %d %v", id, err)
	}
	// Reopening the database simulates a server restart; login needs no old
	// device UUID and returns the same identity and stored progress.
	reopened, err := accountstore.NewAccounts(accounts.Database())
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := reopened.LoginAccount("ARTHUR_1", "correct-password")
	if err != nil || recovered.LoginUUID != uuid || recovered.UserID != identity.UserID {
		t.Fatalf("recovery: %+v %v", recovered, err)
	}
	state, err = reopened.LoadState(recovered.UserID)
	if err != nil || state.User.Name != "原来的角色" {
		t.Fatalf("save lost: %v", err)
	}
	if _, err := reopened.LoginAccount("arthur_1", "wrong-password"); err == nil {
		t.Fatal("accepted incorrect password")
	}
	if _, err := reopened.BindInternalAccount(uuid, "someone_else", "correct-password"); err == nil {
		t.Fatal("rebound an existing account")
	}
	otherUUID := "d4640000-0000-4000-8000-000000000002"
	if _, err := reopened.BindInternalAccount(otherUUID, "arthur_1", "different-password"); err == nil {
		t.Fatal("stole a registered name")
	}
	if binding, err := reopened.AccountBinding(otherUUID); err != nil || binding.UserID != 0 {
		t.Fatalf("failed binding left a guest account: %+v %v", binding, err)
	}
	admin := &API{accounts: reopened}
	router := chi.NewRouter()
	router.Post("/api/accounts/{userID}/credentials", admin.setAccountCredentials)
	router.Delete("/api/accounts/{userID}/credentials", admin.unbindAccount)
	body := []byte(`{"username":"arthur_1","password":"new-password-464"}`)
	request := httptest.NewRequest("POST", "/api/accounts/1000001/credentials", bytes.NewReader(body))
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Content-Type", "application/json")
	denied := httptest.NewRecorder()
	router.ServeHTTP(denied, request)
	if denied.Code != 403 {
		t.Fatalf("missing mutation guard: %d", denied.Code)
	}
	request = httptest.NewRequest("POST", "/api/accounts/1000001/credentials", bytes.NewReader(body))
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Kairisei-Admin-Action", "apply")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	if _, err := reopened.LoginAccount("arthur_1", "correct-password"); err == nil {
		t.Fatal("old password still accepted")
	}
	if _, err := reopened.LoginAccount("arthur_1", "new-password-464"); err != nil {
		t.Fatal(err)
	}
	rows, err := admin.loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(rows)
	if !bytes.Contains(encoded, []byte("arthur_1")) || bytes.Contains(encoded, []byte("password")) {
		t.Fatalf("admin credential projection: %s", encoded)
	}
	for _, guarded := range []bool{false, true} {
		request = httptest.NewRequest("DELETE", "/api/accounts/1000001/credentials", strings.NewReader(`{"username":"arthur_1"}`))
		request.RemoteAddr = "127.0.0.1:12345"
		request.Header.Set("Content-Type", "application/json")
		if guarded {
			request.Header.Set("X-Kairisei-Admin-Action", "apply")
		}
		response = httptest.NewRecorder()
		router.ServeHTTP(response, request)
		want := http.StatusForbidden
		if guarded {
			want = http.StatusOK
		}
		if response.Code != want {
			t.Fatalf("unbind guarded=%v: %d %s", guarded, response.Code, response.Body.String())
		}
	}
	if _, err := reopened.LoginAccount("arthur_1", "new-password-464"); err == nil {
		t.Fatal("unbound password still accepted")
	}
	if binding, err := reopened.AccountBinding(uuid); err != nil || binding.UserID != identity.UserID || binding.Username != "" {
		t.Fatalf("unbind removed the character identity: %+v %v", binding, err)
	}
	state, err = reopened.LoadState(identity.UserID)
	if err != nil || state.User.Name != "原来的角色" {
		t.Fatalf("unbind lost the save: %v", err)
	}
	request = httptest.NewRequest("POST", "/api/accounts/1000001/credentials", strings.NewReader(`{"username":"arthur_new","password":"rebound-password"}`))
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Kairisei-Admin-Action", "apply")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	if err := reopened.UnbindAccount(identity.UserID, "arthur_1"); err == nil {
		t.Fatal("stale unbind removed a newer binding")
	}
	if rebound, err := reopened.LoginAccount("arthur_new", "rebound-password"); err != nil || rebound.UserID != identity.UserID || rebound.LoginUUID != uuid {
		t.Fatalf("admin rebind: %+v %v", rebound, err)
	}
	db, err := reopened.Database().Open()
	if err != nil {
		t.Fatal(err)
	}
	var audit string
	if err := db.QueryRow(`SELECT GROUP_CONCAT(payload_json) FROM cn_admin_audit`).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(audit, "password") {
		t.Fatal("password in audit")
	}
}
