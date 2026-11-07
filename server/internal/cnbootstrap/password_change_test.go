package cnbootstrap

import (
	"context"
	"encoding/json"
	"kairisei.local/server/internal/accountstore"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"kairisei.local/server/internal/testfixture"
)

func TestAccountPasswordChangeRequiresExistingCredentialAndKeepsCharacter(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	if err := accounts.AttachCreationPolicy(func() accountstore.CreationPolicy {
		return accountstore.CreationPolicy{DailyLimit: 10, VerificationBits: 0}
	}); err != nil {
		t.Fatal(err)
	}
	const uuid = "d4140000-0000-4000-8000-000000000001"
	identity, err := accounts.BindInternalAccount(uuid, "password_change", "original-password")
	if err != nil {
		t.Fatal(err)
	}
	db, err := accounts.Database().OpenRead()
	if err != nil {
		t.Fatal(err)
	}
	var sessionBefore, snapshotBefore string
	if err := db.QueryRow(`SELECT a.session_key, p.payload_sha256 FROM cn_local_account a JOIN cn_account_projection p ON p.user_id = a.user_id WHERE a.user_id = ?`, identity.UserID).Scan(&sessionBefore, &snapshotBefore); err != nil {
		t.Fatal(err)
	}
	var previousHash []byte
	if err := db.QueryRow(`SELECT password_hash FROM cn_account_credentials WHERE user_id = ?`, identity.UserID).Scan(&previousHash); err != nil {
		t.Fatal(err)
	}
	gateway := newCNAccountGateway(accounts)
	send := func(key, password, next string) int {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"uuid": key, "username": identity.Username, "password": password, "new_password": next})
		r := httptest.NewRequest("POST", "/local/account/change-password", strings.NewReader(string(body)))
		r.Header.Set("X-Kairisei-Account", "1")
		w := httptest.NewRecorder()
		gateway.ServeHTTP(w, r)
		return w.Code
	}
	if send(uuid, "wrong-password", "replacement-password") != 400 ||
		send("d4140000-0000-4000-8000-000000000002", "original-password", "replacement-password") != 400 ||
		send(uuid, "original-password", "short") != 400 {
		t.Fatal("invalid password change accepted")
	}
	var afterFailures []byte
	if err := db.QueryRow(`SELECT password_hash FROM cn_account_credentials WHERE user_id = ?`, identity.UserID).Scan(&afterFailures); err != nil || string(afterFailures) != string(previousHash) {
		t.Fatal("failed change altered the credential", err)
	}
	if send(uuid, "original-password", " replacement-password ") != 200 {
		t.Fatal("valid password change failed")
	}
	if _, err := accounts.LoginAccount(identity.Username, "original-password"); err == nil {
		t.Fatal("old password still authenticates")
	}
	if _, err := accounts.LoginAccount(identity.Username, "replacement-password"); err == nil {
		t.Fatal("password whitespace was trimmed")
	}
	if got, err := accounts.LoginAccount(identity.Username, " replacement-password "); err != nil || got != identity {
		t.Fatal("new password changed character identity", got, err)
	}
	var sessionAfter, snapshotAfter string
	if err := db.QueryRow(`SELECT a.session_key, p.payload_sha256 FROM cn_local_account a JOIN cn_account_projection p ON p.user_id = a.user_id WHERE a.user_id = ?`, identity.UserID).Scan(&sessionAfter, &snapshotAfter); err != nil || sessionAfter != sessionBefore || snapshotAfter != snapshotBefore {
		t.Fatal("password change altered game session or progress", err)
	}
	if len(gateway.workers) != 0 {
		t.Fatal("password worker retained")
	}
}

func TestConcurrentPasswordChangesCannotReuseOldAuthority(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	if err := accounts.AttachCreationPolicy(func() accountstore.CreationPolicy {
		return accountstore.CreationPolicy{DailyLimit: 10, VerificationBits: 0}
	}); err != nil {
		t.Fatal(err)
	}
	const uuid = "d4140000-0000-4000-8000-000000000003"
	identity, err := accounts.BindInternalAccount(uuid, "password_race", "original-password")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var running sync.WaitGroup
	for _, password := range []string{"first-replacement", "second-replacement"} {
		running.Add(1)
		go func() {
			defer running.Done()
			<-start
			_, err := accounts.ChangeAccountPassword(context.Background(), uuid, identity.Username, "original-password", password)
			results <- err
		}()
	}
	close(start)
	running.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("password authority reused: %d changes succeeded", succeeded)
	}
	if _, err := accounts.LoginAccount(identity.Username, "original-password"); err == nil {
		t.Fatal("old password survived concurrent replacement")
	}
}
